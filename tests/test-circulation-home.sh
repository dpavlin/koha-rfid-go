#!/bin/bash
# test-circulation-home.sh — circulation-home.pl (tabbed: checkout/checkin/renew)
# Usage: ./tests/test-circulation-home.sh [scenario_id]
set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE="circulation-home"
PAGE_URL="$KOHA_URL/circ/circulation-home.pl"
SCENARIO_FILTER="${1:-}"

# Scenarios for circ-home: 1-10, 21-23
SCENARIO_IDS=$(echo "$SCENARIOS" | jq -r '.[] | select(.pages | index("circulation-home")) | .id')

run_scenario() {
    local sid="$1"
    SCENARIO_ID="$sid"
    local scenario tags tab_name error_mode timeout_mode remove_after_scan

    scenario=$(echo "$SCENARIOS" | jq ".[] | select(.id == $sid)")
    local name; name=$(echo "$scenario" | jq -r '.name')
    tags=$(echo "$scenario" | jq -r '.tags // [] | join(" ")')
    tab_name=$(echo "$scenario" | jq -r '.tab // ""')
    error_mode=$(echo "$scenario" | jq -r '.error_mode // 0')
    timeout_mode=$(echo "$scenario" | jq -r '.timeout_mode // 0')
    remove_after_scan=$(echo "$scenario" | jq -r '.remove_after_scan // false')

    echo ""
    echo "  ── Scenario $sid: $name ──"

    mock_clear
    [ "$error_mode" -gt 0 ] && mock_error "$error_mode"
    [ "$timeout_mode" -gt 0 ] && mock_timeout 100

    tab_switch "$tab_name"

    for tag_key in $tags; do load_tag "$tag_key"; done
    sleep 3

    # Tier 1: error/timeout/empty checks
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

    # Tier 5: verify input filled
    case "$sid" in
        21) check_input_filled 'input[name=findborrower]' && result "pass" || result "fail" ;;
        22) check_input_filled '#ret_barcode' && result "pass" || result "fail" ;;
        23) check_input_filled '#ren_barcode' && result "pass" || result "fail" ;;
        *)
            local first_content
            first_content=$(echo "$tags" | awk '{print $1}' | while read -r key; do echo "$TAGS" | jq -r ".\"$key\".content" 2>/dev/null; done)
            if [ -n "$first_content" ]; then
                check_popup_contains "$first_content" && result "pass" || result "fail"
            else
                check_popup_contains "patron" && result "pass" || result "fail"
            fi
            ;;
    esac
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
