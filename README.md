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
4. RFID program detects tag → reads barcode from RFID501 blocks
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

## Binaries

Three Windows executables are built from the same Go codebase:

| Binary | Size | Purpose |
|---|---|---|
| `koha-rfid.exe` | 8.9 MB | HTTP/JSONP server + background scan (production use) |
| `scan.exe` | 3.1 MB | CLI scan tool with enter/leave detection |
| `program.exe` | 3.1 MB | CLI tag programming tool |

## API Endpoints (koha-rfid.exe HTTP server)

| Endpoint | Method | Description |
|---|---|---|
| `/` | GET | HTML status page |
| `/scan/` | GET | JSONP inventory scan → tag list with AFI + RFID501 barcode |
| `/scan/only/<filter>` | GET | Filter tags by reader name (future) |
| `/secure?<TAG>=<AFI>` | GET | Set AFI byte (redirects back) |
| `/secure.js?<TAG>=<AFI>&callback=...` | GET | JSONP version of secure |
| `/program?<TAG>=<content>` | GET | RFID501 encode + write blocks + auto AFI |

## CLI Tools

### scan.exe (replaces scan.pl)

Continuously scans RFID tags with enter/leave detection. Prints ISO date, tag SID, AFI, and RFID501 decoded hash.

```cmd
:: One-shot scan
scan.exe -com COM3

:: Continuous loop (Ctrl+C to stop)
scan.exe -com COM3 -loop

:: Continuous loop with CSV logging
scan.exe -com COM3 -loop -log tags.csv
```

Output format:
```
2025-06-27T19:30:00 reader 3M810 enter E2001234567890ABCDEF AFI: DA { content => "1301234567", type => 1 (Book), set => 1, total => 1, branch => 0, library => 0, custom => 0 }
2025-06-27T19:30:01 visible: E2001234567890ABCDEF
2025-06-27T19:30:02 leave E2001234567890ABCDEF
```

### program.exe (replaces program.pl)

Programs an RFID tag with RFID501-encoded content and optional AFI.

```cmd
:: Write barcode to tag (auto-detect item type)
program.exe -com COM3 E2001234567890ABCDEF 1301234567

:: Comma-separated SID and barcode
program.exe -com COM3 E2001234567890ABCDEF,1301234567

:: Specify item type and AFI
program.exe -com COM3 -type 6 -afi 214 E2001234567890ABCDEF 1301234567

:: Write generic blank tag (3× zero blocks)
program.exe -com COM3 -blank E2001234567890ABCDEF

:: Write 3M manufacturing blank (6× 0x55 + zeros)
program.exe -com COM3 -3mblank E2001234567890ABCDEF

:: Set AFI only (no content write)
program.exe -com COM3 -afi 218 E2001234567890ABCDEF
```

Options:
- `-com` – Serial port (default COM3)
- `-debug` – Enable protocol debug logging
- `-afi N` – AFI byte to write (214 = secure/DA, 218 = unsecure/D7)
- `-type N` – RFID501 item type (1=Book, 6=CD, 2=Magazine, etc.)
- `-set N` / `-total N` – Set index and total items (0-15)
- `-branch N` – Branch number (0-4095)
- `-library N` – Library number (0-1048575)
- `-blank` – Write generic blank tag
- `-3mblank` – Write 3M manufacturing blank

## Build for Windows from Linux

Requires Go ≥ 1.18 on the Linux build machine.

```bash
# Clone or copy files to your Linux machine
cd koha-rfid-go

# Download dependencies (serial library)
go mod tidy

# Cross-compile all binaries for Windows (64-bit)
GOOS=windows GOARCH=amd64 go build -o koha-rfid.exe .
GOOS=windows GOARCH=amd64 go build -o scan.exe ./cmd/scan
GOOS=windows GOARCH=amd64 go build -o program.exe ./cmd/program

# Verify Windows PE32+ binaries
file koha-rfid.exe scan.exe program.exe
# → PE32+ executable for MS Windows 6.01 (console), x86-64

# All binaries are static – no DLLs needed
```

## Deployment on Windows

Copy the needed `.exe` files plus `examples/` folder to the staff PC.

### Quick scan test
```cmd
scan.exe -com COM3
```

### Full server mode (command prompt)
```cmd
koha-rfid.exe -com COM3 -listen localhost:9000 -debug
```

### Program a tag
```cmd
program.exe -com COM3 E2001234567890ABCDEF 1301234567
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

## RFID501 Tag Format

The program uses the 3M RFID501 standard (8 blocks × 4 bytes = 32 bytes):

```
Block 0: [04] [set/total nibbles] [00] [item type]
Blocks 1-4: 16-byte null-padded ASCII barcode
Block 5: branch(12 bits) + library(20 bits), big-endian
Block 6: custom signed integer, big-endian
Block 7: zero (must be 0x00000000)
```

**Item types**: 1=Book, 6=CD, 2=Magazine, 13=Book+Audio, 9=Book+CD, 0=Other

The scan endpoint decodes RFID501 blocks to extract properly null-padded barcodes. The program endpoint encodes content using the full 8-block format before writing. Blank tags use 3× zero blocks.

## Files

| File | Lines | Purpose |
|---|---|---|
| `internal/rfid/reader.go` | 390 | 3M 810 serial protocol (CRC, framing, all commands) |
| `internal/rfid/rfid501.go` | 170 | RFID501 tag encode/decode (8-block format, item types, barcode extraction) |
| `internal/rfid/rfid501_test.go` | 80 | Unit tests for RFID501 roundtrip encode/decode |
| `main.go` | 120 | HTTP server + background scan loop entry point |
| `server.go` | 260 | HTTP/JSONP server with RFID501 integration |
| `cmd/scan/main.go` | 140 | CLI scan tool (replaces scan.pl) |
| `cmd/program/main.go` | 130 | CLI program tool (replaces program.pl) |
| `examples/koha-rfid.js` | – | Browser userscript for Koha integration |
| `start.bat` | – | Windows double-click launcher |

## License

GPL v2 (same as original Biblio-RFID)
