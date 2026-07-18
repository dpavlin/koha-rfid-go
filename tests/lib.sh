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

# Tag definitions inlined directly using barcodes as keys
declare -A TAG_SID
declare -A TAG_SECURITY
declare -A TAG_TYPE

TAG_SID["200000000042"]="e00401001f77fb98"
TAG_SECURITY["200000000042"]="DA"
TAG_TYPE["200000000042"]="patron"

TAG_SID["1301111111"]="e00401001f7812ed"
TAG_SECURITY["1301111111"]="DA"
TAG_TYPE["1301111111"]="book"

TAG_SID["1302079605"]="e00401003126a0c8"
TAG_SECURITY["1302079605"]="DA"
TAG_TYPE["1302079605"]="book"

TAG_SID["1302099999"]="e004010031269117"
TAG_SECURITY["1302099999"]="D7"
TAG_TYPE["1302099999"]="book"

get_tag_sid() {
    echo "${TAG_SID[$1]:-}"
}

# ------------------------------------------------------------------
# Test results tracking
# ------------------------------------------------------------------
TEST_RESULT_PASS=0
TEST_RESULT_FAIL=0
TEST_RESULT_SKIP=0

# ------------------------------------------------------------------
# Logging
# ------------------------------------------------------------------
pass()  {
    TEST_RESULT_PASS=$((TEST_RESULT_PASS + 1))
    echo "  OK $*"
}
fail()  {
    TEST_RESULT_FAIL=$((TEST_RESULT_FAIL + 1))
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
        echo "  -> Mock RFID server detected (good)"
        return 0
    fi
    if echo "$resp" | grep -qiE '(koha|rfid|html|read|tag)' 2>/dev/null; then
        echo ""
        echo "  [WARNING] REAL RFID SERVER detected at $RFID_HOST:$RFID_PORT"
        echo "  [WARNING] Please stop the real server so mock can start."
        echo "  [WARNING] The real server will interfere with controlled testing."
        echo ""
        return 1
    fi
    # No response — nothing running, mock can start
    echo "  -> No RFID server detected — mock will start"
    return 0
}

# ------------------------------------------------------------------
# Mock server lifecycle. State is reset by suite_start, once per page suite.
# If nothing is running, start mock. If real reader is running, stop it first.
# ------------------------------------------------------------------
mock_start() {
    # Check if mock server is already running
    if curl -sk "$MOCK_URL/mock/status" >/dev/null 2>&1; then
        return 0
    fi
    # Nothing running — start mock
    check_rfid_server
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
    if rodney exists '#loginform' 2>/dev/null; then
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
    # Force a hard reload to clear cached koha-rfid.js
    echo "Performing hard reload to refresh script cache..."
    rodney reload --hard
    rodney waitload
    rodney sleep 2
}

tab_switch() {
    local tab="$1"
    [ -z "$tab" ] && return 0
    rodney wait '#circ_search' >/dev/null 2>&1 || true
    case "$tab" in
        checkout) rodney js 'document.querySelector("a[href$=\"#circ_search\"]")?.click()' >/dev/null 2>&1 || true;;
        checkin)  rodney js 'document.querySelector("a[href$=\"#checkin_search\"]")?.click()' >/dev/null 2>&1 || true;;
        renew)    rodney js 'document.querySelector("a[href$=\"#renew_search\"]")?.click()' >/dev/null 2>&1 || true;;
        *)        echo "  [warn] unknown tab: $tab"; return 1;;
    esac
    rodney sleep 1.5
}

# Pause RFID polling so waitstable doesn't hang (polling keeps mutating DOM)
rfid_pause() {
    rodney js "clearTimeout(rfid_timeout)" >/dev/null 2>&1
}

# Resume RFID polling after a pause
rfid_resume() {
    rodney js "rfid_poll()" >/dev/null 2>&1
}

# ------------------------------------------------------------------
# Tags
# ------------------------------------------------------------------
load_tag() {
    local barcode="$1"
    local sid security
    if [ "$barcode" = "empty" ]; then
        sid="${TAG_SID["1301111111"]}"
        mock_add "$sid" "" "DA"
        return
    fi
    sid="${TAG_SID[$barcode]:-}"
    security="${TAG_SECURITY[$barcode]:-}"
    if [ -z "$sid" ]; then
        # Fallback for dynamic/arbitrary barcodes: generate a unique dummy SID
        sid="e0040100$(echo -n "$barcode" | md5sum | cut -c1-8)"
        security="DA"
    fi
    mock_add "$sid" "$barcode" "$security"
}

