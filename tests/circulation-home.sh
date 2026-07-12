#!/bin/bash
# tests/circulation-home.sh — Linear test suite for circulation-home.pl
# Single initialization, no JSON parsing.

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE_URL="$KOHA_URL/circ/circulation-home.pl"

# --- Single Initialization ---
echo "[════════════════════════════════════════]"
echo "|  circulation-home"
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

# --- Scenario 2: Patron only ---
echo "Running Scenario 2: Patron only"
mock_clear
tab_switch "checkout"
load_tag "patron"
rodney sleep 5
rodney waitload
check_popup_contains "200000000042"

# --- Scenario 3: Book DA checkin ---
echo "Running Scenario 3: Book DA checkin"
mock_clear
tab_switch "checkin"
load_tag "book1"
rodney sleep 5
rodney waitload
check_popup_contains "1301111111"

# --- Scenario 4: Book D7 renew ---
echo "Running Scenario 4: Book D7 renew"
mock_clear
tab_switch "renew"
load_tag "book3"
rodney sleep 5
rodney waitload
check_popup_contains "1302099999"

# --- Scenario 5: Empty tag ---
echo "Running Scenario 5: Empty tag"
mock_clear
tab_switch "checkout"
load_tag "empty"
rodney sleep 5
rodney waitload
check_popup_empty

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
tab_switch "checkout"
load_tag "book1"
rodney sleep 5
rodney waitload
mock_clear
rodney sleep 5
check_popup_empty

# --- Scenario 21: Patron on circ-home ---
echo "Running Scenario 21: Patron on circ-home"
mock_clear
tab_switch "checkout"
load_tag "patron"
rodney sleep 5
rodney waitload
check_input_filled 'input[name=findborrower]'

# --- Scenario 22: Book checkin on circ-home ---
echo "Running Scenario 22: Book checkin on circ-home"
mock_clear
tab_switch "checkin"
load_tag "book1"
rodney sleep 5
rodney waitload
check_input_filled '#ret_barcode'

# --- Scenario 23: Book renew on circ-home ---
echo "Running Scenario 23: Book renew on circ-home"
mock_clear
tab_switch "renew"
load_tag "book3"
rodney sleep 5
rodney waitload
check_input_filled '#ren_barcode'

echo ""
echo "Done."
