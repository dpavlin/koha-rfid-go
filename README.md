# Koha RFID – 3M 810 Windows Staff Station

Cross-compiled from Linux to Windows. Zero Windows required for development.

## Architecture

```
┌──────────────────────┐      ┌──────────────────┐      ┌──────────────┐
│   Koha Staff Browser │      │  RFID Program    │      │  3M 810      │
│   (userscript)       │◄────►│  (Windows .exe)  │◄────►│  RFID Reader │
│                      │JSONP │                  │Serial│              │
│   /cgi-bin/koha/     │      │  HTTP :9000      │      │  USB→COM     │
│   circulation.pl     │      │  SIP2 / REST API │─────►│              │
│   returns.pl         │      │  → Koha server   │      └──────────────┘
└──────────────────────┘      └──────────────────┘
```

### Workflow (Patron at desk)

1. Librarian opens Koha staff interface in browser
2. Selects patron on **circulation.pl** (checkout) or **returns.pl** (check-in)
3. Patron places book on RFID pad
4. RFID program detects tag → reads barcode from block 0
5. Program calls Koha REST API or SIP2 to perform checkout/checkin
6. If successful → writes AFI bit (D7 = unsecure/checked-out, DA = secure/checked-in)
7. JavaScript overlay updates UI, shows status

### API Endpoints (compatible with existing `koha-rfid.js`)

| Endpoint | Method | Description |
|---|---|---|
| `/scan/` | GET | JSONP inventory scan → tag list with AFI + barcode |
| `/scan/only/<filter>` | GET | Filter tags by reader name |
| `/secure?<TAG>=<AFI>` | GET | Set AFI byte for a tag |
| `/secure.js?<TAG>=<AFI>&callback=...` | GET | JSONP version of secure |
| `/program?<TAG>=<content>` | GET | Write blocks + set AFI |
| `/sip2/checkout/<patron>/<barcode>/<sid>` | GET | SIP2 checkout + AFI change |
| `/sip2/checkin/<barcode>/<sid>` | GET | SIP2 check-in + AFI change |

## Build for Windows from Linux

Requires Go ≥ 1.18 on the Linux build machine.

```bash
# Clone or copy files to your Linux machine
cd koha-rfid

# Download dependencies (serial library)
go mod tidy

# Cross-compile for Windows (64-bit)
GOOS=windows GOARCH=amd64 go build -o koha-rfid.exe .

# Single static binary – no DLLs needed
file koha-rfid.exe
# → PE32+ executable (GUI) ... (actually a console binary)
```

## Deployment on Windows

Copy `koha-rfid.exe` to the staff PC. Run as a background service (use `nssm` or Task Scheduler).

```cmd
# Quick test (scan once and exit)
koha-rfid.exe -com COM3 -scan

# Full server mode
koha-rfid.exe ^
  -com COM3 ^
  -listen localhost:9000 ^
  -koha-url http://koha.example.com:8080 ^
  -sip-server 10.0.0.1:6002 ^
  -sip-user selfcheck ^
  -sip-pass secret ^
  -sip-loc kohalibrary ^
  -debug
```

### Windows Service with NSSM

```cmd
nssm install KohaRFID ^
  "C:\path\to\koha-rfid.exe" ^
  -com COM3 -listen localhost:9000 ^
  -koha-url http://koha.example.com:8080
nssm start KohaRFID
```

### Koha JavaScript Integration

Inject the existing `koha-rfid.js` into Koha's `intranetuserjs` (System Preferences → IntranetUserJS):

```javascript
$.getScript('http://localhost:9000/examples/koha-rfid.js');
```

The script will:
- Poll the RFID server every 500ms
- Detect which Koha page the staff is on (circulation.pl / returns.pl)
- When a tag appears, fill the barcode field and submit the form
- Call `/secure.js` to change AFI bits
- Show tag status in the toolbar

## 3M RFID Protocol Details (from reverse engineering)

Based on `Biblio::RFID::Reader::3M810` (Perl).

- **Serial**: 19200 8N1, no handshake
- **Frame**: `D6` + 2-byte big-endian length + payload + 2-byte CRC
- **CRC**: CCITT-16, init=0xFFFF, xorout=0xFFFF, poly=0x1021, refin=false
- **Probe**: `D5 00 05 04 00 11 8C66` → 4-byte hardware version
- **Inventory**: `FE 00 05` → tag count + 8-byte IDs
- **Read blocks**: `02 <tag8> <start> <blocks>` → block data
- **Write blocks**: `04 <tag8> <start> <blocks> <data>` → status
- **Read AFI**: `0A <tag8>` → AFI byte
- **Write AFI**: `09 <tag8> <afi>` → status (retry up to 100x)

## Alternative Languages

| Language | Cross-compile | Binary size | Effort |
|---|---|---|---|
| **Go** | `GOOS=windows go build` | ~5 MB static | Low ★ |
| **Rust** | `--target x86_64-pc-windows-gnu` | ~1 MB | Medium |
| **C + MinGW** | `x86_64-w64-mingw32-gcc` | ~100 KB | High |
| **Python** | Requires Windows VM + PyInstaller | ~30 MB | Very high |
| **Nim** | `--os:win64 --cc:gcc` | ~1 MB | Medium |

**Go is recommended** because:
- Single static binary, no runtime
- Serial library works natively on Windows COM ports
- Cross-compilation is a one-liner
- Excellent standard library for HTTP, binary protocols, CRC

## Source Files

| File | Lines | Purpose |
|---|---|---|
| `rfid.go` | 375 | 3M 810 serial protocol (CRC, framing, commands) |
| `koha.go` | 207 | Koha REST API + SIP2 client |
| `server.go` | 329 | HTTP/JSONP server (compatible with koha-rfid.js) |
| `main.go` | 139 | CLI flags, event loop, signal handling |
| `go.mod` | – | Module with serial dependency |

## License

GPL v2 (same as original Biblio-RFID)
