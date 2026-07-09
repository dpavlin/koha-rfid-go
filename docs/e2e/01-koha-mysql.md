# koha-mysql — Database Verification

## Purpose

Query Koha's MySQL/MariaDB to verify that RFID-driven form submissions
actually changed the database (checkout, checkin, renew, patron search).

## How to connect

`koha-mysql` runs on the Koha server (`ffzg.koha-dev.rot13.org`), not locally.
Tests need to SSH into the server to run queries.

```bash
# Via SSH (requires sudo on remote)
ssh koha-dev.rot13.org sudo /usr/sbin/koha-mysql ffzg -e "SELECT ..."
```

## Connection details (verified)

- Host: `ffzg.koha-dev.rot13.org`
- SSH user: `dpavlin` (key-based auth)
- Instance name: `ffzg`
- Command: `sudo /usr/sbin/koha-mysql ffzg -e "<query>"`
- Works with `SELECT 1` → returns `1`

## Verified barcodes in database

All four mock-rfid tag barcodes exist:

| SID | Barcode | Type | DB table | DB row |
|-----|---------|------|----------|--------|
| `e00401001f77fb98` | `200000000042` | Patron | `borrowers` | Dobrica Pavlinušić |
| `e00401001f7812ed` | `1301111111` | Book | `items` | exists |
| `e00401003126a0c8` | `1302079605` | Book | `items` | exists |
| `e004010031269117` | `1302099999` | Book | `items` | exists |

## Tables to query

## Questions to resolve

1. ✅ `koha-mysql` command: `sudo /usr/sbin/koha-mysql ffzg -e "<query>"`
2. ✅ Instance name: `ffzg`
3. ✅ All four mock-rfid barcodes exist in Koha DB
4. ✅ SSH works with key-based auth as `dpavlin`
5. ❓ Should we tunnel MySQL port or run commands via SSH? — **SSH is fine for now**
