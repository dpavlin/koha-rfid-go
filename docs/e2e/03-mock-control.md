# Mock Server Control — Shell Patterns

## Purpose

Standardize how test scripts drive the mock RFID server via HTTP.

## Starting the mock

```bash
# Option A (chosen): port 9000 + TLS — no JS override needed
./cmd/mock-rfid/mock-rfid -port 9000 -tls &
MOCK_PID=$!
MOCK_URL="https://localhost:9000"

# Option B (not used): port 9001 + HTTP — would need rfid_base_url override
# ./cmd/mock-rfid/mock-rfid -port 9001 &
# MOCK_PID=$!
# MOCK_URL="http://localhost:9001"
```

## Endpoints reference

| Operation | Method | URL | Body |
|-----------|--------|-----|------|
| Add tag | POST | `/mock/tag` | `{"sid":"16hex","content":"barcode","security":"DA\|D7"}` |
| Remove tag | POST | `/mock/remove` | `{"sid":"16hex"}` |
| Clear all | POST | `/mock/clear` | (empty) |
| Replace all | POST | `/mock/set` | `[{"sid":"...","content":"...","security":"DA"},...]` |
| Set error mode | POST | `/mock/error` | `{"count":N}` |
| Set timeout mode | POST | `/mock/timeout` | `{"count":N}` |
| Get status | GET | `/mock/status` | — |
| Reset all | POST | `/mock/reset` | (empty) |

## Real tag data (from physical reader)

| SID | Barcode | Security | Type |
|-----|---------|----------|------|
| `e004010031269117` | `1302099999` | D7 | Book (on loan) |
| `e00401001f7812ed` | `1301111111` | DA | Book (checked in) |
| `e00401001f77fb98` | `200000000042` | DA | Patron card |
| `e00401003126a0c8` | `1302079605` | DA | Book (checked in) |

## Sequence patterns

### Basic: add one tag, wait, verify
```bash
mock_clear
mock_add e00401001f7812ed 1301111111 DA
sleep 3  # wait for JS scan cycle
# verify DOM
```

### Multi-tag: set exact state
```bash
mock_set '[
    {"sid":"e00401001f77fb98","content":"200000000042","security":"DA"},
    {"sid":"e00401001f7812ed","content":"1301111111","security":"DA"}
]'
```

### Error simulation
```bash
curl -s -X POST -d '{"count":1}' "$MOCK_URL/mock/error"
# Next /scan/ returns 500 → JS shows error
```

## Questions to resolve

1. ✅ **Option A**: port 9000 + TLS — no JS override needed, certs exist in repo (`rfid-localhost.crt` / `rfid-localhost.key`)
2. How to ensure mock is running before test starts?
3. How to cleanup mock state between tests?
