# rodney — Reusable Test Script Patterns

## Purpose

Standardize rodney commands in shell scripts so each page test is
consistent, handles login, tab management, and error checking.

## Setup

```bash
export PATH=$PATH:/home/dpavlin/.local/bin
export CDP_PORT=${CDP_PORT:-9333}
export RODNEY_HOME=${RODNEY_HOME:-~/.rodney}

# Connect to Chrome
uvx rodney connect localhost:$CDP_PORT
```

## Login sequence

```bash
koha_login() {
    local url="$1" user="$2" pass="$3"
    uvx rodney open "$url"
    uvx rodney wait '#login form'
    uvx rodney input 'input[name=userid]' "$user"
    uvx rodney input 'input[name=password]' "$pass"
    uvx rodney click 'input#login'
    uvx rodney waitload
}
```

## Page navigation

```bash
uvx rodney newpage "$KOHA_URL/<page>"
uvx rodney waitload
```

## Tab switching

Pages use jQuery UI tabs. Click the tab link to activate:

```bash
# Check out tab (patron search + checkout barcode)
uvx rodney click 'a[href="#circ_search"]'
uvx rodney sleep 1

# Check in tab (ret_barcode)
uvx rodney click 'a[href="#checkin_search"]'
uvx rodney sleep 1

# Renew tab (ren_barcode)
uvx rodney click 'a[href="#renew_search"]'
uvx rodney sleep 1
```

## Input fields by page

| Page | Input ID | Tab required | Notes |
|------|----------|-------------|-------|
| circulation.pl | `#findborrower` | checkout (default) | patron search |
| circulation.pl | `#ret_barcode` | checkin | checkin barcode |
| circulation.pl | `#ren_barcode` | renew | renew barcode |
| circulation.pl | `#barcode` | checkout | appears only after patron selected |
| circulation-home.pl | `#findborrower` | checkout (default) | patron search |
| circulation-home.pl | `#ret_barcode` | checkin | checkin barcode |
| circulation-home.pl | `#ren_barcode` | renew | renew barcode |
| returns.pl | `#barcode` | none (main form) | checkin barcode |
| renew.pl | `#barcode` | none (main form) | renew barcode |
| mainpage.pl | none | — | no forms |

## Mock control helpers

```bash
MOCK_URL=${MOCK_URL:-https://localhost:9000}

mock_clear() { curl -sk -X POST "$MOCK_URL/mock/clear"; }
mock_add() {
    local sid="$1" content="$2" security="$3"
    curl -sk -X POST -d "{\"sid\":\"$sid\",\"content\":\"$content\",\"security\":\"$security\"}" \
        "$MOCK_URL/mock/tag"
}
mock_set() { curl -sk -X POST -d "$1" "$MOCK_URL/mock/set"; }
mock_status() { curl -sk "$MOCK_URL/mock/status"; }
mock_remove() { curl -sk -X POST -d "{\"sid\":\"$1\"}" "$MOCK_URL/mock/remove"; }
mock_error() { curl -sk -X POST -d "{\"count\":$1}" "$MOCK_URL/mock/error"; }
mock_timeout() { curl -sk -X POST -d "{\"count\":$1}" "$MOCK_URL/mock/timeout"; }
mock_reset() { curl -sk -X POST "$MOCK_URL/mock/reset"; }
```

Note: `-sk` = skip TLS verification for self-signed cert.

## Wait for RFID scan cycle

After loading tags, wait for the JS to poll and process:

```bash
rfid_wait() { sleep 3; }  # default; adjust as needed
```

## Verify DOM state

```bash
# Check RFID popup text
uvx rodney text '#rfid-popup-body'

# Check barcode field value
uvx rodney js 'document.querySelector("#barcode")?.value || document.querySelector("#ret_barcode")?.value || document.querySelector("#ren_barcode")?.value || "none"'

# Check RFID info
uvx rodney text '#rfid-info'

# Check console logs for errors
uvx rodney js 'JSON.stringify(window.__consoleLogs || [])'

# Check RFID status text
uvx rodney text '#rfid'
```

## koha-mysql verification

```bash
koha_mysql() {
    ssh koha-dev.rot13.org sudo /usr/sbin/koha-mysql ffzg -e "$1"
}

# Check if book is checked out
koha_mysql "SELECT itemnumber, barcode FROM items WHERE barcode IN ('1301111111','1302079605','1302099999') AND notforloan=0"
```

## Test result tracking

```bash
# Record pass/fail per scenario per page
record_result() {
    local page="$1" scenario="$2" status="$3"
    echo "$page.$scenario=$status" >> /tmp/rfid-test-results
}

# Check if scenario already passed
scenario_passed() {
    local page="$1" scenario="$2"
    grep -q "$page.$scenario=pass" /tmp/rfid-test-results 2>/dev/null
}

# Skip if already passed
if scenario_passed "circulation" "11"; then
    echo "Scenario 11 already passed — skipping"
    return 0
fi
```
