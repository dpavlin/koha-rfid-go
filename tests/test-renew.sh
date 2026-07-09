#!/bin/bash
# test-renew.sh — renew.pl (single #barcode form, renew flow)
# Usage: ./tests/test-renew.sh [scenario_id]
set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE="renew"
PAGE_URL="$KOHA_URL/circ/renew.pl"
SCENARIO_FILTER="${1:-}"

# Scenarios for renew: 1, 4-10, 19-20
SCENARIO_IDS=$(echo "$SCENARIOS" | jq -r '.[] | select(.pages | index("renew")) | .id')

run_scenario() {
    local sid="$1"
    SCENARIO_ID="$sid"
    local scenario tags error_mode timeout_mode remove_after_scan

    scenario=$(echo "$SCENARIOS" | jq ".[] | select(.id == $sid)")
    local name; name=$(echo "$scenario" | jq -r '.name')
    tags=$(echo "$scenario" | jq -r '.tags // [] | join(" ")')
    error_mode=$(echo "$scenario" | jq -r '.error_mode // 0')
    timeout_mode=$(echo "$scenario" | jq -r '.timeout_mode // 0')
    remove_after_scan=$(echo "$scenario" | jq -r '.remove_after_scan // false')

    echo ""
    echo "  ── Scenario $sid: $name ──"

    mock_clear
    [ "$error_mode" -gt 0 ] && mock_error "$error_mode"
    [ "$timeout_mode" -gt 0 ] && mock_timeout 100

    # renew.pl has no tabs — #barcode visible by default
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

    # Tier 4: verify ren_barcode filled (plugin uses #ren_barcode on renew.pl) + DB effects
    check_input_filled '#ren_barcode' || { result "fail"; return; }

    # Scenario 19 (D7 renew): DB should show renewed
    if [ "$sid" -eq 19 ]; then
        check_db "SELECT renews FROM issues WHERE itemnumber=(SELECT itemnumber FROM items WHERE barcode='1302099999')" "1" && result "pass" || result "fail"
    # Scenario 20 (DA renew): popup "not on loan"
    elif [ "$sid" -eq 20 ]; then
        check_popup_contains "not on loan" && result "pass" || result "fail"
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
