#!/bin/bash
# test-circulation.sh — circulation.pl (tabbed: checkout/checkin/renew, patron+book flow)
# Usage: ./tests/test-circulation.sh [scenario_id]
set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE="circulation"
PAGE_URL="$KOHA_URL/circ/circulation.pl"
SCENARIO_FILTER="${1:-}"

# Scenarios for circulation: 1-14
SCENARIO_IDS=$(echo "$SCENARIOS" | jq -r '.[] | select(.pages | index("circulation")) | .id')

run_scenario() {
    local sid="$1"
    SCENARIO_ID="$sid"
    local scenario tags tab_name error_mode timeout_mode remove_after_scan sequence

    scenario=$(echo "$SCENARIOS" | jq ".[] | select(.id == $sid)")
    local name; name=$(echo "$scenario" | jq -r '.name')
    tags=$(echo "$scenario" | jq -r '.tags // [] | join(" ")')
    tab_name=$(echo "$scenario" | jq -r '.tab // ""')
    error_mode=$(echo "$scenario" | jq -r '.error_mode // 0')
    timeout_mode=$(echo "$scenario" | jq -r '.timeout_mode // 0')
    remove_after_scan=$(echo "$scenario" | jq -r '.remove_after_scan // false')
    sequence=$(echo "$scenario" | jq -r '.sequence // false')

    echo ""
    echo "  -- Scenario $sid: $name --"

    mock_clear
    [ "$error_mode" -gt 0 ] && mock_error "$error_mode"
    [ "$timeout_mode" -gt 0 ] && mock_timeout 100

    tab_switch "$tab_name"

    if [ "$sequence" = "true" ] && [ "$sid" -ge 11 ] && [ "$sid" -le 14 ]; then
        # Sequential: patron first, then books
        info "loading patron tag..."
        load_tag "patron"
        sleep 3
        # Verify patron found
        check_input_filled 'input[name=findborrower]' || { result "fail"; return; }

        # Now submit patron search so page reloads and #barcode appears
        info "submitting patron search..."
        rodney click 'input#submit_findborrower'
        rodney waitload
        rodney sleep 2

        # Load books one by one
        for tag_key in book1 book2 book3; do
            if echo "$tags" | grep -q "$tag_key"; then
                info "loading $tag_key..."
                mock_clear
                load_tag "$tag_key"
                sleep 3
                check_input_filled '#barcode' || echo "  [warn] barcode not filled"
            fi
        done

        # For scenario 14 (patron + D7), expect popup "not checked in"
        if [ "$sid" -eq 14 ]; then
            check_popup_contains "not checked in" && result "pass" || result "fail"
        else
            # Verify DB — issues created
            local db_check
            db_check=$(echo "$scenario" | jq -r '.expect.db_query // ""')
            if [ -n "$db_check" ]; then
                check_db "$db_check" "1" && result "pass" || result "fail"
            else
                info "DB: checking issues count"
                local count
                count=$(ssh koha-dev.rot13.org sudo /usr/sbin/koha-mysql ffzg -e "SELECT COUNT(*) FROM issues WHERE borrowernumber=(SELECT borrowernumber FROM borrowers WHERE cardnumber='200000000042')" 2>/dev/null)
                echo "$count" | grep -qE '[1-3]' && result "pass" || result "fail"
            fi
        fi
    else
        # Non-sequential: load all tags at once
        for tag_key in $tags; do load_tag "$tag_key"; done
        sleep 3

        if [ "$error_mode" -gt 0 ]; then
            check_popup_contains "error" && result "pass" || result "fail"
            return
        fi
        if [ "$timeout_mode" -gt 0 ]; then
            check_popup_contains "timeout" && result "pass" || result "fail"
            return
        fi
        if [ "$remove_after_scan" = "true" ]; then
            local first_tag sid
            first_tag=$(echo "$tags" | awk '{print $1}')
            sid=$(echo "$TAGS" | jq -r ".\"$first_tag\".sid")
            mock_remove "$sid"
            sleep 3
            check_popup_empty && result "pass" || result "fail"
            return
        fi
        if [ -z "$tags" ]; then
            check_popup_empty && result "pass" || result "fail"
            return
        fi
        if echo "$tags" | grep -q "empty"; then
            check_popup_empty && result "pass" || result "fail"
            return
        fi
        # Check popup shows barcode content (the 130... number) for book tags
        local first_content
        first_content=$(echo "$tags" | awk '{print $1}' | while read -r key; do echo "$TAGS" | jq -r ".\"$key\".content" 2>/dev/null; done)
        if [ -n "$first_content" ]; then
            check_popup_contains "$first_content" && result "pass" || result "fail"
        else
            # patron tag — check for "patron" in popup
            check_popup_contains "patron" && result "pass" || result "fail"
        fi
    fi
}

# -- main --
echo "[════════════════════════════════════════]"
echo "|  $PAGE"
echo "[════════════════════════════════════════]"

rodney connect localhost:$CDP_PORT
koha_login
mock_start

# Navigate to the test page (reusing page 0 instead of opening a new tab)
rodney open "$PAGE_URL"
rodney waitload

# Clear RFID localStorage state from previous runs
rodney js "localStorage.removeItem('rfid_afi')"

# -- Pre-flight: verify Koha DB state and default form --
pre_flight_check

# Check that the default checkout form actually works — patron scan fills findborrower
echo ""
echo "-- Default form check --"
if rodney exists 'input[name=findborrower]' 2>/dev/null; then
    pass "default checkout form (findborrower) is present"
    # Test the form works: load a patron tag and verify patron is found
    mock_clear
    load_tag "patron"
    sleep 3
    if rodney visible '.patroninfo' 2>/dev/null; then
        pass "default form works — patron scan finds patron"
    else
        fail "default form not responding to RFID scan" || true
        debug_help
        exit 1
    fi
else
    fail "default checkout form not found" || true
    debug_help
    exit 1
fi
echo "-- Default form OK --"

# Run scenarios
for sid in $SCENARIO_IDS; do
    [ -n "$SCENARIO_FILTER" ] && [ "$sid" != "$SCENARIO_FILTER" ] && continue
    run_scenario "$sid"
done

# -- Cleanup: revert Koha DB to original state --
cleanup_issues

# -- Post-flight: verify state matches beginning --
echo ""
echo "-- Post-flight check --"
for bc in 1301111111 1302079605 1302099999; do
    issued=$(ssh koha-dev.rot13.org sudo /usr/sbin/koha-mysql ffzg -e "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bc'" 2>/dev/null || echo "")
    if echo "$issued" | grep -q "0"; then
        pass "barcode $bc is not issued — clean"
    else
        fail "barcode $bc is still issued"
    fi
done
echo "-- Post-flight done --"

echo ""
echo "Done."
