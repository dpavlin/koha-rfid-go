#!/bin/bash
# test-returns.sh — returns.pl (single #barcode form, checkin flow)
# Usage: ./tests/test-returns.sh [scenario_id]
set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE="returns"
PAGE_URL="$KOHA_URL/circ/returns.pl"
SCENARIO_FILTER="${1:-}"

# Scenarios for returns: 1, 3, 5-10, 15-18
SCENARIO_IDS=$(echo "$SCENARIOS" | jq -r '.[] | select(.pages | index("returns")) | .id')

run_scenario() {
    local sid="$1"
    SCENARIO_ID="$sid"
    local scenario tags error_mode timeout_mode remove_after_scan sequence

    scenario=$(echo "$SCENARIOS" | jq ".[] | select(.id == $sid)")
    local name; name=$(echo "$scenario" | jq -r '.name')
    tags=$(echo "$scenario" | jq -r '.tags // [] | join(" ")')
    error_mode=$(echo "$scenario" | jq -r '.error_mode // 0')
    timeout_mode=$(echo "$scenario" | jq -r '.timeout_mode // 0')
    remove_after_scan=$(echo "$scenario" | jq -r '.remove_after_scan // false')
    sequence=$(echo "$scenario" | jq -r '.sequence // false')

    echo ""
    echo "  ── Scenario $sid: $name ──"

    mock_clear
    [ "$error_mode" -gt 0 ] && mock_error "$error_mode"
    [ "$timeout_mode" -gt 0 ] && mock_timeout 100

    # returns.pl has no tabs — #barcode visible by default
    for tag_key in $tags; do load_tag "$tag_key"; done
    sleep 3

    # Tier 1: error/timeout/empty
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

    # Tier 3: verify barcode filled + DB effects
    check_input_filled '#barcode' || { result "fail"; return; }

    # For returns, DB should show items returned
    if [ "$sid" -ge 15 ]; then
        local book_count
        book_count=$(echo "$tags" | wc -w)
        info "DB: checking $book_count items returned"
        local barcodes; barcodes=$(echo "$tags" | xargs -n1 sh -c 'echo "$TAGS" | jq -r ".\"$1\".content" ' 2>/dev/null | tr '\n' ' ')
        local count
        count=$(ssh koha-dev.rot13.org sudo /usr/sbin/koha-mysql ffzg -e "SELECT COUNT(*) FROM items WHERE barcode IN ($barcodes) AND onloan IS NULL" 2>/dev/null)
        echo "$count" | grep -qE "$book_count" && result "pass" || result "fail"
    else
        result "pass"
    fi
}

# ── main ──
echo "╔════════════════════════════════════════╗"
echo "║  $PAGE"
echo "╚════════════════════════════════════════╝"

rodney connect localhost:$CDP_PORT
koha_login
mock_start
# Navigate to the test page (reusing page 0 instead of opening a new tab)
rodney open "$PAGE_URL"
rodney waitload
for sid in $SCENARIO_IDS; do
    [ -n "$SCENARIO_FILTER" ] && [ "$sid" != "$SCENARIO_FILTER" ] && continue
    run_scenario "$sid"
done

echo ""
echo "Done."
