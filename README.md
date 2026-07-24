# Koha RFID – 3M 810 Staff Station

Cross-compiled from Linux to Windows. Zero Windows development environment needed.

## Quick Start

```bash
# One-shot scan to verify reader connectivity
./build/linux/scan -port /dev/ttyUSB0

# Production HTTP server (background)
./build/linux/koha-rfid -port /dev/ttyUSB0 -listen localhost:9000 \
  -allow-origin https://koha.example.org -debug &
```

```cmd
:: Windows – one-shot scan
scan.exe -port COM3

:: Windows – production server
koha-rfid.exe -port COM3 -listen localhost:9000 -allow-origin https://koha.example.org
```

Then install the **Koha plugin** which injects [`koha-rfid.js`](plugin/Koha/Plugin/Rot13/RFID/koha-rfid.js) only on RFID-relevant pages (circulation, returns, renew, mainpage).

Build the plugin KPZ and upload via Koha: Plugins → Upload plugin.

## Architecture

```
┌──────────────────────┐      ┌──────────────────┐      ┌──────────────┐
│   Koha Staff Browser │      │  RFID Server     │      │  3M 810      │
│   (koha-rfid.js)     │◄────►│  (Linux/Windows) │◄────►│  RFID Reader │
│                      │JSON  │                  │Serial│              │
│   /cgi-bin/koha/     │      │  HTTP :9000      │      │  USB→COM     │
│   circulation.pl     │      │  (no SIP2/REST)  │      └──────────────┘
│   returns.pl         │      └──────────────────┘
└──────────────────────┘
```

### Workflow (Patron at desk)

1. Librarian opens Koha staff interface in browser
2. Selects patron on **circulation.pl** (checkout) or **returns.pl** (check-in)
3. Patron places book on RFID pad
4. RFID server detects tag → reads barcode from RFID501 blocks
5. JavaScript (`koha-rfid.js`) fills barcode field and submits Koha's own form
6. Koha processes checkout/check-in natively (no external API calls)
7. JavaScript calls `/secure` to set AFI bit (D7 = unsecure, DA = secure)
8. Status shown in browser toolbar

### Why no Koha REST API or SIP2?

The existing `koha-rfid.js` userscript already handles all Koha page interaction:
- Detects which page the staff is on (circulation.pl / returns.pl)
- Fills the barcode input and submits the form
- Koha's own logic performs checkout/check-in
- Only AFI changes need the RFID program's HTTP API

The RFID server only needs to:
1. Scan tags and expose barcodes via JSON
2. Change AFI bits when instructed by the JavaScript

## Binaries

Three executables are built from the same Go codebase. Native Linux builds use the `build/linux/` directory; Windows cross-compiles produce `.exe` files.

| Binary | Purpose |
|---|---|
| `koha-rfid` / `koha-rfid.exe` | HTTPS/JSON server (production use) |
| `scan` / `scan.exe` | CLI scan tool with enter/leave detection |
| `program` / `program.exe` | CLI tag programming tool |

## Server Flags (koha-rfid)

| Flag | Default | Description |
|---|---|---|
| `-port` | `/dev/ttyUSB0` | Serial port for 3M RFID reader |
| `-debug` | `false` | Enable protocol debug logging |
| `-listen` | `localhost:9000` | HTTP server listen address |
| `-allow-origin` | `""` | Koha origin allowed to make browser API calls, e.g. `https://koha.example.org` |
| `-scan` | `false` | Scan once and exit (no HTTP server) |

The `-scan` flag runs a one-shot inventory scan, prints tag SIDs, AFI, and RFID501 decoded content, then exits. Useful for quick diagnostics without starting the full HTTP server.

```bash
koha-rfid -port /dev/ttyUSB0 -scan
```

## API Endpoints (koha-rfid HTTP server)

| Endpoint | Method | Description |
|---|---|---|
| `/` | GET | HTML status page |
| `/scan/` | GET | Live inventory scan → tag list with AFI + RFID501 barcode |
| `/secure` | POST | Set AFI byte (returns JSON with ok/error) |
| `/program` | POST | RFID501 encode + write blocks + auto AFI |


### `/scan/`

Performs a **live** RFID inventory scan each request. Returns JSON with tag SIDs, decoded RFID501 content, AFI security byte, and reader info.

Sample response:
```json
{"tags":[{"sid":"E2001234567890AB","content":"1301234567","security":"DA","tag_type":"RFID501","reader":"3M810"}]}
```

### `/secure`

Writes an AFI byte to a tag. Send a form-encoded `POST` with an `X-RFID-Client: koha-rfid` header, for example `E2001234567890AB=DA`. Browser callers must come from the configured `-allow-origin` value.

**AFI constants:**
- `DA` (0xDA, decimal 218) = **secure** – item checked in, security gate ignores tag
- `D7` (0xD7, decimal 214) = **unsecure** – item checked out, security gate beeps

### `/program`

Programs RFID tags with RFID501-encoded barcode content. Send a form-encoded `POST` with an `X-RFID-Client: koha-rfid` header, for example `E2001234567890AB=1301234567`. RFID501 barcode content is limited to 16 bytes and longer input is rejected.

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
2025-06-27T19:30:00 reader 3M810 enter E2001234567890AB AFI: DA { content => "1301234567", type => 1 (Book), set => 1, total => 1, branch => 0, library => 0, custom => 0 }
2025-06-27T19:30:01 visible: E2001234567890AB
2025-06-27T19:30:02 leave E2001234567890AB
```

**scan flags:**

| Flag | Default | Description |
|---|---|---|
| `-port` | `/dev/ttyUSB0` | Serial port |
| `-debug` | `false` | Protocol debug logging |
| `-loop` | `false` | Continuously scan (default: one-shot) |
| `-log` | `""` | CSV log file path for tag appearances |

### program (replaces program.pl)

Programs an RFID tag with RFID501-encoded content and optional AFI.

```bash
# Linux – write barcode (auto-detect item type)
./build/linux/program -port /dev/ttyUSB0 E2001234567890AB 1301234567

