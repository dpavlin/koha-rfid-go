#!/bin/bash
# tests/circulation.sh — Full Linear Test Suite for Circulation
# Single initialization, no per-scenario setup/teardown, no JSON parsing.

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE_URL="$KOHA_URL/circ/circulation.pl"

# --- Single Initialization ---
echo "[════════════════════════════════════════]"
echo "|  circulation"
echo "[════════════════════════════════════════]"

rodney connect localhost:$CDP_PORT
koha_login
mock_start
rodney open "$PAGE_URL"
rodney waitload
rodney js "localStorage.removeItem('rfid_afi')"
pre_flight_check

# Default form check
echo ""
echo "-- Default form check --"
if rodney exists 'input[name=findborrower]' 2>/dev/null; then
    pass "default checkout form (findborrower) is present"
    mock_clear
    load_tag "patron"
    sleep 3
    if rodney visible '.patroninfo' 2>/dev/null; then
        pass "default form works — patron scan finds patron"
    else
        fail "default form not responding to RFID scan" || true
        rodney js "localStorage.removeItem('rfid_afi')"
    fi
else
    fail "default checkout form not found" || true
    rodney js "localStorage.removeItem('rfid_afi')"
fi
echo "-- Default form OK --"

# --- Scenario 1: No tags ---
echo "Running Scenario 1: No tags"
mock_clear
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "checkout"
rodney sleep 3
check_popup_empty

# --- Scenario 2: Patron only ---
echo "Running Scenario 2: Patron only"
mock_clear
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "checkout"
load_tag "patron"
rodney sleep 5
rodney waitload
check_popup_contains "200000000042"

# --- Scenario 3: Book DA checkin ---
echo "Running Scenario 3: Book DA checkin"
mock_clear
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "checkin"
load_tag "book1"
rodney sleep 5
rodney waitload
check_popup_contains "1301111111"

# --- Scenario 4: Book D7 renew ---
echo "Running Scenario 4: Book D7 renew"
mock_clear
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "renew"
load_tag "book3"
rodney sleep 5
rodney waitload
check_popup_contains "1302099999"

# --- Scenario 5: Empty tag ---
echo "Running Scenario 5: Empty tag"
mock_clear
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "checkout"
load_tag "empty"
rodney sleep 5
rodney waitload
check_popup_empty

# --- Scenario 6: Error mode ---
echo "Running Scenario 6: Error mode"
mock_clear
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "returns"
mock_error 1
load_tag "book1"
rodney sleep 5
rodney waitload
check_popup_contains "not checked in"

# --- Scenario 7: Timeout mode ---
echo "Running Scenario 7: Timeout mode"
mock_clear
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "returns"
mock_timeout 100
load_tag "book1"
rodney sleep 5
rodney waitload
check_popup_contains "timeout"

# --- Scenario 8: Tag leaves range ---
echo "Running Scenario 8: Tag leaves range"
mock_clear
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "checkout"
load_tag "book1"
rodney sleep 5
rodney waitload
mock_clear
rodney sleep 5
check_popup_empty

# --- Scenario 9: Multiple books DA ---
echo "Running Scenario 9: Multiple books DA"
mock_clear
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "checkout"
load_tag "book1"
load_tag "book2"
rodney sleep 5
rodney waitload
check_popup_contains "1301111111"
check_popup_contains "1302079605"

# --- Scenario 10: Mixed AFI ---
echo "Running Scenario 10: Mixed AFI"
mock_clear
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "checkout"
load_tag "book1"
load_tag "book3"
rodney sleep 5
rodney waitload
check_popup_contains "1302099999"

# --- Scenario 11: Patron + 1 book DA (Checkout) ---
echo "Running Scenario 11: Patron + 1 book DA"
rodney open "$PAGE_URL"
rodney waitload
rodney sleep 2
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "checkout"

mock_clear
load_tag "patron"
rodney sleep 5
rodney waitload
rodney sleep 2

# Check if patron input was filled automatically
if [[ "$(rodney js "document.querySelector('input[name=findborrower]')?.value")" == "" ]]; then
    info "input is empty, submitting manually..."
    rodney js "window.location.href = window.location.pathname + '?findborrower=200000000042&Submit=Submit'"
    rodney waitload
    rodney sleep 2
else
    info "input is already filled by rfid_scan"
    rodney waitload
    rodney sleep 2
fi

tab_switch "checkout"

# Book 1 — load one by one
mock_clear
load_tag "book1"
rodney sleep 3
rodney waitload
rodney sleep 2
bc="1301111111"
count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bc'")
if echo "$count" | grep -qE '[1-3]'; then
    pass "book1 is checked out"
else
    fail "book1 is NOT checked out in DB"
fi

