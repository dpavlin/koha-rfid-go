#!/bin/bash
# tests/circulation.sh — Simple linear test script for circulation

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

# --- Helpers ---

setup() {
    echo "--------------------------------------------------"
    echo "Setting up environment..."
    mock_start
    koha_login
    rodney open "$KOHA_URL/circ/circulation.pl"
    rodney waitload
    rodney js "localStorage.removeItem('rfid_afi')"
    pre_flight_check
}

teardown() {
    cleanup_issues
    echo "--------------------------------------------------"
}

# --- Scenarios ---

# Scenario 1: Patron Scan
echo "Running Scenario: Patron Scan"
setup
tab_switch "checkout"
load_tag "patron"
rodney sleep 3
rodney waitload

# Check that tag was seen in localStorage as per user instruction
if rodney js "JSON.parse(localStorage.getItem('rfid_afi'))['200000000042']?.sec === 'patron'"; then
    echo "  OK: Patron tag detected in localStorage."
else
    echo "  DEBUG: localStorage content is: $(rodney js "localStorage.getItem('rfid_afi')")"
    fail "  FAIL: Patron tag NOT detected in localStorage."
fi

# Scenario 2: Checkout Book
echo "Running Scenario: Checkout Book"
mock_clear
load_tag "book1"
rodney sleep 3
rodney waitload
check_db "SELECT COUNT(*) FROM issues WHERE itemnumber=(SELECT itemnumber FROM items WHERE barcode='1301111111')" "1"
teardown

# Scenario 3: Checkin Book
echo "Running Scenario: Checkin Book"
setup
tab_switch "checkin"
load_tag "book1"
rodney sleep 3
rodney waitload
check_popup_contains "1301111111"
teardown

echo "All tests completed."
