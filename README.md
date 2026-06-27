# Koha RFID – 3M 810 Windows Staff Station

Cross-compiled from Linux to Windows. Zero Windows development environment needed.

## Architecture

```
┌──────────────────────┐      ┌──────────────────┐      ┌──────────────┐
│   Koha Staff Browser │      │  RFID Program    │      │  3M 810      │
│   (koha-rfid.js)     │◄────►│  (Windows .exe)  │◄────►│  RFID Reader │
│                      │JSONP │                  │Serial│              │
│   /cgi-bin/koha/     │      │  HTTP :9000      │      │  USB→COM     │
│   circulation.pl     │      │  (no SIP2/REST)  │      └──────────────┘
│   returns.pl         │      └──────────────────┘
└──────────────────────┘
```

### Workflow (Patron at desk)

1. Librarian opens Koha staff interface in browser
2. Selects patron on **circulation.pl** (checkout) or **returns.pl** (check-in)
3. Patron places book on RFID pad
4. RFID program detects tag → reads barcode from block 0
5. JavaScript (`koha-rfid.js`) fills barcode field and submits Koha's own form
6. Koha processes checkout/check-in natively (no external API calls)
7. JavaScript calls `/secure.js` to set AFI bit (D7 = unsecure, DA = secure)
8. Status shown in browser toolbar

### Why no Koha REST API or SIP2?

The existing `koha-rfid.js` userscript already handles all Koha page interaction:
- Detects which page the staff is on (circulation.pl / returns.pl)
- Fills the barcode input and submits the form
- Koha's own logic performs checkout/check-in
- Only AFI changes need the RFID program's HTTP API

The RFID program only needs to:
1. Scan tags and expose barcodes via JSONP
2. Change AFI bits when instructed by the JavaScript

## API Endpoints (compatible with existing `koha-rfid.js`)

| Endpoint | Method | Description |
|---|---|---|
| `/` | GET | HTML status page |
| `/scan/` | GET | JSONP inventory scan → tag list with AFI + barcode |
| `/scan/only/<filter>` | GET | Filter tags by reader name (future) |
| `/secure?<TAG>=<AFI>` | GET | Set AFI byte (redirects back) |
| `/secure.js?<TAG>=<AFI>&callback=...` | GET | JSONP version of secure |
| `/program?<TAG>=<content>` | GET | Write block 0 + auto AFI |

## Build for Windows from Linux

Requires Go ≥ 1.18 on the Linux build machine.

```bash
# Clone or copy files to your Linux machine
cd koha-rfid-go

# Download dependencies (serial library)
go mod tidy

# Cross-compile for Windows (64-bit console binary)
GOOS=windows GOARCH=amd64 go build -o koha-rfid.exe .

# Verify it's a Windows PE32+ binary
file koha-rfid.exe
# → PE32+ executable for MS Windows 6.01 (console), x86-64

# Single static binary – no DLLs needed
```

## Deployment on Windows

Copy `koha-rfid.exe` plus the `examples/` folder to the staff PC.

### Quick test
```cmd
koha-rfid.exe -com COM3 -scan
```

### Full server mode (command prompt)
```cmd
koha-rfid.exe -com COM3 -listen localhost:9000 -debug
```

### Windows Service with NSSM (run in background)
```cmd
nssm install KohaRFID "C:\path\to\koha-rfid.exe" -com COM3 -listen localhost:9000
nssm start KohaRFID
```

### Using start.bat (double-click to run)
Create `start.bat` on the staff PC:
```batch
@echo off
koha-rfid.exe -com COM3 -listen localhost:9000
pause
```

## Koha JavaScript Integration

Inject the existing `koha-rfid.js` into Koha's `intranetuserjs` (System Preferences → IntranetUserJS):

```javascript
$.getScript('http://localhost:9000/examples/koha-rfid.js');
```

The script will:
- Poll the RFID server every 1s
- Detect which Koha page the staff is on (circulation.pl / returns.pl)
- When a tag appears, fill the barcode field and submit the form
- Call `/secure.js` to change AFI bits
- Show tag status in the toolbar

## 3M RFID Protocol Details (from reverse engineering)

Based on `Biblio::RFID::Reader::3M810` (Perl). CRC verified against known test vectors.

- **Serial**: 19200 8N1, no handshake
- **Frame**: `D6` + 2-byte big-endian length + payload + 2-byte CRC
- **CRC**: Modified CCITT-16, init=0xFFFF, xorout=0xFFFF, poly=0x1021, refin=false
- **Probe**: `D5 00 05 04 00 11 8C66` → 4-byte hardware version
- **Inventory**: `FE 00 05` → tag count + 8-byte IDs
- **Read blocks**: `02 <tag8> <start> <blocks>` → block data
- **Write blocks**: `04 <tag8> <start> <blocks> <data>` → status (verified by read-back, retry up to 10×)
- **Read AFI**: `0A <tag8>` → AFI byte
- **Write AFI**: `09 <tag8> <afi>` → status (retry up to 100×)

## Files

| File | Lines | Purpose |
|---|---|---|
| `rfid.go` | 390 | 3M 810 serial protocol (CRC, framing, all commands) |
| `server.go` | 230 | HTTP/JSONP server (compatible with koha-rfid.js) |
| `main.go` | 119 | CLI flags, background scan loop, signal handler |
| `koha-rfid.exe` | – | Windows PE32+ binary (8.9 MB) |
| `examples/koha-rfid.js` | – | Browser userscript for Koha integration |
| `start.bat` | – | Windows double-click launcher |

## License

GPL v2 (same as original Biblio-RFID)