# Verify final DB state
db_check="SELECT COUNT(*) FROM issues WHERE borrowernumber=(SELECT borrowernumber FROM borrowers WHERE cardnumber='200000000042') AND itemnumber=(SELECT itemnumber FROM items WHERE barcode='1301111111')"
count=$(koha_mysql "$db_check")
if echo "$count" | grep -qE '[1-3]'; then
    pass "DB: 1"
else
    fail "DB expected 1 but got: $count"
fi

# --- Scenario 12: Patron + 2 books DA ---
echo "Running Scenario 12: Patron + 2 books DA"
rodney open "$PAGE_URL"
rodney waitload
rodney sleep 2
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "checkout"

mock_clear
load_tag "patron"
rodney sleep 5
rodney waitload
rodney sleep 2

if [[ "$(rodney js "document.querySelector('input[name=findborrower]')?.value")" == "" ]]; then
    info "input is empty, submitting manually..."
    rodney js "window.location.href = window.location.pathname + '?findborrower=200000000042&Submit=Submit'"
    rodney waitload
    rodney sleep 2
else
    info "input is already filled by rfid_scan"
    rodney waitload
    rodney sleep 2
fi

tab_switch "checkout"

# Book 1
mock_clear
load_tag "book1"
rodney sleep 3
rodney waitload
rodney sleep 2
bc="1301111111"
count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bc'")
if echo "$count" | grep -qE '[1-3]'; then
    pass "book1 is checked out"
else
    fail "book1 is NOT checked out in DB"
fi

# Book 2
mock_clear
load_tag "book2"
rodney sleep 3
rodney waitload
rodney sleep 2
bc="1302079605"
count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bc'")
if echo "$count" | grep -qE '[1-3]'; then
    pass "book2 is checked out"
else
    fail "book2 is NOT checked out in DB"
fi

# --- Scenario 13: Patron + 3 books DA ---
echo "Running Scenario 13: Patron + 3 books DA"
rodney open "$PAGE_URL"
rodney waitload
rodney sleep 2
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "checkout"

mock_clear
load_tag "patron"
rodney sleep 5
rodney waitload
rodney sleep 2

if [[ "$(rodney js "document.querySelector('input[name=findborrower]')?.value")" == "" ]]; then
    info "input is empty, submitting manually..."
    rodney js "window.location.href = window.location.pathname + '?findborrower=200000000042&Submit=Submit'"
    rodney waitload
    rodney sleep 2
else
    info "input is already filled by rfid_scan"
    rodney waitload
    rodney sleep 2
fi

tab_switch "checkout"

# Book 1
mock_clear
load_tag "book1"
rodney sleep 3
rodney waitload
rodney sleep 2
bc="1301111111"
count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bc'")
if echo "$count" | grep -qE '[1-3]'; then
    pass "book1 is checked out"
else
    fail "book1 is NOT checked out in DB"
fi

# Book 2
mock_clear
load_tag "book2"
rodney sleep 3
rodney waitload
rodney sleep 2
bc="1302079605"
count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bc'")
if echo "$count" | grep -qE '[1-3]'; then
    pass "book2 is checked out"
else
    fail "book2 is NOT checked out in DB"
fi

# Book 3 (D7, on loan — won't checkout, skip DB check)
mock_clear
load_tag "book3"
rodney sleep 3
rodney waitload
rodney sleep 2

# --- Scenario 14: Patron + 1 book D7 ---
echo "Running Scenario 14: Patron + 1 book D7"
rodney open "$PAGE_URL"
rodney waitload
rodney sleep 2
rodney js "localStorage.removeItem('rfid_afi')"
tab_switch "checkout"

mock_clear
load_tag "patron"
rodney sleep 5
rodney waitload
rodney sleep 2

if [[ "$(rodney js "document.querySelector('input[name=findborrower]')?.value")" == "" ]]; then
    info "input is empty, submitting manually..."
    rodney js "window.location.href = window.location.pathname + '?findborrower=200000000042&Submit=Submit'"
    rodney waitload
    rodney sleep 2
else
    info "input is already filled by rfid_scan"
    rodney waitload
    rodney sleep 2
fi

tab_switch "checkout"

# Book 3 (D7)
mock_clear
load_tag "book3"
rodney sleep 3
rodney waitload
rodney sleep 2
check_popup_contains "not checked in"

# --- Single Teardown ---
cleanup_issues

echo ""
echo "-- Post-flight check --"
for bc in 1301111111 1302079605 1302099999; do
    issued=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bc'" || echo "")
    if echo "$issued" | grep -q "0"; then
        pass "barcode $bc is not issued — clean"
    else
        fail "barcode $bc is still issued"
    fi
done
echo "-- Post-flight done --"

echo ""
echo "Done."
