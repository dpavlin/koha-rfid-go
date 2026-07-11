#!/bin/bash
# tests/circulation.sh — Full Linear Test Suite for Circulation

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

# --- Helpers ---

setup() {
    echo "--------------------------------------------------"
    echo "Setting up environment..."
    mock_start
    koha_login
    rodney open "$KOHA_URL/circ/circulation.pl?cb=$(date +%s)"
    rodney waitload
    rodney js "localStorage.clear(), location.reload()"
    rodney waitload
    rodney sleep 2
    pre_flight_check
}

teardown() {
    cleanup_issues
    echo "--------------------------------------------------"
}

# --- Scenarios ---

# 1. No tags
echo "Running Scenario 1: No tags"
setup
tab_switch "checkout"
mock_clear
rodney sleep 3
check_popup_empty
teardown

# 2. Patron only
echo "Running Scenario 2: Patron only"
setup
tab_switch "checkout"
load_tag "patron"
rodney sleep 5
rodney waitload
check_input_filled "input[name=findborrower]"
teardown

# 3. Book DA checkin
echo "Running Scenario 3: Book DA checkin"
setup
tab_switch "checkin"
mock_clear
load_tag "book1"
rodney sleep 5
rodney waitload
check_popup_contains "1301111111"
teardown

# 4. Book D7 renew
echo "Running Scenario 4: Book D7 renew"
setup
tab_switch "renew"
mock_clear
load_tag "book3"
rodney sleep 5
rodney waitload
check_input_filled "#ren_barcode"
teardown

# 5. Empty tag
echo "Running Scenario 5: Empty tag"
setup
tab_switch "checkout"
mock_clear
load_tag "empty"
rodney sleep 5
rodney waitload
check_popup_empty
teardown

# 6. Error mode
echo "Running Scenario 6: Error mode"
setup
tab_switch "returns"
mock_error 1
load_tag "book1"
rodney sleep 5
rodney waitload
check_popup_contains "not checked in"
teardown

# 7. Timeout mode
echo "Running Scenario 7: Timeout mode"
setup
tab_switch "returns"
mock_timeout 100
load_tag "book1"
rodney sleep 5
rodney waitload
check_popup_contains "timeout"
teardown

# 8. Tag leaves range
echo "Running Scenario 8: Tag leaves range"
setup
tab_switch "checkout"
load_tag "book1"
rodney sleep 5
rodney waitload
mock_clear
rodney sleep 5
check_popup_empty
teardown

# 9. Multiple books DA
echo "Running Scenario 9: Multiple books DA"
setup
tab_switch "checkout"
load_tag "book1"
load_tag "book2"
rodney sleep 5
rodney waitload
check_popup_contains "1301111111"
check_popup_contains "1302079605"
teardown

# 10. Mixed AFI
echo "Running Scenario 10: Mixed AFI"
setup
tab_switch "checkout"
load_tag "book1"
load_tag "book3"
rodney sleep 5
rodney waitload
check_popup_contains "1302099999"
teardown

# 11. Patron + 1 book DA (Checkout)
echo "Running Scenario 11: Patron + 1 book DA"
setup
tab_switch "checkout"
load_tag "patron"
rodney sleep 5
rodney waitload
if [[ "$(rodney js "document.querySelector('input[name=findborrower]')?.value")" == "" ]]; then
    rodney click 'input.submit'
    rodney waitload
    rodney sleep 2
fi
check_input_filled "input[name=findborrower]"

mock_clear
load_tag "book1"
rodney sleep 5
rodney waitload
check_db "SELECT COUNT(*) FROM issues WHERE itemnumber=(SELECT itemnumber FROM items WHERE barcode='1301111111')" "1"
teardown

# 12. Patron + 2 books DA
echo "Running Scenario 12: Patron + 2 books DA"
setup
tab_switch "checkout"
load_tag "patron"
rodney sleep 5
rodney waitload
if [[ "$(rodney js "document.querySelector('input[name=findborrower]')?.value")" == "" ]]; then
    rodney click 'input.submit'
    rodney waitload
    rodney sleep 2
fi
check_input_filled "input[name=findborrower]"

mock_clear
load_tag "book1"
load_tag "book2"
rodney sleep 5
rodney waitload
check_db "SELECT COUNT(*) FROM issues WHERE itemnumber IN (SELECT itemnumber FROM items WHERE barcode IN ('1301111111', '1302079605'))" "2"
teardown

# 13. Patron + 3 books DA
echo "Running Scenario 13: Patron + 3 books DA"
setup
tab_switch "checkout"
load_tag "patron"
rodney sleep 5
rodney waitload
if [[ "$(rodney js "document.querySelector('input[name=findborrower]')?.value")" == "" ]]; then
    rodney click 'input.submit'
    rodney waitload
    rodney sleep 2
fi
check_input_filled "input[name=findborrower]"

mock_clear
load_tag "book1"
load_tag "book2"
load_tag "book3"
rodney sleep 5
rodney waitload
check_db "SELECT COUNT(*) FROM issues WHERE itemnumber IN (SELECT itemnumber FROM items WHERE barcode IN ('1301111111', '1302079605', '1302099999'))" "3"
teardown

# 14. Patron + 1 book D7
echo "Running Scenario 14: Patron + 1 book D7"
setup
tab_switch "checkout"
load_tag "patron"
rodney sleep 5
rodney waitload
if [[ "$(rodney js "document.querySelector('input[name=findborrower]')?.value")" == "" ]]; then
    rodney click 'input.submit'
    rodney waitload
    rodney sleep 2
fi
check_input_filled "input[name=findborrower]"

mock_clear
load_tag "book3"
rodney sleep 5
rodney waitload
check_popup_contains "not checked in"
teardown

echo "--------------------------------------------------"
echo "All tests completed."
