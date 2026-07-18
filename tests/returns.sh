#!/bin/bash
# tests/returns.sh — Linear test suite for returns.pl
# Single initialization, no JSON parsing.

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE="returns"
PAGE_URL="$KOHA_URL/circ/returns.pl"

# --- Single Initialization ---
echo "[========================================]"
echo "|  returns"
echo "[========================================]"

rodney connect localhost:$CDP_PORT
koha_login
cleanup_issues
mock_start
suite_start "$PAGE_URL"

# --- Scenario 1: No tags ---
scenario_start 1 "No tags"
mock_clear
check_popup_empty

# --- Scenario 3: Book DA checkin ---
scenario_start 3 "Book DA checkin"
mock_clear
load_tag "1301111111"
rodney sleep 3
check_popup_contains "1301111111"

# --- Scenario 5: Empty tag ---
scenario_start 5 "Empty tag"
mock_clear
load_tag "empty"
rodney sleep 2
check_popup_empty

# --- Scenario 6: Error mode ---
scenario_start 6 "Error mode"
rfid_pause
mock_clear
mock_error 3
load_tag "1301111111"
rfid_resume
rodney sleep 3
check_popup_contains "error"

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
load_tag "1301111111"
rodney sleep 3
mock_clear
rodney sleep 3
check_popup_empty

# --- Scenario 9: Multiple books DA ---
scenario_start 9 "Multiple books DA"
mock_clear
load_tag "1301111111"
load_tag "1302079605"
rodney sleep 5
check_popup_contains "1301111111"
check_popup_contains "1302079605"

# --- Scenario 10: Mixed AFI ---
scenario_start 10 "Mixed AFI"
mock_clear
load_tag "1301111111"
load_tag "1302099999"
rodney sleep 5
check_popup_contains "1302099999"

# --- Scenario 15: Return 1 issued book (D7 → DA) ---
db_checkout "200000000042" "1301111111"

scenario_start 15 "Return 1 issued book (D7 → DA)"
mock_clear
load_tag_with_security "1301111111" "D7"
check_db "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='1301111111'" "0"
check_mock_tag_security "1301111111" "DA"

# --- Scenario 16: Return 2 issued books (D7 → DA) ---
db_checkout "200000000042" "1301111111"
db_checkout "200000000042" "1302079605"

scenario_start 16 "Return 2 issued books (D7 → DA)"
mock_clear
load_tag_with_security "1301111111" "D7"
load_tag_with_security "1302079605" "D7"
check_db "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode IN ('1301111111', '1302079605')" "0"
check_mock_tag_security "1301111111" "DA"
check_mock_tag_security "1302079605" "DA"

# --- Scenario 17: Return 3 issued books (D7 → DA) ---
db_checkout "200000000042" "1301111111"
db_checkout "200000000042" "1302079605"
db_checkout "200000000042" "1302099999"

scenario_start 17 "Return 3 issued books (D7 → DA)"
mock_clear
load_tag_with_security "1301111111" "D7"
load_tag_with_security "1302079605" "D7"
load_tag_with_security "1302099999" "D7"
check_db "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode IN ('1301111111', '1302079605', '1302099999')" "0"
check_mock_tag_security "1301111111" "DA"
check_mock_tag_security "1302079605" "DA"
check_mock_tag_security "1302099999" "DA"

# --- Scenario 18: Return D7 book ---
db_checkout "200000000042" "1302099999"

scenario_start 18 "Return D7 book"
mock_clear
load_tag_with_security "1302099999" "D7"
check_db "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='1302099999'" "0"
check_mock_tag_security "1302099999" "DA"

cleanup_issues

echo ""
echo "Done."