# Place a known tag with an explicit AFI when a scenario models a transition.
# This does not alter the fixture default used by later scenarios.
load_tag_with_security() {
    local barcode="$1" security="$2"
    local sid
    sid="${TAG_SID[$barcode]:-}"
    if [ -z "$sid" ]; then
        echo "  [error] no SID fixture for $barcode"
        return 1
    fi
    mock_add "$sid" "$barcode" "$security"
}

check_popup_contains() {
    local search="$1"
    local text=""
    local start; start=$(date +%s)
    while [ $(( $(date +%s) - start )) -lt 6 ]; do
        check_koha_messages
        text=$(rodney js "document.getElementById('rfid-popup-body')?.innerText || ''" 2>/dev/null)
        if echo "$text" | grep -qiE "$search"; then
            pass "popup contains '$search'"
            return 0
        fi
        sleep 0.2
    done
    fail "popup does not contain '$search' (found: '$text')"
    return 1
}

check_popup_empty() {
    local text=""
    local start; start=$(date +%s)
    while [ $(( $(date +%s) - start )) -lt 6 ]; do
        check_koha_messages
        text=$(rodney js "document.getElementById('rfid-popup-body')?.innerText || ''" 2>/dev/null)
        if [[ -z "$text" || "$text" == *"(no tags)"* || "$text" == *"no tags in range"* || "$text" == *" empty"* ]]; then
            pass "popup is empty"
            return 0
        fi
        sleep 0.2
    done
    fail "popup is not empty (found: '$text')"
    return 1
}

check_mock_tag_security() {
    local bc_key="$1" expected_sec="$2"
    local sid
    sid=$(get_tag_sid "$bc_key")
    local status_json=""
    local actual_sec=""
    local start; start=$(date +%s)
    local expected_upper
    expected_upper=$(echo "$expected_sec" | tr '[:lower:]' '[:upper:]')
    while [ $(( $(date +%s) - start )) -lt 6 ]; do
        status_json=$(curl -sk "$MOCK_URL/mock/status" 2>/dev/null || echo "")
        actual_sec=$(echo "$status_json" | jq -r ".tags[] | select(.sid == \"$sid\") | .security" 2>/dev/null || echo "")
        local actual_upper
        actual_upper=$(echo "$actual_sec" | tr '[:lower:]' '[:upper:]')
        if [ "$actual_upper" = "$expected_upper" ]; then
            pass "mock tag $bc_key security is $expected_sec"
            return 0
        fi
        sleep 0.2
    done
    fail "mock tag $bc_key security expected $expected_sec but got $actual_sec"
    return 1
}

# ------------------------------------------------------------------
# RFID state management. Reset only at a suite boundary; scenarios must model
# physical tag placement/removal and retain browser state from prior actions.
# ------------------------------------------------------------------
reset_rfid_state() {
    mock_reset
    rodney js "(function() { localStorage.removeItem('rfid_afi'); window.rfid_popup_update(); var b=document.getElementById('rfid-popup-body'); if(b) b.textContent='(no tags)'; })()" >/dev/null 2>&1
}

suite_start() {
    local url="$1"
    reset_rfid_state
    rodney open "$url"
    rodney waitload
    rodney sleep 1
}

visit_page() {
    rodney open "$1"
    rodney waitload
    rodney sleep 1
}

# ------------------------------------------------------------------
# Scenario helpers — better output with context
# ------------------------------------------------------------------
scenario_start() {
    local sid="$1" name="$2"
    echo ""
    echo "  --- Scenario $sid: $name ---"
    # Do not reset RFID/browser state or navigate here. A scenario continues
    # from the previous librarian action. Tests that genuinely need another
    # page must call visit_page explicitly.
}

scenario_end() {
    : # no-op, can be extended
}

# ------------------------------------------------------------------
# Test summary — prints pass/fail/skip counts at the end
# ------------------------------------------------------------------
test_summary() {
    echo ""
    echo "==================================================================="
    echo "  Test Summary"
    echo "==================================================================="
    local total=$((TEST_RESULT_PASS + TEST_RESULT_FAIL))
    echo "  Total: $total"
    echo "  Pass:  $TEST_RESULT_PASS"
    echo "  Fail:  $TEST_RESULT_FAIL"
    if [ "$TEST_RESULT_FAIL" -eq 0 ]; then
        echo "  Result: ALL TESTS PASSED"
    else
        echo "  Result: SOME TESTS FAILED"
    fi
    echo "==================================================================="
    echo ""
}

# ------------------------------------------------------------------
# HTML message/warning check
# ------------------------------------------------------------------
check_koha_messages() {
    local msgs
    msgs=$(rodney js "(function() {
        var selectors = [
            '#circ_messages',
            '#circ_needsconfirmation',
            '.dialog.alert',
            '.dialog.warning',
            '.dialog.message',
            '.alert-warning',
            '.alert-info',
            '.alert-danger',
            '.dialog'
        ];
        var found = [];
        selectors.forEach(function(sel) {
            var el = document.querySelector(sel);
            if (el && el.offsetParent !== null) {
                var text = el.innerText.trim().replace(/\\s+/g, ' ');
                if (text) { found.push(sel + ': \"' + text + '\"'); }
            }
        });
        return found.join(' | ');
    })()" 2>/dev/null || echo "")
    if [ -n "$msgs" ]; then
        info "Koha page message(s): $msgs"
    fi
}

