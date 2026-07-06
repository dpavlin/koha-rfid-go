# Mock RFID Server

A controllable HTTP server that simulates the real RFID reader's API so you
can test `koha-rfid.js` without a physical reader.

## Quick start

```bash
# Build and start on port 9001
go build -o cmd/mock-rfid/mock-rfid ./cmd/mock-rfid/
./cmd/mock-rfid/mock-rfid -port 9001
```

## Pointing the JS at the mock

In `koha-rfid.js`, change `rfid_base_url`:

```js
var rfid_base_url = 'http://localhost:9001';   // mock, no TLS needed
```

If TLS is needed for testing, start with `-tls`:

```bash
./cmd/mock-rfid/mock-rfid -port 9001 -tls
```

## Endpoints

### Real endpoints (same as physical reader)

| Endpoint | Method | Response |
|---|---|---|
| `/ping` | GET | `{"status":"ok"}` |
| `/scan/` | GET | `{"tags":[{"sid":"…","content":"…","security":"DA","tag_type":"RFID501","reader":"mock"},…]}` |
| `/secure.js?SID=D7&callback=jsonp` | GET | `jsonp({"ok":1,"error":""})` |

### Control endpoints (drive the simulation)

| Endpoint | Method | Body (JSON) | Effect |
|---|---|---|---|
| `/mock/tag` | POST | `{"sid":"16hex","content":"130…","security":"DA"}` | Add one tag to inventory |
| `/mock/remove` | POST | `{"sid":"16hex"}` | Remove one tag by SID |
| `/mock/clear` | POST | — | Remove all tags |
| `/mock/set` | POST | `[{"sid":"…","content":"…","security":"DA"},…]` | Replace entire inventory |
| `/mock/error` | POST | `{"count":N}` | Next N `/scan/` calls return 500 |
| `/mock/timeout` | POST | `{"count":N}` | Next N `/scan/` calls hang 30 s |
| `/mock/status` | GET | — | Returns current tags + counters |
| `/mock/reset` | POST | — | Clears all state |

## Real tag data from RFID reader

These are the actual tags currently visible on the physical reader (3M810).
Use these values in mock scenarios so Koha sees the same barcodes.

### Current inventory

| SID | Content (barcode) | Security | Type |
|---|---|---|---|
| `e004010031269117` | `1302099999` | `d7` (on loan) | Book |
| `e00401001f7812ed` | `1301111111` | `da` (checked in) | Book |
| `e00401001f77fb98` | `200000000042` | `da` | Patron card |
| `e00401003126a0c8` | `1302079605` | `da` (checked in) | Book |

### How the JS handles each type

| Content prefix | JS action |
|---|---|
| `130…` | Book — fill barcode field, submit to Koha |
| `200…` | Patron card — fill borrower search, submit |
| Other | Treated as patron card |

**Security logic** (for books):
- `DA` (checked in) → fill barcode and submit on checkin/checkout pages
- `D7` (on loan) → fill barcode on renew page; rejected on checkout

### Setup script

Save this as `setup-mock.sh` to load the exact real-world state into the mock:

```bash
#!/bin/bash
# Load current RFID reader state into mock server on port 9001
MOCK=http://localhost:9001

# Clear first
curl -s -X POST $MOCK/mock/clear

# Add patron card
curl -s -X POST -d '{"sid":"e00401001f77fb98","content":"200000000042","security":"DA"}' $MOCK/mock/tag

# Add books
curl -s -X POST -d '{"sid":"e004010031269117","content":"1302099999","security":"D7"}' $MOCK/mock/tag
curl -s -X POST -d '{"sid":"e00401001f7812ed","content":"1301111111","security":"DA"}' $MOCK/mock/tag
curl -s -X POST -d '{"sid":"e00401003126a0c8","content":"1302079605","security":"DA"}' $MOCK/mock/tag

echo "Mock loaded with real tag data"
curl -s $MOCK/mock/status | jq
```

## Simulating RFID events with rodney

Below are scenarios using **real barcodes** from the reader so Koha's database
mappings match.

### 1. Book enters range (checked in)

Simulate placing a book with DA (checked in) on the reader:

