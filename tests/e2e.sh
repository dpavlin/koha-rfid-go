#!/bin/bash
# e2e.sh — RFID E2E test driver
# Runs all page-specific test scripts in dependency order.
# Usage:
#   ./tests/e2e.sh                          # run all pages
#   ./tests/e2e.sh mainpage                 # run only mainpage

set -eu
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR/.."
source "$SCRIPT_DIR/lib.sh"

PAGE_FILTER="${1:-}"
SCENARIO_FILTER="${2:-}"

PAGE_ORDER=("mainpage" "circulation-home" "circulation" "returns" "renew")
declare -A PAGE_SCRIPT
PAGE_SCRIPT["mainpage"]="./tests/mainpage.sh"
PAGE_SCRIPT["circulation-home"]="./tests/circulation-home.sh"
PAGE_SCRIPT["circulation"]="./tests/circulation.sh"
PAGE_SCRIPT["returns"]="./tests/returns.sh"
PAGE_SCRIPT["renew"]="./tests/renew.sh"

MOCK_URL="${MOCK_URL:-https://localhost:9000}"
RESULTS_FILE="${RESULTS_FILE:-/tmp/rfid-test-results}"

info() { echo -e "$*"; }

ensure_mock_running() {
    mock_start || {
        echo "ERROR: mock server did not start"
        exit 1
    }
}

run_page() {
    local page="$1" script
    script="${PAGE_SCRIPT[$page]}"
    if [ ! -x "$script" ]; then
        echo "  [error] no script for page '$page'"
        return 1
    fi
    echo ""
    echo "==================================================================="
    echo "  Running $page: $script $SCENARIO_FILTER"
    echo "==================================================================="
    bash "$script" "$SCENARIO_FILTER"
}

main() {
    ensure_mock_running

    local failed=0
    if [ -n "$PAGE_FILTER" ]; then
        if [ "$PAGE_FILTER" = "" ] && [ -n "$SCENARIO_FILTER" ]; then
            for page in "${PAGE_ORDER[@]}"; do
                run_page "$page" "$SCENARIO_FILTER" || failed=1
            done
        else
            run_page "$PAGE_FILTER" "$SCENARIO_FILTER" || failed=1
        fi
    else
        for page in "${PAGE_ORDER[@]}"; do
            run_page "$page" "" || failed=1
        done
    fi

    echo ""
    echo "==================================================================="
    if [ $failed -eq 0 ]; then
        echo "  E2E Result: ALL PAGE SUITES PASSED"
        return 0
    else
        echo "  E2E Result: SOME PAGE SUITES FAILED"
        return 1
    fi
}

main