# ------------------------------------------------------------------
# Debug helper — prints commands and HTML dump for interactive debugging
# ------------------------------------------------------------------
debug_help() {
    local url; url=$(rodney url 2>/dev/null || echo "unknown")
    echo ""
    echo "  [== Debug ---------------------------------------------]"
    echo "  |  To inspect interactively:                          |"
    echo "  |    /home/dpavlin/.local/bin/uvx rodney '$url'       |"
    echo "  |    /home/dpavlin/.local/bin/uvx rodney html         |"
    echo "  |    /home/dpavlin/.local/bin/uvx rodney js '...'     |"
    echo "  [======================================================]"
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
    local sql="$1" expected="$2"
    local start; start=$(date +%s)
    local result=""
    while [ $(( $(date +%s) - start )) -lt 15 ]; do
        result=$(koha_mysql "$sql" 2>/dev/null | tail -n 1 | tr -d '\r')
        if echo "$result" | grep -q "$expected" 2>/dev/null; then
            pass "DB: $expected"
            return 0
        fi
        sleep 0.2
    done
    fail "DB expected '$expected' but got: $result"
    return 1
}

# ------------------------------------------------------------------
# Helper to run koha-mysql queries via SSH (handles quoting properly)
# ------------------------------------------------------------------
koha_mysql() {
    local sql="$1"
    timeout 30 ssh -o ConnectTimeout=5 koha-dev.rot13.org "sudo /usr/sbin/koha-mysql ffzg -e '$sql'" 2>/dev/null
}

db_checkout() {
    local patron="$1" barcode="$2"
    local borrowernumber itemnumber
    borrowernumber=$(koha_mysql "SELECT borrowernumber FROM borrowers WHERE cardnumber=\"$patron\"" | tail -n 1 | tr -d '\r')
    itemnumber=$(koha_mysql "SELECT itemnumber FROM items WHERE barcode=\"$barcode\"" | tail -n 1 | tr -d '\r')
    if [ -n "$borrowernumber" ] && [ -n "$itemnumber" ]; then
        koha_mysql "DELETE FROM issues WHERE itemnumber=$itemnumber; INSERT INTO issues (itemnumber, borrowernumber, date_due, branchcode, issuedate) VALUES ($itemnumber, $borrowernumber, DATE_ADD(NOW(), INTERVAL 14 DAY), \"FFZG\", NOW()); UPDATE items SET onloan=DATE(DATE_ADD(NOW(), INTERVAL 14 DAY)) WHERE itemnumber=$itemnumber;" >/dev/null 2>&1
    fi
}

# ------------------------------------------------------------------
# DOM checks
check_input_filled() {
    check_koha_messages
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
    local all_clean=1
    for bc in 1301111111 1302079605 1302099999; do
        local issued
        issued=$(koha_mysql "SELECT COUNT(*) FROM issues JOIN items USING (itemnumber) WHERE items.barcode='$bc'" 2>/dev/null || echo "")
        if echo "$issued" | grep -q "0"; then
            echo "  OK barcode $bc is not issued — clean"
        else
            echo "  WARNING barcode $bc is currently issued — cleaning"
            local patron_id
            patron_id=$(koha_mysql "SELECT borrowernumber FROM borrowers WHERE cardnumber='200000000042'" | grep -v borrowernumber | tr -d ' ')
            koha_mysql "DELETE FROM issues WHERE borrowernumber=$patron_id AND itemnumber=(SELECT itemnumber FROM items WHERE barcode='$bc')" >/dev/null 2>&1
            koha_mysql "UPDATE items SET onloan=NULL WHERE barcode='$bc' AND onloan IS NOT NULL" >/dev/null 2>&1
            all_clean=0
        fi
    done

    if [ "$all_clean" -eq 1 ]; then
        echo "-- Pre-flight OK --"
    else
        echo "-- Pre-flight OK (after cleanup) --"
    fi
    return 0
}

# ------------------------------------------------------------------
# Cleanup — revert Koha DB to original state (delete issues created by tests)
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
        # Also clear the onloan field in items table (Koha doesn't do this automatically)
        koha_mysql "UPDATE items SET onloan=NULL WHERE barcode='$bc' AND onloan IS NOT NULL" >/dev/null 2>&1
    done
    echo "-- Cleanup done --"
}
