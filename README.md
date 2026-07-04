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

Three executables are built from the same Go codebase. Native Linux builds use the `build/linux/` directory; Windows cross-compiles produce `.exe` files.

| Binary | Purpose |
|---|---|
| `koha-rfid` / `koha-rfid.exe` | HTTP/JSONP server + background scan (production use) |
| `scan` / `scan.exe` | CLI scan tool with enter/leave detection |
| `program` / `program.exe` | CLI tag programming tool |

## API Endpoints (koha-rfid HTTP server)

| Endpoint | Method | Description |
|---|---|---|
| `/` | GET | HTML status page |
| `/scan/` | GET | JSONP inventory scan → tag list with AFI + RFID501 barcode |
| `/secure?<TAG>=<AFI>` | GET | Set AFI byte (redirects back) |
| `/secure.js?<TAG>=<AFI>&callback=...` | GET | JSONP version of secure |
| `/program?<TAG>=<barcode>` | GET | RFID501 encode + write blocks + auto AFI. Content `blank` writes 3 zero blocks and sets AFI unsecure. Multiple tags can be programmed in one request. |

## CLI Tools

### scan (replaces scan.pl)

Continuously scans RFID tags with enter/leave detection. Prints ISO date, tag SID, AFI, and RFID501 decoded hash.

```bash
# Linux – one-shot scan
./build/linux/scan -port /dev/ttyUSB0

# Linux – continuous loop (Ctrl+C to stop)
./build/linux/scan -port /dev/ttyUSB0 -loop

# Linux – continuous loop with CSV logging
./build/linux/scan -port /dev/ttyUSB0 -loop -log tags.csv
```

```cmd
:: Windows – one-shot scan
scan.exe -port COM3

:: Windows – continuous loop
scan.exe -port COM3 -loop

:: Windows – continuous loop with CSV logging
scan.exe -port COM3 -loop -log tags.csv
```

Output format:
```
2025-06-27T19:30:00 reader 3M810 enter E2001234567890ABCDEF AFI: DA { content => "1301234567", type => 1 (Book), set => 1, total => 1, branch => 0, library => 0, custom => 0 }
2025-06-27T19:30:01 visible: E2001234567890ABCDEF
2025-06-27T19:30:02 leave E2001234567890ABCDEF
```

### program (replaces program.pl)

Programs an RFID tag with RFID501-encoded content and optional AFI.

```bash
# Linux – write barcode (auto-detect item type)
./build/linux/program -port /dev/ttyUSB0 E2001234567890ABCDEF 1301234567

# Linux – comma-separated SID and barcode
./build/linux/program -port /dev/ttyUSB0 E2001234567890ABCDEF,1301234567

# Linux – specify item type and AFI
./build/linux/program -port /dev/ttyUSB0 -type 6 -afi 214 E2001234567890ABCDEF 1301234567

# Linux – write generic blank tag (3 zero blocks)
./build/linux/program -port /dev/ttyUSB0 -blank E2001234567890ABCDEF

# Linux – write 3M manufacturing blank (6× 0x55 + zeros)
./build/linux/program -port /dev/ttyUSB0 -3mblank E2001234567890ABCDEF

# Linux – set AFI only (no content write)
./build/linux/program -port /dev/ttyUSB0 -afi 218 E2001234567890ABCDEF
```

```cmd
:: Windows – write barcode (auto-detect item type)
program.exe -port COM3 E2001234567890ABCDEF 1301234567

:: Windows – comma-separated SID and barcode
program.exe -port COM3 E2001234567890ABCDEF,1301234567

:: Windows – specify item type and AFI
program.exe -port COM3 -type 6 -afi 214 E2001234567890ABCDEF 1301234567

:: Windows – write generic blank tag (3 zero blocks)
program.exe -port COM3 -blank E2001234567890ABCDEF

:: Windows – write 3M manufacturing blank (6× 0x55 + zeros)
program.exe -port COM3 -3mblank E2001234567890ABCDEF

:: Windows – set AFI only (no content write)
program.exe -port COM3 -afi 218 E2001234567890ABCDEF
```

Options:
- `-port` – Serial port (default `/dev/ttyUSB0` on Linux, `COM3` on Windows)
- `-debug` – Enable protocol debug logging
- `-afi N` – AFI byte to write (214 = secure/DA, 218 = unsecure/D7)
- `-type N` – RFID501 item type (1=Book, 6=CD, 2=Magazine, etc.)
- `-set N` / `-total N` – Set index and total items (0-15)
- `-branch N` – Branch number (0-4095)
- `-library N` – Library number (0-1048575)
- `-blank` – Write generic blank tag
- `-3mblank` – Write 3M manufacturing blank

## Build

Requires Go ≥ 1.18.

### Linux native

```bash
# Build all binaries
mkdir -p build/linux
go build -o build/linux/koha-rfid .
go build -o build/linux/scan ./cmd/scan
go build -o build/linux/program ./cmd/program
```

### Windows cross-compile from Linux

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

## Deployment on Linux

Copy the built binaries from `build/linux/` to the staff PC or run directly on the development machine.

```bash
# Quick scan test
./build/linux/scan -port /dev/ttyUSB0

# Full server mode (background)
./build/linux/koha-rfid -port /dev/ttyUSB0 -listen localhost:9000 -debug &

# Program a tag
./build/linux/program -port /dev/ttyUSB0 E2001234567890ABCDEF 1301234567
```

## Deployment on Windows

Copy the needed `.exe` files plus `examples/` folder to the staff PC.

### Quick scan test
```cmd
scan.exe -port COM3
```

### Full server mode (command prompt)
```cmd
koha-rfid.exe -port COM3 -listen localhost:9000 -debug
```

### Program a tag
```cmd
program.exe -port COM3 E2001234567890ABCDEF 1301234567
```

### Windows Service with NSSM (run in background)
```cmd
nssm install KohaRFID "C:\path\to\koha-rfid.exe" -port COM3 -listen localhost:9000
nssm start KohaRFID
```

### Using start.bat (double-click to run)
Create `start.bat` on the staff PC:
```batch
@echo off
koha-rfid.exe -port COM3 -listen localhost:9000
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

Based on [`Biblio::RFID::Reader::3M810`](https://github.com/dpavlin/Biblio-RFID) (Perl). CRC verified against known test vectors.

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

## License

GPL v2 (same as original Biblio-RFID)
