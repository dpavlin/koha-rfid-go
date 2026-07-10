# lib.sh — shared functions for RFID E2E test scripts
# Source this in each page test script.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

# Config
source /home/dpavlin/koha-dev.env
export PATH="$PATH:/home/dpavlin/.local/bin"
CDP_PORT="${CDP_PORT:-9333}"
KOHA_URL="${KOHA_URL:-https://ffzg.koha-dev.rot13.org:8443/cgi-bin/koha}"
KOHA_USER="${KOHA_USER:-}"
KOHA_PASS="${KOHA_PASS:-}"
[ -z "${SKIP_KOHA_LOGIN:-}" ] && [ -z "$KOHA_USER" ] && { echo "ERROR: set KOHA_USER or SKIP_KOHA_LOGIN"; exit 1; }
[ -z "${SKIP_KOHA_LOGIN:-}" ] && [ -z "$KOHA_PASS" ] && { echo "ERROR: set KOHA_USER or SKIP_KOHA_PASS"; exit 1; }
MOCK_URL="${MOCK_URL:-https://localhost:9000}"
RESULTS_FILE="${RESULTS_FILE:-/tmp/rfid-test-results}"

# Data files
TAGS="$(cat tests/tags.json)"
PAGES="$(cat tests/pages.json)"
SCENARIOS="$(cat tests/scenarios.json)"

# ------------------------------------------------------------------
# Logging
# ------------------------------------------------------------------
pass()  { echo "  OK $*"; }
fail()  {
    echo "  FAIL $*"
    if [ -n "${SCENARIO_ID:-}" ]; then
        record_result "$PAGE" "$SCENARIO_ID" "fail"
    fi
    return 1
}
info()  { echo "  - $*"; }
result(){ local s="$1"; record_result "$PAGE" "$SCENARIO_ID" "$s"; }

record_result() {
    local page="$1"
    local scenario_id="$2"
    local status="$3"
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    echo "$timestamp | $page | $scenario_id | $status" >> "$RESULTS_FILE"
}

# ------------------------------------------------------------------
# RFID server detection
# ------------------------------------------------------------------
RFID_HOST="${RFID_HOST:-localhost}"
RFID_PORT="${RFID_PORT:-9000}"

check_rfid_server() {
    echo ""
    echo "Checking RFID server at $RFID_HOST:$RFID_PORT..."
    local resp
    resp=$(curl -sk "https://$RFID_HOST:$RFID_PORT/" 2>/dev/null || echo "")
    if echo "$resp" | grep -q "mock"; then
        echo "  → Mock RFID server detected (good)"
        return 0
    fi
    if echo "$resp" | grep -qiE '(koha|rfid|html|read|tag)' 2>/dev/null; then
        echo ""
        echo "  ⚠  REAL RFID SERVER detected at $RFID_HOST:$RFID_PORT"
        echo "  ⚠  Please stop the real server so mock can start."
        echo "  ⚠  The real server will interfere with controlled testing."
        echo ""
        return 1
    fi
    # No response — nothing running, mock can start
    echo "  → No RFID server detected — mock will start"
    return 0
}

# ------------------------------------------------------------------
# Mock server — delegates to server.sh for clean start/stop with logging.
# If a mock-mode server is already running, skip start and stop (leave it for other tests).
# If a real reader is running, stop it first so mock can take over.
# ------------------------------------------------------------------
mock_start() {
    # Check server.sh status — it prints mode (mock or real)
    local status
    status=$(./server.sh status 2>/dev/null || echo "")
    if echo "$status" | grep -q "mode: mock"; then
        echo "  → Mock server already running — reusing"
        return 0
    fi
    if echo "$status" | grep -q "mode: real"; then
        echo "  → Stopping real server so mock can start"
        ./server.sh stop
        sleep 1
    fi
    ./server.sh start --mock || return 1
}

