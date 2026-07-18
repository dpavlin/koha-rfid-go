#!/bin/bash
# tests/renew.sh — Linear test suite for renew.pl
# Single initialization, no JSON parsing.

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE="renew"
PAGE_URL="$KOHA_URL/circ/renew.pl"

# --- Single Initialization ---
echo "[========================================]"
echo "|  renew"
echo "[========================================]"

rodney connect localhost:$CDP_PORT
koha_login
cleanup_issues
mock_start
rodney open "$PAGE_URL"
rodney waitload

# --- Scenario 1: No tags ---
scenario_start 1 "No tags"
mock_clear
check_popup_empty

# --- Scenario 4: Book D7 renew ---
scenario_start 4 "Book D7 renew"
mock_clear
load_tag "1302099999"
rodney sleep 3
check_popup_contains "1302099999"

# --- Scenario 5: Empty tag ---
scenario_start 5 "Empty tag"
mock_clear
load_tag "empty"
rodney sleep 2
check_popup_empty

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
reset_rfid_state
load_tag "1301111111"
rodney sleep 3
mock_clear
rodney sleep 3
check_popup_empty

# --- Scenario 19: Renew D7 book ---
db_checkout "200000000042" "1302099999"

scenario_start 19 "Renew D7 book"
mock_clear
load_tag "1302099999"
rodney sleep 1.5
mock_clear
check_db "SELECT renewals FROM issues WHERE itemnumber=(SELECT itemnumber FROM items WHERE barcode='1302099999')" "1"

# --- Scenario 20: Renew DA book ---
scenario_start 20 "Renew DA book"
mock_clear
load_tag "1301111111"
check_popup_contains "not on loan"

cleanup_issues

echo ""
echo "Done."
