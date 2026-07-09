#!/bin/bash
# test-mainpage.sh — mainpage.pl (no forms, just RFID status display)
# Usage: ./tests/test-mainpage.sh [scenario_id]
set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE="mainpage"
PAGE_URL="$KOHA_URL/mainpage.pl"
SCENARIO_FILTER="${1:-}"

# Scenarios that apply to mainpage: 1, 5-10
SCENARIO_IDS=$(echo "$SCENARIOS" | jq -r '.[] | select(.pages | index("mainpage")) | .id')

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
    [ "$timeout_mode" -gt 0 ] && mock_timeout 1  # initial timeout count; timeout block overrides to 100

    for tag_key in $tags; do load_tag "$tag_key"; done

    if [ "$timeout_mode" -gt 0 ]; then
        # Timeout scenario: mock returns timeout for all scan retries.
        # Use a high count (100) to cover multiple polls during the 6s sleep.
        mock_timeout 100
        sleep 6
        check_popup_contains "timeout" && result "pass" || result "fail"
    else
        sleep 3
    fi

    # mainpage has no forms — just check popup behavior
    if [ "$error_mode" -gt 0 ]; then
        check_popup_contains "error" && result "pass" || result "fail"
    elif [ "$timeout_mode" -gt 0 ]; then
        : # already handled above
    elif [ "$remove_after_scan" = "true" ]; then
        local first_tag sid
        first_tag=$(echo "$tags" | awk '{print $1}')
        sid=$(echo "$TAGS" | jq -r ".\"$first_tag\".sid")
        mock_remove "$sid"
        sleep 3
        check_popup_empty && result "pass" || result "fail"
    elif [ -z "$tags" ]; then
        check_popup_empty && result "pass" || result "fail"
    elif echo "$tags" | grep -q "empty"; then
        check_popup_empty && result "pass" || result "fail"
    else
        local first_content
        first_content=$(echo "$tags" | awk '{print $1}' | while read -r key; do echo "$TAGS" | jq -r ".\"$key\".content" 2>/dev/null; done)
        if [ -n "$first_content" ]; then
            check_popup_contains "$first_content" && result "pass" || result "fail"
        else
            check_popup_contains "patron" && result "pass" || result "fail"
        fi
    fi
}

# ── main ──
echo "╔════════════════════════════════════════╗"
echo "║  $PAGE"
echo "╚════════════════════════════════════════╝"

rodney connect localhost:$CDP_PORT
koha_login
mock_start

for sid in $SCENARIO_IDS; do
    [ -n "$SCENARIO_FILTER" ] && [ "$sid" != "$SCENARIO_FILTER" ] && continue
    run_scenario "$sid"
done

echo ""
echo "Done."
