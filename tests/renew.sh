#!/bin/bash
# tests/renew.sh — Linear test suite for renew.pl
# Single initialization, no JSON parsing.

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE_URL="$KOHA_URL/circ/renew.pl"

# --- Single Initialization ---
echo "[════════════════════════════════════════]"
echo "|  renew"
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

# --- Scenario 4: Book D7 renew ---
echo "Running Scenario 4: Book D7 renew"
mock_clear
load_tag "book3"
rodney sleep 5
rodney waitload
check_popup_contains "1302099999"

# --- Scenario 5: Empty tag ---
echo "Running Scenario 5: Empty tag"
mock_clear
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
load_tag "book1"
rodney sleep 5
rodney waitload
mock_clear
rodney sleep 5
check_popup_empty

# --- Scenario 19: Renew D7 book ---
echo "Running Scenario 19: Renew D7 book"
mock_clear
load_tag "book3"
rodney sleep 5
rodney waitload
check_input_filled '#ren_barcode'
check_db "SELECT renews FROM issues WHERE itemnumber=(SELECT itemnumber FROM items WHERE barcode='1302099999')" "1" && result "pass" || result "fail"

# --- Scenario 20: Renew DA book ---
echo "Running Scenario 20: Renew DA book"
mock_clear
load_tag "book1"
rodney sleep 5
rodney waitload
check_input_filled '#ren_barcode'
check_popup_contains "not on loan" && result "pass" || result "fail"

echo ""
echo "Done."
