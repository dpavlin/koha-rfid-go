#!/bin/bash
# e2e.sh — RFID E2E test driver
# Orchestrates per-page test scripts.
# Usage:
#   ./tests/e2e.sh                          # run all pages in dependency order
#   ./tests/e2e.sh circulation              # run only circulation.pl
#   ./tests/e2e.sh circulation 11           # run scenario 11 on circulation.pl
#   ./tests/e2e.sh '' 11                    # run scenario 11 on all pages
#   ./tests/e2e.sh list                     # show available pages and scenarios
set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR/.."
source "$SCRIPT_DIR/lib.sh"

# ──────────────────────────────────────────────────────────────────
# Config
# ──────────────────────────────────────────────────────────────────
MOCK_URL="${MOCK_URL:-https://localhost:9000}"
RESULTS_FILE="${RESULTS_FILE:-/tmp/rfid-test-results}"
PAGE_FILTER="${1:-}"
SCENARIO_FILTER="${2:-}"

# Propagate env to child scripts
export SKIP_KOHA_LOGIN

# ──────────────────────────────────────────────────────────────────
# Pages in dependency order
# ──────────────────────────────────────────────────────────────────
PAGE_ORDER=("mainpage" "circulation-home" "circulation" "returns" "renew")

declare -A PAGE_SCRIPT
PAGE_SCRIPT["mainpage"]="./tests/test-mainpage.sh"
PAGE_SCRIPT["circulation-home"]="./tests/test-circulation-home.sh"
PAGE_SCRIPT["circulation"]="./tests/test-circulation.sh"
PAGE_SCRIPT["returns"]="./tests/test-returns.sh"
PAGE_SCRIPT["renew"]="./tests/test-renew.sh"

# ──────────────────────────────────────────────────────────────────
# Helpers
# ──────────────────────────────────────────────────────────────────
info() { echo -e "$*"; }

list_scenarios() {
    echo ""
    echo "Available pages: ${PAGE_ORDER[*]}"
    echo ""
    echo "Scenarios:"
    cat tests/scenarios.json | jq -r '.[] | "  \(.id): \(.name) — pages: \(.pages | join(", "))"'
    echo ""
}

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
    echo "═══════════════════════════════════════════════════════════════════"
    echo "  Running $page: $script $SCENARIO_FILTER"
    echo "═══════════════════════════════════════════════════════════════════"
    bash "$script" "$SCENARIO_FILTER"
}

# ──────────────────────────────────────────────────────────────────
# Main
# ──────────────────────────────────────────────────────────────────
main() {
    # Ensure mock server is running
    ensure_mock_running

    if [ -n "$PAGE_FILTER" ]; then
        # Run specific page(s)
        if [ "$PAGE_FILTER" = "list" ]; then
            list_scenarios
            return
        fi
        if [ "$PAGE_FILTER" = "" ] && [ -n "$SCENARIO_FILTER" ]; then
            # Run scenario on all pages
            for page in "${PAGE_ORDER[@]}"; do
                run_page "$page" "$SCENARIO_FILTER"
            done
        else
            run_page "$PAGE_FILTER" "$SCENARIO_FILTER"
        fi
    else
        # Run all pages in dependency order
        for page in "${PAGE_ORDER[@]}"; do
            run_page "$page" ""
        done
    fi

    echo ""
    echo "═══════════════════════════════════════════════════════════════════"
    echo "Results: $RESULTS_FILE"
    if [ -f "$RESULTS_FILE" ]; then
        echo ""
        cat "$RESULTS_FILE"
        echo ""
        echo "Summary:"
        local pass fail skip
        pass=$(grep -c "=pass" "$RESULTS_FILE" 2>/dev/null || echo 0)
        fail=$(grep -c "=fail" "$RESULTS_FILE" 2>/dev/null || echo 0)
        skip=$(grep -c "=skip" "$RESULTS_FILE" 2>/dev/null || echo 0)
        echo "  pass=$pass fail=$fail skip=$skip"
    fi
}

main