# Linux – comma-separated SID and barcode
./build/linux/program -port /dev/ttyUSB0 E2001234567890AB,1301234567

# Linux – specify item type and AFI
./build/linux/program -port /dev/ttyUSB0 -type 6 -afi 214 E2001234567890AB 1301234567

# Linux – write generic blank tag (3 zero blocks)
./build/linux/program -port /dev/ttyUSB0 -blank E2001234567890AB

# Linux – write 3M manufacturing blank (6× 0x55 + zeros)
./build/linux/program -port /dev/ttyUSB0 -3mblank E2001234567890AB

# Linux – set AFI only (no content write)
./build/linux/program -port /dev/ttyUSB0 -afi 218 E2001234567890AB
```

```cmd
:: Windows – write barcode (auto-detect item type)
program.exe -port COM3 E2001234567890AB 1301234567

:: Windows – comma-separated SID and barcode
program.exe -port COM3 E2001234567890AB,1301234567

:: Windows – specify item type and AFI
program.exe -port COM3 -type 6 -afi 214 E2001234567890AB 1301234567

:: Windows – write generic blank tag (3 zero blocks)
program.exe -port COM3 -blank E2001234567890AB

:: Windows – write 3M manufacturing blank (6× 0x55 + zeros)
program.exe -port COM3 -3mblank E2001234567890AB

:: Windows – set AFI only (no content write)
program.exe -port COM3 -afi 218 E2001234567890AB
```

**program flags:**

| Flag | Default | Description |
|---|---|---|
| `-port` | `/dev/ttyUSB0` | Serial port |
| `-debug` | `false` | Protocol debug logging |
| `-afi N` | `0` | AFI byte to write (214=DA secure, 218=D7 unsecure) |
| `-type N` | `0` | RFID501 item type (1=Book, 6=CD, 2=Magazine, etc.) |
| `-set N` | `1` | Set index (0-15) |
| `-total N` | `1` | Total items in set (0-15) |
| `-branch N` | `0` | Branch number (0-4095) |
| `-library N` | `0` | Library number (0-1048575) |
| `-blank` | — | Write generic blank tag (3 zero blocks) |
| `-3mblank` | — | Write 3M manufacturing blank (6× 0x55 + zeros) |

When `-type` is not set, the tool auto-detects: barcodes starting with `130` get `type=1 (Book)`, otherwise `type=0 (Other)`.

## Scanning

The `/scan/` endpoint performs a **live** inventory scan for every request. The reader is serialized with a mutex so a scan and a tag write cannot interleave on the serial connection. There is no background scan cache.

## Build

Requires Go ≥ 1.18. A convenience build script is also available:

```bash
./build.sh
```

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
./build/linux/program -port /dev/ttyUSB0 E2001234567890AB 1301234567
```

## Deployment on Windows

Copy the needed `.exe` files to the staff PC.

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
program.exe -port COM3 E2001234567890AB 1301234567
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

## Building and Deploying

A `Makefile` orchestrates the full workflow:

```bash
# Build Go binaries for Linux and Windows
make build

# Build for one platform only
make build-linux
make build-windows

# Deploy koha-rfid.js to Koha server (SCP + restart plack)
make deploy-js

# Deploy RFID.pm plugin module to Koha server
make deploy-plugin

# Deploy both JS and plugin
make deploy

# Build KPZ plugin package (upload to Koha: Plugins → Upload plugin)
make kpz

# Clean build artifacts
make clean
```

Set version for ldflags:
```bash
VERSION=1.0.0 make build
```

Under the hood `make build` calls [`build.sh`](build.sh) which cross-compiles Go binaries to `build/linux/` and `build/windows/`. Deploy targets call [`deploy.sh`](deploy.sh) and [`deploy-plugin.sh`](deploy-plugin.sh) which SCP files to `koha-dev.rot13.org` and restart plack.

### Quick deploy workflow after editing

```bash
make build          # compile server + CLI tools
make deploy         # push JS + plugin to Koha server
./server.sh start   # start RFID server
```

## Koha JavaScript Integration

The [`koha-rfid.js`](plugin/Koha/Plugin/Rot13/RFID/koha-rfid.js) is injected by the Koha plugin on RFID-relevant pages (circulation, returns, renew, mainpage).

The script will:
- Poll the RFID server every 1s
- Detect which Koha page the staff is on (circulation.pl / returns.pl)
- When a tag appears, fill the barcode field and submit the form
- Call `/secure` to change AFI bits
- Show tag status in the toolbar

## 3M RFID Protocol Details (from reverse engineering)

Based on [`Biblio::RFID::Reader::3M810`](https://github.com/dpavlin/Biblio-RFID) (Perl). CRC verified against known test vectors.

- **Serial**: 19200 8N1, no handshake
- **Frame**: `D6` + 2-byte big-endian length + payload + 2-byte CRC
- **CRC**: Modified CCITT-16, init=0xFFFF, xorout=0xFFFF, poly=0x1021, refin=false
- **Probe**: `D5 00 05 04 00 11 8C66` → 4-byte hardware version
- **Inventory**: `FE 00 05` → tag count + 8-byte IDs
- **Read blocks**: `02 <tag8> <start> <blocks>` → block data
- **Write blocks**: `04 <tag8> <start> <blocks> <data>` → status (verified by read-back, **retry up to 10×**)
- **Read AFI**: `0A <tag8>` → AFI byte
- **Write AFI**: `09 <tag8> <afi>` → status (**retry up to 100×**)

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
