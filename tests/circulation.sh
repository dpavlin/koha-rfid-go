#!/bin/bash
# tests/circulation.sh — Full Linear Test Suite for Circulation
# Single init/teardown, resets RFID state before each scenario.

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE="circulation"
PAGE_URL="$KOHA_URL/circ/circulation.pl"

# --- Single Initialization (browser state changes happen here only) ---
echo "[════════════════════════════════════════]"
echo "|  circulation"
echo "[════════════════════════════════════════]"

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
    load_tag "patron"
    sleep 3
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
    local bc="$1" label="$2"
    mock_clear
    load_tag "$bc"
    rodney sleep 10
    count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bc'")
    if echo "$count" | grep -qE '[1-3]'; then
        pass "$label is checked out"
    else
        fail "$label is NOT checked out in DB"
    fi
}

# --- Helper: checkin a book (switch to checkin tab, load tag, verify clean) ---
checkin_book() {
    local bc="$1" label="$2"
    tab_switch "checkin"
    mock_clear
    load_tag "$bc"
    rodney sleep 10
    # Verify the book is no longer issued
    count=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bc'")
    if echo "$count" | grep -q "0"; then
        pass "$label checked in — not issued"
    else
        fail "$label still issued in DB"
    fi
}

# ============================================================
# PHASE 1: Isolated scenarios (no DB state changes)
# ============================================================

scenario_start 1 "No tags"
mock_clear
reset_rfid_state
tab_switch "checkout"
rodney sleep 3
check_popup_empty

scenario_start 2 "Patron only"
mock_clear
reset_rfid_state
tab_switch "checkout"
load_tag "patron"
rodney sleep 10
check_popup_contains "200000000042"

scenario_start 3 "Book DA checkin"
mock_clear
reset_rfid_state
tab_switch "checkin"
load_tag "book1"
rodney sleep 10
check_popup_contains "1301111111"

scenario_start 4 "Book D7 renew"
mock_clear
reset_rfid_state
tab_switch "renew"
load_tag "book3"
rodney sleep 10
check_popup_contains "1302099999"

scenario_start 5 "Empty tag"
mock_clear
reset_rfid_state
tab_switch "checkout"
load_tag "empty"
rodney sleep 10
check_popup_empty

scenario_start 7 "Timeout mode"
mock_clear
reset_rfid_state
mock_timeout 100
load_tag "book1"
rodney sleep 10
check_popup_contains "timeout"

scenario_start 8 "Tag leaves range"
mock_clear
reset_rfid_state
tab_switch "checkout"
load_tag "book1"
rodney sleep 10
mock_clear
rodney sleep 10
check_popup_empty

# ============================================================
# PHASE 2: Sequential checkout scenarios (11-13)
# Each followed by checkin to restore clean state
# ============================================================

scenario_start 11 "Patron + 1 book DA"
rodney open "$PAGE_URL"
rodney waitload
rodney sleep 2
reset_rfid_state
tab_switch "checkout"

mock_clear
load_tag "patron"
rodney sleep 15

# Check if koha-rfid.js populated patron info in DOM
if patron_loaded; then
    info "patron info loaded in DOM by rfid_scan"
else
    info "patron info not in DOM, submitting search manually..."
    rodney js "window.location.href = window.location.pathname + '?findborrower=200000000042&Submit=Submit'"
    rodney waitload
    rodney sleep 5
fi

tab_switch "checkout"
rodney sleep 2
checkout_book "book1" "book1"

echo "  -- Checkin book1 --"
checkin_book "book1" "book1"

scenario_start 12 "Patron + 2 books DA"
rodney open "$PAGE_URL"
rodney waitload
rodney sleep 2
reset_rfid_state
tab_switch "checkout"

mock_clear
load_tag "patron"
rodney sleep 15

if patron_loaded; then
    info "patron info loaded in DOM by rfid_scan"
else
    info "patron info not in DOM, submitting search manually..."
    rodney js "window.location.href = window.location.pathname + '?findborrower=200000000042&Submit=Submit'"
    rodney waitload
    rodney sleep 5
fi

tab_switch "checkout"
rodney sleep 2
checkout_book "book1" "book1"
checkout_book "book2" "book2"

echo "  -- Checkin book1 and book2 --"
checkin_book "book1" "book1"
checkin_book "book2" "book2"

scenario_start 13 "Patron + 3 books DA"
rodney open "$PAGE_URL"
rodney waitload
rodney sleep 2
reset_rfid_state
tab_switch "checkout"

mock_clear
load_tag "patron"
rodney sleep 15

if patron_loaded; then
    info "patron info loaded in DOM by rfid_scan"
else
    info "patron info not in DOM, submitting search manually..."
    rodney js "window.location.href = window.location.pathname + '?findborrower=200000000042&Submit=Submit'"
    rodney waitload
    rodney sleep 5
fi

tab_switch "checkout"
rodney sleep 2
checkout_book "book1" "book1"
checkout_book "book2" "book2"
checkout_book "book3" "book3"

echo "  -- Checkin book1, book2, book3 --"
checkin_book "book1" "book1"
checkin_book "book2" "book2"
checkin_book "book3" "book3"

# ============================================================
# PHASE 3: D7 book test (14)
# ============================================================

scenario_start 14 "Patron + 1 book D7"
rodney open "$PAGE_URL"
rodney waitload
rodney sleep 2
reset_rfid_state
tab_switch "checkout"

mock_clear
load_tag "patron"
rodney sleep 15

if patron_loaded; then
    info "patron info loaded in DOM by rfid_scan"
else
    info "patron info not in DOM, submitting search manually..."
    rodney js "window.location.href = window.location.pathname + '?findborrower=200000000042&Submit=Submit'"
    rodney waitload
    rodney sleep 5
fi

tab_switch "checkout"
rodney sleep 2

# Book3 is D7 (on loan) — should show "not checked in"
mock_clear
load_tag "book3"
rodney sleep 10
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
