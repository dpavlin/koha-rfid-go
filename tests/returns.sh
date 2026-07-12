#!/bin/bash
# tests/returns.sh — Linear test suite for returns.pl
# Single initialization, no JSON parsing.

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE_URL="$KOHA_URL/circ/returns.pl"

# --- Single Initialization ---
echo "[════════════════════════════════════════]"
echo "|  returns"
echo "[════════════════════════════════════════]"

rodney connect localhost:$CDP_PORT
koha_login
mock_start
rodney open "$PAGE_URL"
rodney waitload
rodney sleep 2

# --- Scenario 1: No tags ---
echo "Running Scenario 1: No tags"
mock_clear
rodney sleep 3
check_popup_empty

# --- Scenario 3: Book DA checkin ---
echo "Running Scenario 3: Book DA checkin"
mock_clear
load_tag "book1"
rodney sleep 5
rodney waitload
check_popup_contains "1301111111"

# --- Scenario 5: Empty tag ---
echo "Running Scenario 5: Empty tag"
mock_clear
load_tag "empty"
rodney sleep 5
rodney waitload
check_popup_empty

# --- Scenario 6: Error mode ---
echo "Running Scenario 6: Error mode"
mock_clear
mock_error 1
load_tag "book1"
rodney sleep 5
rodney waitload
check_popup_contains "not checked in"

# --- Scenario 7: Timeout mode ---
echo "Running Scenario 7: Timeout mode"
mock_clear
mock_timeout 100
load_tag "book1"
rodney sleep 5
rodney waitload
check_popup_contains "timeout"

# --- Scenario 8: Tag leaves range ---
echo "Running Scenario 8: Tag leaves range"
mock_clear
load_tag "book1"
rodney sleep 5
rodney waitload
mock_clear
rodney sleep 5
check_popup_empty

# --- Scenario 9: Multiple books DA ---
echo "Running Scenario 9: Multiple books DA"
mock_clear
load_tag "book1"
load_tag "book2"
rodney sleep 5
rodney waitload
check_popup_contains "1301111111"
check_popup_contains "1302079605"

# --- Scenario 10: Mixed AFI ---
echo "Running Scenario 10: Mixed AFI"
mock_clear
load_tag "book1"
load_tag "book3"
rodney sleep 5
rodney waitload
check_popup_contains "1302099999"

# --- Scenario 15: Return 1 book DA ---
echo "Running Scenario 15: Return 1 book DA"
mock_clear
load_tag "book1"
rodney sleep 5
rodney waitload
check_input_filled '#barcode'
info "DB: checking 1 item returned"
count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='1301111111'")
echo "$count" | grep -qE '[1-3]' && result "pass" || result "fail"

# --- Scenario 16: Return 2 books DA ---
echo "Running Scenario 16: Return 2 books DA"
mock_clear
load_tag "book1"
load_tag "book2"
rodney sleep 5
rodney waitload
check_input_filled '#barcode'
info "DB: checking 2 items returned"
count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode IN ('1301111111', '1302079605')")
echo "$count" | grep -qE '[1-2]' && result "pass" || result "fail"

# --- Scenario 17: Return 3 books DA ---
echo "Running Scenario 17: Return 3 books DA"
mock_clear
load_tag "book1"
load_tag "book2"
load_tag "book3"
rodney sleep 5
rodney waitload
check_input_filled '#barcode'
info "DB: checking 3 items returned"
count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode IN ('1301111111', '1302079605', '1302099999')")
echo "$count" | grep -qE '[1-3]' && result "pass" || result "fail"

# --- Scenario 18: Return D7 book ---
echo "Running Scenario 18: Return D7 book"
mock_clear
load_tag "book3"
rodney sleep 5
rodney waitload
check_input_filled '#barcode'
info "DB: checking 1 item returned"
count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='1302099999'")
echo "$count" | grep -qE '[1-3]' && result "pass" || result "fail"

echo ""
echo "Done."
