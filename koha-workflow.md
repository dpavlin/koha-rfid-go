# RFID + Koha Integration Workflow

## Overview

The RFID reader detects tags in range. The JavaScript code (`koha-rfid.js`) running inside Koha's intranet pages processes each tag and coordinates between Koha (form submission) and the RFID tag (AFI write).

The critical design principle: **Koha state takes priority over RFID tag state**. The RFID tag AFI is only changed after Koha confirms the operation, using localStorage to persist pending writes across page reloads.

## localStorage Schema

| Key | Value | Purpose |
|-----|-------|---------|
| `rfid_pending` | `{ barcode: { target, current, time } }` | Pending AFI write after Koha form submission (survives page reload) |
| `koha_state` | `{ barcode: 'DA' or 'D7' }` | Cached Koha-verified loan state for each barcode |
| `rfid_last_barcode` | single barcode string | Prevents double-submit on the same page load |
| `rfid_popup_pos` | `{ top, right }` | Draggable popup position |

## Pages and their workflows

### 1. returns.pl — Checkin

**Purpose:** Process returned books — record the return in Koha, then update the RFID tag.

**Trigger:** Any book tag placed on the reader (regardless of current AFI).

**Workflow:**
```
┌──────────────────────────────────────────────┐
│ 1. Scan tag → barcode=X, security=sec        │
│                                               │
│ 2. Store pending_afi[X] = DA                 │
│    (current AFI = sec, target = DA)           │
│                                               │
│ 3. Fill #ret_barcode with X                  │
│    Submit checkin form to Koha                │
│                                               │
│ 4. Page reloads after Koha processes          │
│                                               │
│ 5. On next scan of X:                        │
│    - pending_afi[X] exists                    │
│    - tag AFI still = sec (not yet written)    │
│    → Write AFI = DA to tag                   │
│    → Clear pending_afi[X]                    │
│    → Set koha_state[X] = DA                  │
└──────────────────────────────────────────────┘
```

**Why no AFI filter:** The user must be able to check in books regardless of their current RFID AFI. A book might show DA (checked in) on the tag but still need Koha processing (e.g., previous AFI write failed, or the tag was never updated). Koha's returns page handles this correctly.

### 2. circulation.pl — Checkout

**Purpose:** Check out books to patrons — record the loan in Koha, then update the RFID tag.

**Trigger:** Only books with AFI = DA (checked in, meaning they are in the library and available for checkout).

**Workflow:**
```
┌──────────────────────────────────────────────┐
│ 1. Scan tag → barcode=X, security=DA         │
│                                               │
│ 2. Store pending_afi[X] = D7                 │
│    (current AFI = DA, target = D7)            │
│                                               │
│ 3. Fill input[name=barcode]:last with X      │
│    Submit checkout form to Koha               │
│                                               │
│ 4. Page reloads after Koha processes          │
│                                               │
│ 5. On next scan of X:                        │
│    - pending_afi[X] exists                    │
│    - tag AFI still = DA (not yet written)     │
│    → Write AFI = D7 to tag                   │
│    → Clear pending_afi[X]                    │
│    → Set koha_state[X] = D7                  │
└──────────────────────────────────────────────┘
```

**AFI filter:** Only DA tags are processed. If the tag already shows D7 (on loan), it's already checked out — nothing to do.

### 3. renew.pl — Renewal

**Purpose:** Renew loans — extend the due date in Koha.

**Trigger:** Only books with AFI = D7 (on loan, eligible for renewal).

**Workflow:**
```
┌──────────────────────────────────────────────┐
│ 1. Scan tag → barcode=X, security=D7         │
│                                               │
│ 2. Fill #barcode with X                      │
│    Submit renewal form to Koha                │
│                                               │
│ 3. No AFI write needed                       │
│    (renewal keeps the book on loan, D7 stays) │
└──────────────────────────────────────────────┘
```

**No AFI write:** Renewal does not change the loan status, so the RFID tag AFI stays D7.

### 4. circulation.pl — Patron card

**Purpose:** Look up a patron by scanning their library card.

**Trigger:** Any barcode that doesn't start with `130` (book barcodes start with 130).

**Workflow:**
```
┌──────────────────────────────────────────────┐
│ 1. Scan tag → barcode=200000000042            │
│                                               │
│ 2. Fill input[name=findborrower] with card    │
│    Submit patron search form                  │
└──────────────────────────────────────────────┘
```

## AFI Value Reference

| AFI | Meaning | Door behavior | Koha state |
|-----|---------|---------------|------------|
| DA | Secured (checked in) | No alarm | Book is in library (available for checkout) |
| D7 | Unsecured (checked out) | Alarm beeps | Book is on loan (checked out to patron) |

## Fail-Safe Properties

### Page reload handling
Koha intranet pages reload after form submission. The `rfid_pending` localStorage entry survives the reload, so the AFI write can be completed after Koha confirms the operation.

### State validation (koha_state)
Each barcode's Koha-verified state is cached in localStorage. On future scans, the code can compare the RFID tag AFI with the cached koha_state to detect mismatches (e.g., tag says DA but koha_state says D7 → tag needs updating).

### No double-submit
`rfid_last_barcode` in sessionStorage prevents the same barcode from being submitted multiple times on the same page load. After page reload, sessionStorage is cleared, so the same barcode can be processed again if it's still on the reader.

## Server-side handlers

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/scan/` | GET | Read RFID tags in range |
| `/secure` | POST | Write AFI to tag (JSON response) |
| `/program` | POST | Write barcode content to tag |

## TLS Certificate

- Self-signed, generated on first startup, reused thereafter
- 10-year validity (self-signed certs are not subject to the 398-day public CA limit)
- Located in the server working directory as `rfid-localhost.crt` and `rfid-localhost.key`