mock_stop() { :; }
mock_clear() { curl -sk -X POST "$MOCK_URL/mock/clear" >/dev/null 2>&1; }
mock_add() { curl -sk -X POST -d "{\"sid\":\"$1\",\"content\":\"$2\",\"security\":\"$3\"}" "$MOCK_URL/mock/tag" >/dev/null 2>&1; }
mock_reset() { curl -sk -X POST "$MOCK_URL/mock/reset" >/dev/null 2>&1; }
mock_error() { curl -sk -X POST -d "{\"count\":$1}" "$MOCK_URL/mock/error" >/dev/null 2>&1; }
mock_timeout() { curl -sk -X POST -d "{\"count\":$1}" "$MOCK_URL/mock/timeout" >/dev/null 2>&1; }
mock_remove() { curl -sk -X POST -d "{\"sid\":\"$1\"}" "$MOCK_URL/mock/remove" >/dev/null 2>&1; }

# ------------------------------------------------------------------
# rodney
# ------------------------------------------------------------------
rodney() { uvx rodney "$@" 2>&1; }

koha_login() {
    [ -n "${SKIP_KOHA_LOGIN:-}" ] && echo "  SKIP_KOHA_LOGIN set — skipping login" && return
    rodney page 0
    # Only navigate if not already on the Koha page (avoids unnecessary reload).
    local current_url
    current_url=$(rodney url 2>/dev/null || echo "")
    if [[ "$current_url" != "$KOHA_URL/mainpage.pl" ]]; then
        rodney open "$KOHA_URL/mainpage.pl"
        rodney waitload
    fi
    # Check if already logged in — no login form means session active
    if rodney exists '#login form' 2>/dev/null; then
        rodney input 'input[name=userid]' "$KOHA_USER"
        rodney input 'input[name=password]' "$KOHA_PASS"
        rodney click 'input#submit'
        rodney waitload
    else
        echo "  Already logged in — skipping form fill"
    fi
    # Verify logged-in user by checking the logged-in username element
    local logged_user
    logged_user=$(rodney text '.loggedinusername' 2>/dev/null || rodney text '.logged-info .loggedinusername' 2>/dev/null || rodney js "document.querySelector('.loggedinusername, .logged-info .loggedinusername, #loggedinusername, .loggedincontact, #logout')?.innerText" 2>/dev/null)
    if [ -n "$logged_user" ]; then
        echo "  Logged in as: $logged_user"
    else
        echo "  [warn] could not verify logged-in user — page may not be staff client"
    fi
    rodney sleep 2
}

tab_switch() {
    local tab="$1"
    [ -z "$tab" ] && return 0
    case "$tab" in
        checkout) rodney js 'document.querySelector("a[href=\"#circ_search\"]").click()';;
        checkin)  rodney js 'document.querySelector("a[href=\"#checkin_search\"]").click()';;
        renew)    rodney js 'document.querySelector("a[href=\"#renew_search\"]").click()';;
        *)        echo "  [warn] unknown tab: $tab"; return 1;;
    esac
    rodney sleep 1
}

# ------------------------------------------------------------------
# Tags
# ------------------------------------------------------------------
load_tag() {
    local key="$1"
    local sid content security
    if [ "$key" = "empty" ]; then
        sid=$(echo "$TAGS" | jq -r '.book1.sid')
        mock_add "$sid" "" "DA"
        return
    fi
    sid=$(echo "$TAGS" | jq -r ".\"$key\".sid")
    content=$(echo "$TAGS" | jq -r ".\"$key\".content")
    security=$(echo "$TAGS" | jq -r ".\"$key\".security")
    [ "$sid" = "null" ] && echo "  [error] unknown tag: $key" && return 1
    mock_add "$sid" "$content" "$security"
}

check_popup_contains() {
    local search="$1"
    local text
    text=$(rodney js "document.getElementById('rfid-popup-body')?.innerText || ''" 2>/dev/null)
    if echo "$text" | grep -qiE "$search"; then
        pass "popup contains '$search'"
        return 0
    fi
    fail "popup does not contain '$search' (found: '$text')"
    return 1
}

check_popup_empty() {
    local text
    text=$(rodney js "document.getElementById('rfid-popup-body')?.innerText || ''" 2>/dev/null)
    if [[ -z "$text" || "$text" == *"(no tags)"* || "$text" == *"no tags in range"* ]]; then
        pass "popup is empty"
        return 0
    fi
    fail "popup is not empty (found: '$text')"
    return 1
}

