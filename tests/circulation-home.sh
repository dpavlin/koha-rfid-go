#!/bin/bash
# tests/circulation-home.sh — Linear test suite for circulation-home.pl
# Single initialization, no JSON parsing.

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE="circulation-home"
PAGE_URL="$KOHA_URL/circ/circulation-home.pl"

# --- Single Initialization ---
echo "[========================================]"
echo "|  circulation-home"
echo "[========================================]"

rodney connect localhost:$CDP_PORT
koha_login
mock_start
suite_start "$PAGE_URL"

# --- Scenario 1: No tags ---
scenario_start 1 "No tags"
mock_clear
check_popup_empty

# --- Scenario 2: Patron only ---
scenario_start 2 "Patron only"
mock_clear
tab_switch "checkout"
load_tag "200000000042"
rodney sleep 2
rodney waitload
rodney url | grep -q "circulation.pl" && pass "Navigated to circulation.pl" || fail "Did not navigate to circulation.pl"

# --- Scenario 3: Book DA checkin ---
scenario_start 3 "Book DA checkin"
visit_page "$PAGE_URL"
mock_clear
tab_switch "checkin"
load_tag "1301111111"
rodney sleep 2
rodney waitload
check_popup_contains "1301111111"

# --- Scenario 4: Book D7 renew ---
scenario_start 4 "Book D7 renew"
visit_page "$PAGE_URL"
mock_clear
tab_switch "renew"
load_tag "1302099999"
rodney sleep 2
rodney waitload
check_popup_contains "1302099999"

# --- Scenario 5: Empty tag ---
scenario_start 5 "Empty tag"
mock_clear
tab_switch "checkout"
load_tag "empty"
rodney sleep 2
check_popup_empty

# --- Scenario 7: Timeout mode ---
scenario_start 7 "Timeout mode"
rfid_pause
mock_clear
mock_timeout 100
load_tag "1301111111"
rfid_resume
rodney sleep 3
check_popup_contains "timeout"

# --- Scenario 8: Tag leaves range ---
scenario_start 8 "Tag leaves range"
mock_clear
tab_switch "checkout"
load_tag "1301111111"
rodney sleep 3
mock_clear
rodney sleep 3
check_popup_empty

# --- Scenario 21: Patron on circ-home ---
scenario_start 21 "Patron on circ-home"
visit_page "$PAGE_URL"
mock_clear
tab_switch "checkout"
load_tag "200000000042"
rodney sleep 2
rodney waitload
rodney url | grep -q "circulation.pl" && pass "Navigated to circulation.pl" || fail "Did not navigate to circulation.pl"


# --- Scenario 22: Book checkin on circ-home ---
scenario_start 22 "Book checkin on circ-home"
visit_page "$PAGE_URL"
mock_clear
tab_switch "checkin"
load_tag "1301111111"
rodney sleep 2
rodney waitload
actual_url=$(rodney url)
if echo "$actual_url" | grep -q "returns.pl"; then
    pass "Navigated to returns.pl"
else
    fail "Did not navigate to returns.pl (actual URL: $actual_url)"
fi

# --- Scenario 23: Book renew on circ-home ---
scenario_start 23 "Book renew on circ-home"
visit_page "$PAGE_URL"
mock_clear
tab_switch "renew"
load_tag "1302099999"
rodney sleep 2
rodney waitload
actual_url=$(rodney url)
if echo "$actual_url" | grep -q "renew.pl"; then
    pass "Navigated to renew.pl"
else
    fail "Did not navigate to renew.pl (actual URL: $actual_url)"
fi

echo ""
echo "Done."
