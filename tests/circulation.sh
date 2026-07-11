#!/bin/bash
# tests/circulation.sh — Simple linear test script for circulation

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

# --- Setup ---
mock_start
koha_login
rodney open "$KOHA_URL/circ/circulation.pl"
rodney waitload
rodney js "localStorage.removeItem('rfid_afi')"
pre_flight_check

# --- Scenario 1: Patron Scan ---
echo "Running Scenario: Patron Scan"
tab_switch "checkout"
mock_add "patron" "200000000042" "DA"
rodney sleep 2
rodney waitload
# Verify the tag was seen by checking local storage (as per user suggestion)
rodney js "JSON.parse(localStorage.getItem('rfid_afi'))['200000000042']" | grep -q "patron"
echo "Patron tag detected in localStorage."

# --- Scenario 2: Checkout Book ---
echo "Running Scenario: Checkout Book"
mock_clear
mock_add "book1" "1301111111" "DA"
rodney sleep 2
rodney waitload
# Check DB for successful checkout
check_db "SELECT COUNT(*) FROM issues WHERE itemnumber=(SELECT itemnumber FROM items WHERE barcode='1301111111')" "1"

# --- Scenario 3: Checkin Book ---
echo "Running Scenario: Checkin Book"
tab_switch "checkin"
mock_add "book1" "1301111111" "DA"
rodney sleep 2
rodney waitload
check_popup_contains "1301111111"

# --- Cleanup ---
cleanup_issues
echo "All tests completed successfully."