# ------------------------------------------------------------------
# Debug helper — prints commands and HTML dump for interactive debugging
# ------------------------------------------------------------------
debug_help() {
    local url; url=$(rodney url 2>/dev/null || echo "unknown")
    echo ""
    echo "  [══ Debug ---------------------------------------------]"
    echo "  |  To inspect interactively:                          |"
    echo "  |    /home/dpavlin/.local/bin/uvx rodney '$url'       |"
    echo "  |    /home/dpavlin/.local/bin/uvx rodney html         |"
    echo "  |    /home/dpavlin/.local/bin/uvx rodney js '...'     |"
    echo "  [══════════════════════════════════════════════════════]"
    echo ""
    echo "  -- HTML dump --"
    rodney html 2>/dev/null | head -100 || echo "  [no HTML output]"
    echo ""
    echo "  -- localStorage rfid_afi --"
    rodney js 'JSON.stringify(localStorage.getItem("rfid_afi"))' 2>/dev/null || echo "  [no data]"
    echo ""
    echo "  -- rfidDebug --"
    rodney js 'JSON.stringify(window.rfidDebug || {})' 2>/dev/null || echo "  [no debug object]"
    echo ""
}

# ------------------------------------------------------------------
# DOM checks
check_db() {
    local result; result=$(koha_mysql "$1" 2>/dev/null || echo "")
    if echo "$result" | grep -q "$2" 2>/dev/null; then
        pass "DB: $2"; return 0
    fi
    fail "DB expected '$2' but got: $result"; return 1
}

# ------------------------------------------------------------------
# Helper to run koha-mysql queries via SSH (handles quoting properly)
# ------------------------------------------------------------------
koha_mysql() {
    local sql="$1"
    ssh koha-dev.rot13.org "sudo /usr/sbin/koha-mysql ffzg -e '$sql'" 2>/dev/null
}

# ------------------------------------------------------------------
# DOM checks
check_input_filled() {
    local sel="$1"
    local val
    val=$(rodney js "document.querySelector('$sel')?.value" 2>/dev/null || echo "")
    if [ -n "$val" ]; then
        pass "input '$sel' filled with: $val"
        return 0
    fi
    fail "input '$sel' is empty"
    return 1
}

# ------------------------------------------------------------------
# Pre-flight checks — verify Koha DB state before running tests
# ------------------------------------------------------------------
pre_flight_check() {
    echo ""
    echo "-- Pre-flight checks --"

    # Clean up any leftover issues from previous runs first
    cleanup_issues

    # Check patron exists in borrowers table
    local patron
    patron=$(koha_mysql "SELECT COUNT(*) FROM borrowers WHERE cardnumber='200000000042'" || echo "")
    if echo "$patron" | grep -q "1"; then
        echo "  OK patron 200000000042 exists"
    else
        echo "  FAIL patron 200000000042 not found"
        return 1
    fi

    # Check book barcodes exist in items table
    for bc in 1301111111 1302079605 1302099999; do
        local exists
        exists=$(koha_mysql "SELECT COUNT(*) FROM items WHERE barcode='$bc'" || echo "")
        if echo "$exists" | grep -q "1"; then
            echo "  OK barcode $bc exists in items"
        else
            echo "  FAIL barcode $bc not found in items"
            return 1
        fi
    done

    # Check none of the books are currently issued
    for bc in 1301111111 1302079605 1302099999; do
        local issued
        issued=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bc'" || echo "")
        if echo "$issued" | grep -q "0"; then
            echo "  OK barcode $bc is not issued — clean"
        else
            echo "  FAIL barcode $bc is currently issued — state not reproducible"
            return 1
        fi
    done

    echo "-- Pre-flight OK --"
    return 0
}

# ------------------------------------------------------------------
# Cleanup — revert Koha DB state to original (delete issues created by tests)
# ------------------------------------------------------------------
cleanup_issues() {
    echo ""
    echo "-- Cleanup --"
    # Delete only test patron's issues for our specific test books
    local patron_id
    patron_id=$(koha_mysql "SELECT borrowernumber FROM borrowers WHERE cardnumber='200000000042'" | grep -v 'borrowernumber' || echo "")
    for bc in 1301111111 1302079605 1302099999; do
        local count
        count=$(koha_mysql "DELETE FROM issues WHERE borrowernumber=$patron_id AND itemnumber=(SELECT itemnumber FROM items WHERE barcode='$bc')" || echo "")
        echo "  barcode $bc: deleted $count issue(s)"
    done
    echo "-- Cleanup done --"
}
