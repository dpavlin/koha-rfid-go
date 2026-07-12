#!/bin/bash
# tests/mainpage.sh — Linear test suite for mainpage.pl
# Single initialization, no JSON parsing.

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

PAGE_URL="$KOHA_URL/mainpage.pl"

# --- Single Initialization ---
echo "[════════════════════════════════════════]"
echo "|  mainpage"
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

# --- Scenario 5: Empty tag ---
echo "Running Scenario 5: Empty tag"
mock_clear
load_tag "empty"
rodney sleep 5
rodney waitload
check_popup_empty

# --- Scenario 6: Error mode ---
echo "Running Scenario 6: Error mode"
mock_clear
mock_error 1
load_tag "book1"
rodney sleep 5
rodney waitload
check_popup_contains "not checked in"

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

# --- Scenario 10: Mixed AFI ---
echo "Running Scenario 10: Mixed AFI"
mock_clear
load_tag "book1"
load_tag "book3"
rodney sleep 5
rodney waitload
check_popup_contains "1302099999"

echo ""
echo "Done."
