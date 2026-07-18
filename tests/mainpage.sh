#!/bin/bash
# tests/mainpage.sh — Linear test suite for mainpage.pl
# Single initialization, no JSON parsing.

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE="mainpage"
PAGE_URL="$KOHA_URL/mainpage.pl"

# --- Single Initialization ---
echo "[========================================]"
echo "|  mainpage"
echo "[========================================]"

rodney connect localhost:$CDP_PORT
koha_login
mock_start
rodney open "$PAGE_URL"
rodney waitload

# --- Scenario 1: No tags ---
scenario_start 1 "No tags"
mock_clear
check_popup_empty

# --- Scenario 5: Empty tag ---
scenario_start 5 "Empty tag"
mock_clear
load_tag "empty"
rodney sleep 2
check_popup_empty

# --- Scenario 6: Error mode ---
scenario_start 6 "Error mode"
rfid_pause
mock_clear
mock_error 3
load_tag "1301111111"
rfid_resume
rodney sleep 3
check_popup_contains "error"

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

# --- Scenario 10: Mixed AFI ---
scenario_start 10 "Mixed AFI"
mock_clear
load_tag "1301111111"
load_tag "1302099999"
rodney sleep 3
check_popup_contains "1301111111"

echo ""
echo "Done."
