#!/bin/bash
# tests/circulation.sh — Full Linear Test Suite for Circulation
# Single init/teardown, resets RFID state before each scenario.

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE="circulation"
PAGE_URL="$KOHA_URL/circ/circulation.pl"

# --- Single Initialization ---
echo "[========================================]"
echo "|  circulation"
echo "[========================================]"

rodney connect localhost:$CDP_PORT
koha_login
mock_start
rodney open "$PAGE_URL"
rodney waitload
reset_rfid_state
pre_flight_check

# Default form check
echo ""
echo "-- Default form check --"
if rodney exists 'input[name=findborrower]' 2>/dev/null; then
    pass "default checkout form (findborrower) is present"
    mock_clear
    load_tag "200000000042"
    rodney sleep 3
    if rodney visible '.patroninfo' 2>/dev/null; then
        pass "default form works — patron scan finds patron"
    else
        fail "default form not responding to RFID scan" || true
    fi
else
    fail "default checkout form not found" || true
fi
echo "-- Default form OK --"

# --- Helper: check if koha-rfid.js populated patron info in DOM ---
patron_loaded() {
    rodney exists '#circ_circulation_issue' >/dev/null 2>&1
}

# --- Helper: checkout a book (load tag, wait, verify) ---
checkout_book() {
    local barcode="$1"
    mock_clear
    load_tag "$barcode"
    rodney sleep 2
    rodney waitload
    check_koha_messages
    count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$barcode'")
    if echo "$count" | grep -qE '[1-3]'; then
        pass "barcode $barcode is checked out"
    else
        fail "barcode $barcode is NOT checked out in DB"
    fi
    check_mock_tag_security "$barcode" "D7"
}

# --- Helper: checkin a book (switch to checkin tab, load tag, verify clean) ---
checkin_book() {
    local barcode="$1"
    tab_switch "checkin"
    mock_clear
    load_tag "$barcode"
    rodney sleep 2
    rodney waitload
    check_koha_messages
    # Verify the book is no longer issued
    count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$barcode'")
    if echo "$count" | grep -q "0"; then
        pass "barcode $barcode checked in — not issued"
    else
        fail "barcode $barcode still issued in DB"
    fi
    check_mock_tag_security "$barcode" "DA"
}

# ============================================================
# PHASE 1: Isolated scenarios (no DB state changes)
# ============================================================

scenario_start 1 "No tags"
mock_clear
reset_rfid_state
tab_switch "checkout"
check_popup_empty

scenario_start 2 "Patron only"
mock_clear
reset_rfid_state
tab_switch "checkout"
load_tag "200000000042"
rodney sleep 3
rodney waitload
check_popup_contains "200000000042"

scenario_start 3 "Book DA checkin"
mock_clear
reset_rfid_state
tab_switch "checkin"
load_tag "1301111111"
rodney sleep 3
rodney waitload
check_popup_contains "1301111111"

scenario_start 4 "Book D7 renew"
mock_clear
reset_rfid_state
tab_switch "renew"
load_tag "1302099999"
rodney sleep 3
rodney waitload
check_popup_contains "1302099999"

scenario_start 5 "Empty tag"
mock_clear
reset_rfid_state
tab_switch "checkout"
load_tag "empty"
rodney sleep 3
check_popup_empty

scenario_start 7 "Timeout mode"
rfid_pause
mock_clear
reset_rfid_state
mock_timeout 100
load_tag "1301111111"
rfid_resume
rodney sleep 3
check_popup_contains "timeout"

scenario_start 8 "Tag leaves range"
mock_clear
reset_rfid_state
tab_switch "checkout"
load_tag "1301111111"
rodney sleep 3
mock_clear
rodney sleep 3
check_popup_empty

# ============================================================
# PHASE 2: Sequential checkout scenarios (11-13, 15)
# Each followed by checkin to restore clean state
# ============================================================

scenario_start 11 "Patron + 1 book DA"
reset_rfid_state
tab_switch "checkout"

mock_clear
load_tag "200000000042"
rodney sleep 3
rodney waitload

tab_switch "checkout"
checkout_book "1301111111"

echo "  -- Checkin 1301111111 --"
checkin_book "1301111111"

scenario_start 12 "Patron + 2 books DA"
reset_rfid_state
tab_switch "checkout"

mock_clear
load_tag "200000000042"
rodney sleep 3
rodney waitload

tab_switch "checkout"
checkout_book "1301111111"
checkout_book "1302079605"

echo "  -- Checkin 1301111111 and 1302079605 --"
checkin_book "1301111111"
checkin_book "1302079605"

scenario_start 13 "Patron + 3 books DA"
reset_rfid_state
tab_switch "checkout"

mock_clear
load_tag "200000000042"
rodney sleep 3
rodney waitload

tab_switch "checkout"
TAG_SECURITY["1302099999"]="DA"
checkout_book "1301111111"
checkout_book "1302079605"
checkout_book "1302099999"

echo "  -- Checkin 1301111111, 1302079605, 1302099999 --"
checkin_book "1301111111"
checkin_book "1302079605"
checkin_book "1302099999"
TAG_SECURITY["1302099999"]="D7"

scenario_start 15 "Batch checkout (Patron + 3 books simultaneously)"
reset_rfid_state
tab_switch "checkout"

mock_clear
load_tag "200000000042"
TAG_SECURITY["1302099999"]="DA"
load_tag "1301111111"
load_tag "1302079605"
load_tag "1302099999"

# Wait for patron card scan & checkout of book 1, 2, 3
rodney sleep 25
check_koha_messages
TAG_SECURITY["1302099999"]="D7" # restore

# Verify all 3 books are checked out in DB and updated to D7 on mock RFID reader
for bk in 1301111111 1302079605 1302099999; do
    count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bk'")
    if echo "$count" | grep -qE '[1-3]'; then
        pass "Batch checkout: $bk is checked out in DB"
    else
        fail "Batch checkout: $bk is NOT checked out in DB"
    fi
    check_mock_tag_security "$bk" "D7"
done

echo "  -- Checkin 1301111111, 1302079605, 1302099999 --"
checkin_book "1301111111"
checkin_book "1302079605"
checkin_book "1302099999"

# ============================================================
# PHASE 3: D7 book test (14)
# ============================================================

scenario_start 14 "Patron + 1 book D7"
reset_rfid_state
tab_switch "checkout"

mock_clear
load_tag "200000000042"
rodney sleep 3
rodney waitload
rodney sleep 2

tab_switch "checkout"

# 1302099999 is D7 (on loan) — should show "not checked in"
mock_clear
load_tag "1302099999"
rodney sleep 3
check_popup_contains "not checked in"

# ============================================================
# Teardown
# ============================================================
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

test_summary
