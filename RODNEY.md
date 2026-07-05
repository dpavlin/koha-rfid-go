# Driving Koha RFID workflow with rodney

[rodney](https://github.com/akavel/rodney) is a CLI tool for Chrome automation
via the Chrome DevTools Protocol (CDP). It lets you drive a real Chrome browser
step by step from the command line — examine the page, interact, and decide what
to do next based on what you see.

## Setup

Chrome must be running with `--remote-debugging-port=9333` (or your chosen port).
Rodney connects to it; it does not launch Chrome itself.

```bash
export CDP_PORT=9333                # default, can override
export RODNEY_HOME=~/.rodney        # session state directory
```

## Basic commands

### Connect and inspect

```bash
# Connect to running Chrome and list open tabs
uvx rodney connect localhost:$CDP_PORT
uvx rodney pages

# Navigate to a URL (opens in a new tab)
uvx rodney open "https://ffzg.koha-dev.rot13.org:8443/cgi-bin/koha/mainpage.pl"

# Or open in a specific existing tab
uvx rodney page 3                  # switch to tab index 3
uvx rodney open <url>              # navigates current tab

# Examine the page
uvx rodney url                     # current URL
uvx rodney title                   # page title
uvx rodney html                    # full page HTML
uvx rodney html '#login'           # HTML of a specific element
uvx rodney text '#login form'      # visible text of an element
```

### Interact

```bash
# Type into fields
uvx rodney input 'input[name=userid]' your_username
uvx rodney input 'input[name=password]' your_password

# Click buttons
uvx rodney click 'input#login'     # login button
uvx rodney click 'a[href*="circ"]' # circulation link

# Wait for page changes
uvx rodney wait '#borrower_results'
uvx rodney waitload
uvx rodney sleep 2                 # pause N seconds
```

### JavaScript evaluation

```bash
# Run arbitrary JS and see the result
uvx rodney js 'document.body.innerText'
uvx rodney js 'document.querySelector("#rfid").textContent'
uvx rodney js 'JSON.stringify(window.__consoleLogs || [])'
```

### Element checks (exit code 0=found, 1=not found)

```bash
uvx rodney exists '#rfid'
uvx rodney visible '#rfid_popup'
uvx rodney count 'input[name=barcode]'
```

### Tabs

```bash
uvx rodney pages                   # list all tabs with indices
uvx rodney page 3                  # switch to tab 3
uvx rodney newpage <url>           # open URL in a new tab
uvx rodney closepage 3             # close tab 3
```

## RFID workflow — step by step

### 1. Open Koha intranet

```bash
uvx rodney open "https://ffzg.koha-dev.rot13.org:8443/cgi-bin/koha/mainpage.pl"
```

Wait for login page. Log in manually in the browser.

### 2. Verify login

```bash
uvx rodney title
# Should show something like "Koha › Welcome" with your name
uvx rodney text '.loggedinusername'
```

### 3. Navigate to circulation

```bash
uvx rodney open "https://ffzg.koha-dev.rot13.org:8443/cgi-bin/koha/circ/circulation.pl"
uvx rodney wait '#findborrower'     # wait for patron search input
```

### 4. Search for borrower

```bash
uvx rodney input 'input[name=findborrower]' dobrica
uvx rodney wait '#patron_results'   # wait for results
uvx rodney text '#patron_results'   # verify results appeared
```

### 5. Select borrower

```bash
uvx rodney click '#patron_results a'
uvx rodney waitload
```

### 6. Check RFID polling

The Koha page loads `koha-rfid.js` which polls `///localhost:9000/scan`.
Start the RFID server first:

```bash
RFID_PORT=/dev/ttyUSB0 go run . -scan
# or in background:
RFID_PORT=/dev/ttyUSB0 go run . &
```

Then check RFID state:

```bash
uvx rodney exists '#rfid'
uvx rodney text '#rfid'
uvx rodney text '#rfid-info'
uvx rodney js 'document.querySelector("input[name=barcode]").value'
```

### 7. Capture console logs

The RFID JS logs activity to console. Capture it with:

```bash
uvx rodney js 'JSON.stringify(window.__consoleLogs || [])'
```

Or install a capture hook after page load:

```bash
uvx rodney js '
(function() {
    window.__consoleLogs = [];
    var orig = console.log;
    console.log = function() {
        var args = Array.from(arguments).map(function(a) {
            if (typeof a === "object") return JSON.stringify(a);
            return String(a);
        }).join(" ");
        window.__consoleLogs.push(args);
        orig.apply(console, arguments);
    };
})();
'
```

## Troubleshooting

**"no pages listed"** — rodney uses a global session (`~/.rodney/state.json`).
Use `--global` flag explicitly if working from a different directory.

**Page opens but not visible in Chrome** — check that Chrome started with
`--remote-debugging-port=9333` (not headless, not in another display).
Rodney opens tabs in the browser it connects to; if Chrome is on another
machine you need to tunnel the port.

**`open` doesn't navigate** — use `newpage <url>` instead, or first switch to
the tab with `page <index>` then `open`.

**Commands return only connection info** — rodney prints the connection banner
on stderr; actual command output is on stdout. Some commands (like `html`)
may return empty if the page hasn't loaded — add a `waitload` or `sleep`.

## Practical debugging notes (from RFID integration)

### Tab management

- **Pages reorder** when tabs are opened/closed. Always run `pages` first to
  confirm indices — do NOT assume index 0 is Koha.
- **DevTools tab** appears as a separate tab when DevTools is detached. The
  Koha page shifts to a different index.
- **`newpage <url>`** opens the new URL as tab 0, shifting all existing tabs
  up by one. Use `newpage` then `pages` to find the right index.
- **`page <index>`** switches the *current* tab context. All subsequent
  commands apply to that tab until you switch again.

### JavaScript evaluation pitfalls

Rodney's `js` command accepts only a **single expression** — no statements,
no blocks, no `var`/`let`/`const`, no `try/catch`, no `if` statements.

**DO:**
```bash
uvx rodney js 'typeof RFID_VERSION'
uvx rodney js '$("#rfid-popup-body").length'
uvx rodney js '$("#rfid-popup")[0].outerHTML'
uvx rodney js '(function(){return JSON.parse(localStorage.getItem("rfid_events")||"[]").slice(-5).map(function(e){return e.action+" "+e.detail}).join(", ")})()'
```

**DON'T:**
```bash
# These will all fail with SyntaxError:
uvx rodney js 'var x = 1; x + 1'
uvx rodney js 'if (true) { "ok" }'
uvx rodney js 'try { foo() } catch(e) { "err" }'
```

To run multi-step logic, wrap in an IIFE:
```bash
uvx rodney js '(function(){var r=[];r.push("hello");return JSON.stringify(r)})()'
```

### jQuery return values

jQuery methods return jQuery objects. Commands that invoke `.append()`,
`.trigger()`, `.prop()` will print **the entire jQuery object** (including
event handlers) to stdout — very verbose. Use these workarounds:

```bash
# Instead of inspecting the jQuery return, check the DOM directly:
uvx rodney js '$("#rfid-popup-body")[0].outerHTML'   # actual DOM HTML
uvx rodney js '$("#rfid-events-log").length'          # 0 or 1
uvx rodney js '$("#rfid-events-check").prop("checked")'  # boolean
uvx rodney text '#rfid-popup-body'                    # visible text
```

### Click and form interaction

- **Click on checkbox** may not toggle if drag handlers intercept the event
  (popup has drag-to-move). Use programmatic toggle instead:
  ```bash
  uvx rodney js '$("#rfid-events-check").prop("checked",false).trigger("change")'
  uvx rodney js '$("#rfid-events-check").prop("checked",true).trigger("change")'
  ```
  The `.trigger("change")` return is verbose; check state afterward with
  `.prop("checked")` and `$("#rfid-events-log").length`.

- **Form submission** via `.submit()` will reload the page. After reload,
  rodney commands may fail with "ReferenceError: $ is not defined" because
  the page hasn't finished loading. Add a `sleep 5` before querying.

### Page reload and script injection

- **`reload --hard`** clears the browser cache. If the RFID script is
  injected via intranetuserjs `$.getScript(...)`, the hard reload may cause
  the script to not load (cache miss for the RFID server's HTTPS endpoint).
  Use `reload` (soft) or wait for the script to load after hard reload.

- After a page reload, jQuery may not be immediately available. Wait:
  ```bash
  sleep 5
  uvx rodney js 'typeof $'          # "function" when ready
  uvx rodney js 'RFID_VERSION'      # "1.0" when script loaded
  ```

### TLS / certificate issues

The RFID server uses a self-signed TLS certificate. Chrome refuses to fetch
`https://localhost:9000/scan/` from the Koha page until the certificate is
explicitly accepted:

```bash
# Open the RFID server page in a new tab and let Chrome accept the cert
uvx rodney newpage 'https://localhost:9000'
uvx rodney page 1                    # switch to RFID tab
# Verify Status: OK appears
uvx rodney text 'body'
# Then switch back to Koha
uvx rodney page 0
```

Sometimes the TLS acceptance doesn't propagate to other tabs. If fetch
still fails, clear the browser cache and try again:
```bash
uvx rodney clear-cache
uvx rodney reload --hard
```

### Console error capture

Rodney has no built-in console log capture. To catch JS errors:

```bash
# Install a global error handler (single expression):
uvx rodney js 'window.__jsErrors=[];window.onerror=function(m,u,l,c,e){window.__jsErrors.push({msg:m,url:u,line:l});return true}'

# But this IIFE style also works:
uvx rodney js '(function(){window.__jsErrors=[];window.onerror=function(m,u,l,c,e){window.__jsErrors.push({msg:m,url:u,line:l});return true}})()'
```

To retrieve collected errors:
```bash
uvx rodney js 'JSON.stringify(window.__jsErrors||[])'
```

### RFID-specific debugging

- **Scan timeout**: The RFID server scan takes ~10s but the ping timeout in
  `rfid_poll()` was 5000ms — too short. The 15000ms scan timeout is correct.
  The 5000ms ping guard timeout fires before scan completes. Increase it to
  match the scan timeout or remove it.

- **`rfid_poll_pending` missing**: When editing the JS file, accidentally
  removed `var rfid_poll_pending = false;`. The error "rfid_poll_pending is
  not defined" appears at runtime. Always check `grep -n '\[upto\]'` after
  anchored edits — the tool leaves the literal `[upto]` in the file if the
  tail anchor doesn't match.

- **localStorage state confusion**: Old `rfid_pending`, `rfid_last_barcode`,
  `koha_state` entries persist in localStorage even after code removes them.
  Clear with:
  ```bash
  uvx rodney js 'localStorage.removeItem("rfid_pending");localStorage.removeItem("rfid_last_barcode");localStorage.removeItem("koha_state")'
  ```
  But this syntax won't work — use IIFE:
  ```bash
  uvx rodney js '(function(){localStorage.removeItem("rfid_pending");localStorage.removeItem("rfid_last_barcode");localStorage.removeItem("koha_state");return "cleared"})()'
  ```

- **Audit log events**: The scan loop fires every second, filling the audit
  log with duplicate scan events. The fix skips consecutive duplicate scan
  events for the same barcode (checks in-memory last event).

- **DOM not updating**: `body.append(html)` didn't insert the events log into
  the DOM. Switching to `insertAdjacentHTML('afterend', html)` worked. If
  jQuery DOM manipulation fails, fall back to native DOM methods.

### Anchored edit gotchas

When using the `edit` tool with `[upto]`:
1. The head anchor must match exactly once in the file.
2. The tail anchor must be unique — don't use just `}` as tail.
3. If the tail anchor doesn't match, the tool leaves `[upto]` as literal
   text in the file. Verify with `grep -n '\[upto\]' <file>`.
4. After fixing, the line numbers shift — re-read the file before relying
   on old line numbers for subsequent edits.

## Example: full checkout workflow

```bash
# Terminal 1: start RFID server
RFID_PORT=/dev/ttyUSB0 go run . &

# Terminal 2: drive Chrome
uvx rodney open "https://ffzg.koha-dev.rot13.org:8443/cgi-bin/koha/mainpage.pl"
# ... log in manually ...

uvx rodney open "https://ffzg.koha-dev.rot13.org:8443/cgi-bin/koha/circ/circulation.pl"
uvx rodney input 'input[name=findborrower]' dobrica
uvx rodney wait '#patron_results'
uvx rodney click '#patron_results a'
uvx rodney waitload

# Now place a book with RFID tag on the reader.
# The RFID JS should auto-fill the barcode and submit.
uvx rodney sleep 5
uvx rodney text '#rfid'
uvx rodney js 'document.querySelector("input[name=barcode]").value'
```
