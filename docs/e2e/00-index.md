# E2E Testing — Index

This directory contains the exploration and specification for automated
RFID tests using the mock server + rodney + koha-mysql.

## Files

| File | Topic |
|------|-------|
| `00-index.md` | This index |
| `01-koha-mysql.md` | Database verification via koha-mysql |
| `02-rodney-helper.md` | Reusable rodney test script patterns |
| `03-mock-control.md` | Mock server shell control |

## Data files (used by test runner)

| File | Purpose |
|------|---------|
| `tests/tags.json` | RFID tag definitions (SID, barcode, security) |
| `tests/pages.json` | Page definitions (URLs, tabs, input IDs) |
| `tests/scenarios.json` | 23 scenarios across 5 tiers |
| `tests/e2e.sh` | Test runner that reads JSON files and drives rodney + mock |

## Quick start

```bash
export KOHA_USER=admin
export KOHA_PASS=password
cd /home/dpavlin/koha-rfid-go
./tests/e2e.sh            # run all scenarios on all pages
./tests/e2e.sh circulation # run all scenarios on circulation.pl
./tests/e2e.sh '' 11       # run scenario 11 on all pages
./tests/e2e.sh circulation 11  # run scenario 11 on circulation.pl
```