```bash
curl -s -X POST -d '{"sid":"e00401001f7812ed","content":"1301111111","security":"DA"}' \
  http://localhost:9001/mock/tag
```

→ JS fills barcode field, submits checkin/checkout, writes D7 to tag.

### 2. Book enters range (on loan)

Simulate placing a book that is currently on loan (D7):

```bash
curl -s -X POST -d '{"sid":"e004010031269117","content":"1302099999","security":"D7"}' \
  http://localhost:9001/mock/tag
```

→ On renew page: fills barcode and submits.
→ On checkout page: shows "not checked in — cannot checkout".

### 3. Patron card enters range

```bash
curl -s -X POST -d '{"sid":"e00401001f77fb98","content":"200000000042","security":"DA"}' \
  http://localhost:9001/mock/tag
```

→ JS fills borrower search and submits.

### 4. One book leaves range (others stay)

Remove a single tag from inventory:

```bash
curl -s -X POST -d '{"sid":"e00401001f7812ed"}' http://localhost:9001/mock/remove
```

→ That barcode disappears from `/scan/`; other tags remain.

### 5. All books leave range

```bash
curl -s -X POST http://localhost:9001/mock/clear
```

→ Next `/scan/` returns empty → JS shows "no tags in range".

### 6. AFI write (checkin → D7)

Add a DA book, then simulate the secure write that the JS performs:

```bash
curl -s -X POST -d '{"sid":"e00401001f7812ed","content":"1301111111","security":"DA"}' \
  http://localhost:9001/mock/tag
# JS calls /secure.js?e00401001f7812ed=D7 → mock updates security to D7
curl -s 'http://localhost:9001/secure.js?e00401001f7812ed=D7&callback=jsonp'
# Verify
curl -s http://localhost:9001/mock/status | jq
```

### 6. Reader error

```bash
curl -s -X POST -d '{"count":2}' http://localhost:9001/mock/error
# Next 2 /scan/ calls fail → JS shows error and retries
```

### 7. Reader timeout

```bash
curl -s -X POST -d '{"count":1}' http://localhost:9001/mock/timeout
# Next /scan/ hangs 30 s → JS fetch timeout fires → retry
```

### 8. Multiple tags (ambiguous — more than one tag visible)

```bash
curl -s -X POST -d '[{"sid":"e00401001f7812ed","content":"1301111111","security":"DA"},{"sid":"e00401003126a0c8","content":"1302079605","security":"DA"}]' \
  http://localhost:9001/mock/set
```

→ JS shows warning: "2 tags near reader: 1301111111 1302079605" (red).

### 9. Empty tag (no barcode)

```bash
curl -s -X POST -d '{"sid":"e004010000000000","content":"","security":"DA"}' \
  http://localhost:9001/mock/tag
```

→ JS shows "e004010000000000 empty" (red).

### 10. Full workflow: patron scans card, then scans book

```bash
# Clear and load patron card
curl -s -X POST http://localhost:9001/mock/clear
curl -s -X POST -d '{"sid":"e00401001f77fb98","content":"200000000042","security":"DA"}' \
  http://localhost:9001/mock/tag
# Patron card detected → borrower search submitted

# Clear and load a DA book for checkout
curl -s -X POST http://localhost:9001/mock/clear
curl -s -X POST -d '{"sid":"e00401001f7812ed","content":"1301111111","security":"DA"}' \
  http://localhost:9001/mock/tag
# Book detected → checkout submitted → JS writes D7
curl -s 'http://localhost:9001/secure.js?e00401001f7812ed=D7&callback=jsonp'
```

## How the mock server works

- In-memory tag list protected by a mutex.
- `/scan/` returns whatever tags are in the list, with `tag_type:"RFID501"` and
  `reader:"mock"` to distinguish from real reader output.
- `/secure.js` looks up the tag by SID and updates its `security` field to the
  requested value (DA or D7), then returns success.
- Error/timeout counters decrement on each `/scan/` call and affect only the
  next N calls.
- Every request is logged to stderr.
- CORS headers (`Access-Control-Allow-Origin: *`) on all responses.
- Default port is `9000`; use `9001` to avoid conflicting with the real reader.
