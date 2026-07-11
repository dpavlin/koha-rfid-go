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
    local name
    name=$(echo "$SCENARIOS" | jq -r ".[] | select(.id == $sid) | .name")
    local tags
    tags=$(echo "$SCENARIOS" | jq -r ".[] | select(.id == $sid) | .tags // [] | join(\" \")")
    local tab_name
    tab_name=$(echo "$SCENARIOS" | jq -r ".[] | select(.id == $sid) | .tab // \"\"")
    local error_mode
    error_mode=$(echo "$SCENARIOS" | jq -r ".[] | select(.id == $sid) | .error_mode // 0")
    local timeout_mode
    timeout_mode=$(echo "$SCENARIOS" | jq -r ".[] | select(.id == $sid) | .timeout_mode // 0")
    local remove_after_scan
    remove_after_scan=$(echo "$SCENARIOS" | jq -r ".[] | select(.id == $sid) | .remove_after_scan // false")
    local sequence
    sequence=$(echo "$SCENARIOS" | jq -r ".[] | select(.id == $sid) | .sequence // false")

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
        sleep 5
        
        # Wait for rfid_scan to finish (it might reload the page)
        rodney waitload
        rodney sleep 2

        # Now check if the input is filled. If not, it means rfid_scan didn't submit.
        if [ "$(rodney js "document.querySelector('input[name=findborrower]')?.value")" = "" ]; then
            info "input is empty, submitting manually..."
            rodney click 'input.submit'
            rodney waitload
            rodney sleep 2
        else
            info "input is already filled by rfid_scan"
            rodney waitload
            rodney sleep 2
        fi

        # Now load books one by one
        for tag_key in book1 book2 book3; do
            if echo "$tags" | grep -q "$tag_key"; then
                info "loading $tag_key..."
                mock_clear
                load_tag "$tag_key"
                sleep 3
                rodney waitload
                rodney sleep 2
                if [ "$sid" -le 13 ]; then
                    local bc
                    bc=$(echo "$TAGS" | jq -r ".\"$tag_key\".content")
                    local count
                    count=$(ssh koha-dev.rot13.org sudo /usr/sbin/koha-mysql ffzg -e "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bc'")
                    if [ "$count" -gt 0 ]; then
                        pass "$tag_key is checked out"
                    else
                        fail "$tag_key is NOT checked out in DB"
                    fi
                fi
            fi
        done

        # For scenario 14 (patron + D7), expect popup "not checked in"
        if [ "$sid" -eq 14 ]; then
            check_popup_contains "not checked in" && result "pass" || result "fail"
        else
            # Verify DB — issues created
            local db_check
            db_check=$(echo "$SCENARIOS" | jq -r ".[] | select(.id == $sid) | .expect.db_query // \"\"")
            if [ -n "$db_check" ]; then
                check_db "$db_check" "1" && result "pass" || result "fail"
            else
                info "DB: checking issues count"
                local count
                count=$(ssh koha-dev.rot13.org sudo /usr/sbin/koha-mysql ffzg -e "SELECT COUNT(*) FROM issues WHERE borrowernumber=(SELECT borrowernumber FROM borrowers WHERE cardnumber='200000000042') AND itemnumber=(SELECT itemnumber FROM items WHERE barcode='1301111111')" 2>/dev/null)
                echo "$count" | grep -qE '[1-3]' && result "pass" || result "fail"
            fi
        fi
    else
        # Non-sequential: load all tags at once
        for tag_key in $tags; do load_tag "$tag_key"; done
        sleep 3

        if [ "$error_mode" -gt 0 ]; then
            local expected_err
            expected_err=$(echo "$SCENARIOS" | jq -r ".[] | select(.id == $sid) | .expect.popup // \"error\"")
            check_popup_contains "$expected_err" && result "pass" || result "fail"
            return
        fi
        if [ "$timeout_mode" -gt 0 ]; then
            local expected_tout
            expected_tout=$(echo "$SCENARIOS" | jq -r ".[] | select(.id == $sid) | .expect.popup // \"timeout\"")
            check_popup_contains "$expected_tout" && result "pass" || result "fail"
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

# Navigate to the test page
rodney open "$PAGE_URL"
rodney waitload

# Clear localStorage
rodney js "localStorage.removeItem('rfid_afi')"

# -- Pre-flight checks --
pre_flight_check

# Check that the default checkout form actually works — patron scan finds patron
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
        exit 1
    fi
else
    fail "default checkout form not found" || true
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

# -- Post-flight check --
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
