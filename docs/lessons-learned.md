# Lessons Learned — RFID Returns Fix Session

## Session Goal
Fix Koha RFID check-in on returns page — single book placed on reader was not submitted to Koha.

## Root Cause
On returns page, `rfid_koha_target(t.content)` returned `'DA'` because a prior `submit-checkin` existed in localStorage. The old code had:
```js
if ( ks != sec ) { ... submit ... }
```
If the tag was already `DA`, `ks == sec` was true → form never submitted.

**Fix**: Returns page now always submits regardless of tag AFI. Time-based dedup (10s) prevents rapid re-submit loops.

---

## Plugin Structure Discovered

The plugin `Koha::Plugin::Rot13::RFID` is deployed at:
- **Perl module**: `/var/lib/koha/ffzg/plugins/Koha/Plugin/Rot13/RFID.pm`
- **JS file**: `/var/lib/koha/ffzg/plugins/Koha/Plugin/Rot13/RFID/koha-rfid.js`

The plugin reads the JS from `C4::Context->config('pluginsdir') . '/Koha/Plugin/Rot13/RFID/koha-rfid.js'` and inlines it into pages after `</body>`.

**Pages injected**: `circulation.pl`, `returns.pl`, `renew.pl`, `mainpage.pl`

**Deploy path** (not the template dir!):
```
/var/lib/koha/ffzg/plugins/Koha/Plugin/Rot13/RFID/koha-rfid.js
```

**Restart required after deploy**:
```bash
sudo systemctl restart koha-plack
```

**Git repos**:
- Local workspace: `/home/dpavlin/koha-rfid-go/` — `koha-rfid.js`, `deploy.sh`
- Koha source tree: `/srv/koha_ffzg/ffzg/rfid/koha-rfid.js` — older version in git

---

## Debugging Workflow with rodney

### 1. Cache-busting is essential
Chrome caches the page HTML (including the inlined script). A hard reload doesn't always clear it. Always use:
```bash
uvx rodney newpage "https://ffzg.koha-dev.rot13.org:8443/cgi-bin/koha/circ/returns.pl?cb=$(date +%s)"
```

### 2. Verify the script is the right version
Check the script length — the old version was 20911 bytes, the new version is 22167 bytes:
```bash
uvx rodney js '(function(){var s=document.querySelectorAll("script");for(var i=0;i<s.length;i++){var t=s[i].text;if(t&&t.indexOf("RFID_VERSION")>=0)return "len:"+t.length+" rfidDebug:"+(t.indexOf("rfidDebug")>=0)}return "no script"})()'
```

### 3. Use window.rfidDebug for diagnostics
The debug namespace exposes:
- `events` — in-memory event cache
- `storageEvents()` — full localStorage audit log
- `localStorage()` — snapshot of all localStorage
- `serverOk` — RFID server connectivity (snapshot, not live)
- `noReader` — user opted out flag (snapshot, not live)
- `pendingTarget(barcode)` — pending AFI write target
- `kohaTarget(barcode)` — what Koha should have set
- `alreadySubmitted(barcode)` — event-based dedup check

### 4. Check the audit log for submissions
```bash
uvx rodney js '(function(){var e=window.rfidDebug.storageEvents();return JSON.stringify(e.slice(-5))})()'
```
Look for `submit-checkin` events — that means Koha received the book.

---

## JavaScript Gotchas

### Variable hoisting bites
`var` declarations are hoisted, but **initialization** is not. This code fails:
```js
window.rfidDebug.submittedThisPage = rfid_submitted_this_page; // undefined!
var rfid_submitted_this_page = {};
```
Fix: move the `var` declaration before the reference.

### Anchored edit `[upto]` literal
When using anchored edit with `[upto]`, if the tail anchor doesn't match, the tool leaves `[upto]` as literal text in the file. Always verify:
```bash
grep -n '\[upto\]' <file>
```

### Primitive snapshots vs references
Assigning a primitive value creates a snapshot, not a live reference:
```js
window.rfidDebug.serverOk = rfid_server_ok; // captures current false value
// Later rfid_server_ok becomes true, but debug still shows false
```
Fix: use getter functions for live values.

---

## What Not to Do

1. **Don't copy to `koha-tmpl/intranet-tmpl/js/`** — that directory is not where the plugin reads from. The plugin has its own directory under `/var/lib/koha/ffzg/plugins/`.

2. **Don't forget to restart Plack** after deploying the JS file. The plugin inlines the file on every request, but Plack may cache the Perl module.

3. **Don't assume git HEAD matches the live plugin** — the plugin directory may diverge from the git source tree. Always check both.

4. **Don't test without cache-busting** — Chrome caches the inlined script. Use `?cb=$(date +%s)` or open a fresh tab after deploy.

---

---

## Versioning Strategy

### When to bump major version (RFID_VERSION)
- **Architecture change**: replacing event log with AFI map (v1.0 → v2.0)
- **Data format change**: localStorage keys/values change incompatibly
- **Dedup logic change**: how we decide to submit or skip

### When to bump minor version (RFID_VERSION)
- **Bug fix**: wrong dedup condition, hoisting issue
- **Parameter change**: timeout values, retention period
- **Debug feature**: adding rfidDebug properties

### Current version
`RFID_VERSION = '2.0'` — major bump because:
- Replaced persistent event log (`rfid_events`) with per-barcode AFI map (`rfid_afi`)
- Old `rfid_events` localStorage key is deleted on startup
- Dedup now compares tag AFI against stored AFI, not event history
- Pending writes tracked via `pending` field in AFI map entry

---

---

## Koha Include Paths for Perl Syntax Checks

### Problem
`deploy-plugin.sh` ran `perl -c` with `-I/srv/koha_ffzg/lib`, which failed because that directory does not exist:
```
Base class package "Koha::Plugins::Base" is empty.
```

### Root Cause
The Koha source tree is at `/srv/koha_ffzg` (not `/srv/koha_ffzg/lib`). The Perl modules are at:
- `/srv/koha_ffzg/Koha/Plugins/Base.pm` — plugin base class
- `/srv/koha_ffzg/Koha/` — all Koha Perl modules
- `/srv/koha_ffzg/C4/` — Koha circulation modules

### Fix
Use `-I/srv/koha_ffzg` (the parent of `Koha/` and `C4/`), not `-I/srv/koha_ffzg/lib`:
```bash
ssh koha-dev.rot13.org "sudo perl -I/var/lib/koha/ffzg/plugins -I/srv/koha_ffzg -c 'RFID.pm'"
```

### Verification
```
/var/lib/koha/ffzg/plugins/Koha/Plugin/Rot13/RFID.pm syntax OK
```

---

## circulation-home.pl Support

### Background
`circulation-home.pl` is the modern Koha circulation page with tabbed UI. It has:
- **Checkin tab** (`#checkin_search`) — returns form with `#ret_barcode`
- **Renew tab** (`#renew_search`) — renew form with `#ren_barcode`
- **Patron search tab** (`#circ_search`) — checkout after selecting a patron, uses `#barcode`

### Tab Detection
Tabs are jQuery UI tabs using `aria-hidden` to toggle visibility:
```js
var checkin_active   = $('#checkin_search').attr('aria-hidden') == 'false';
var renew_active     = $('#renew_search').attr('aria-hidden') == 'false';
var checkout_active  = $('#circ_search').attr('aria-hidden') == 'false';
```

### Visibility Checks
Tabbed panels use `aria-hidden` for hiding/showing, not CSS `display: none`. jQuery's `:visible` selector **does** correctly reflect `aria-hidden` state — when a tab is active, elements inside it pass `:visible`; when hidden, they don't. So `i.is(':visible')` works reliably for tabbed panels.

### Handling Logic
On `circulation-home.pl` (detected by checking if `#header_search` length > 0):
1. **Checkin tab active** → fill `#ret_barcode` (returns dedup logic, always submit)
2. **Renew tab active** → fill `#ren_barcode` (renew logic, no AFI write)
3. **Checkout tab active** → wait for patron search (`input[name=findborrower]` visible), then fill `#barcode`
4. **Default tab** (patron search) → show book label, no form filling

### SYNC Convention
The plugin and JS file share a `// SYNC` comment marking the page list:
- Plugin (`RFID.pm`): `@rfid_pages` list with `# SYNC`
- JS (`koha-rfid.js`): `var rfid_pages` list with `// SYNC`

Both lists must be kept in sync. Adding a new page requires updating both files.

---

## Storage Version Invalidation

### Problem
After a major version bump (`2.0 → 2.1`), old localStorage entries (AFI map) from a prior version could persist and cause stale dedup logic.

### Solution
Each version stores its version key in localStorage (`rfid_storage_version`). On page load:
1. Read stored version
2. If mismatch → clear all old keys (`rfid_afi`, `rfid_events`, etc.)
3. Write current version

### When to bump `RFID_VERSION`
- **Major**: architecture change, data format change, dedup logic change
- **Minor**: bug fix, parameter change, debug feature

### Current version
`RFID_VERSION = '2.1'` — minor bump because:
- Added `circulation-home.pl` detection (no data format change)
- Tab detection variables added (no storage change)
- Version invalidation ensures stale AFI maps from v2.0 are cleared

---

## rodney Command Reference

| Command | Purpose |
|---------|---------|
| `uvx rodney connect localhost:9333` | Connect to Chrome |
| `uvx rodney pages` | List all tabs |
| `uvx rodney page <index>` | Switch to tab |
| `uvx rodney newpage <url>` | Open URL in new tab |
| `uvx rodney js '<expression>'` | Evaluate JS (single expression only) |
| `uvx rodney text '#selector'` | Visible text of element |
| `uvx rodney exists '#selector'` | Check element exists (exit code 0/1) |
| `uvx rodney url` | Current URL |
| `uvx rodney title` | Page title |

**Rodney limitation**: only accepts **single expressions** — no statements, no `var`, no `if`, no `try/catch`. Use IIFE for multi-step logic:
```bash
uvx rodney js '(function(){... return result})()'
```

---

## Test Fixes — 2026-07-09

For exact code changes, see `git log --diff-filter=M -p -- server_test.go server_integration_test.go browser_integration_test.go` and `git log -p -- server.go`.

### 1. Multiple `TestMain` in same package
Go only allows one `TestMain` per package. The browser test's `TestMain` was kept as it logs RFID_PORT, KOHA_USER, CDP_PORT. See git log for the exact removal.

### 2. Mock `RfidOps` must implement the full interface
`mockOps` embeds `rfidops.RfidOps` but was missing `Lock()`, `Unlock()`, `InventoryWithReset()`. The `rfidops.Scan()` function calls these, causing nil-pointer panic. See git log for the added methods.

### 3. chromedp API changes
`chromedp.NewRemoteBrowser()` was removed in v0.9.0+ and replaced by `NewRemoteAllocator()`. The URL format changed from `ws://HOST/devtools/browser/` to `http://HOST`. See git log for the exact replacement.

### 4. Dead `tagCache` and `BackgroundScan` removed
`tagCache` was populated on every `/scan/` but only read by `BackgroundScan()`, which was never called from `main.go`. Removed both along with `sync.Mutex`. See git log for the diff.

### 5. Shell E2E tests need specific env
`tests/e2e.sh` and per-page scripts require:
- `MOCK_URL` pointing to mock RFID server
- `KOHA_USER` / `KOHA_PASS` for Koha login
- Chrome with `--remote-debugging-port=9333`
- `source /home/dpavlin/koha-dev.env` (hardcoded path in `lib.sh`)
- rodney (`uvx rodney`) installed

They are **not** standalone unit tests – they are full workflow automation scripts.

---

## 2026-07-18 — What Went Wrong (Circulation Page Test Fixes)

### Mistake 1: Editing files on the server instead of local workspace
**Wrong:** Using `ssh koha-dev.rot13.org "echo ... >> koha-rfid.js"` to make changes directly on the server.
**Right:** Edit `koha-rfid.js` locally, then run `bash deploy.sh` to deploy. `deploy.sh` does syntax checking (`node --check`), linting (`eslint --quiet`), SCP to server, and Plack restart. Editing on server skips all validation and can break things.

### Mistake 2: Trying to call `reset_rfid_state()` from every scenario
**Wrong:** Calling `reset_rfid_state()` before each Phase 2 scenario (11, 12, 13, 15).
**Right:** `reset_rfid_state()` should be called only **once** at the beginning of the test file (inside `suite_start()`). The AFI map in localStorage is the single source of truth — resetting it mid-scenario breaks the state machine and the dedup logic. The AFI map naturally handles repeated scans.

### Mistake 3: Trying manual form submission instead of trusting automatic submission
**Wrong:** Using `rodney input()`, `rodney click()`, or JavaScript `document.getElementById("patronsearch").submit()` to manually submit forms.
**Right:** The RFID mock server + koha-rfid.js works via automatic polling:
1. `load_tag` puts a tag on the mock server
2. koha-rfid.js polls `/scan/` every ~2 seconds
3. It detects the new tag, automatically fills the correct form field (`#findborrower` for patron, `#barcode` for book)
4. It automatically submits the form
5. The test script should ONLY: `load_tag → sleep → waitload → verify result`
**Never** do manual form submission. The entire flow is automatic.

### Mistake 4: AFI dedup skipping checkout on circulation.pl
**Problem:** On circulation.pl checkout tab, when a book is DA and scanned as DA again, `rfid_scan()` skips submission because `entry.sec == sec`. This is correct for dedup BUT incorrect for checkout — a DA book on the checkout tab SHOULD be checked out (it was checked in, then the user wants to check it out again).
**Fix:** Add `&& !checkout_active` to the dedup skip condition on line 490 of koha-rfid.js:
```js
// Step 2: skip if AFI hasn't changed since last submission
// (checkout tab is an exception — DA book on checkout tab means re-checkout)
if ( entry && entry.sec == sec && !returns && !renew && !checkin_active && !renew_active && !checkout_active ) {
```

### Mistake 5: Missing `now` declaration in patron card section
**Problem:** Line 631 used `now = Date.now()` but `now` is only declared inside the `returns` block (line 425). While `now` is already declared at the function scope, using it in the patron card section (which runs before the returns block in some code paths) could cause issues if the variable wasn't properly hoisted.
**Fix:** `now` is already declared at line 425 (`var now, shouldSubmit, pending;`), so this is fine. But always check that variables used across different blocks are declared at the function scope.

### Mistake 6: Not restarting Plack after deploy
**Wrong:** Deploying `koha-rfid.js` but not restarting Plack.
**Right:** After `deploy.sh`, always verify the script is loaded. If the page shows no script or old behavior, restart: `sudo systemctl restart koha-plack`. Plack caches Perl modules, and the plugin reads the JS file via `read_file()` in the Perl module, so a restart may be needed.

### Mistake 7: Cache-busting after deploy
**Wrong:** Opening the page after deploy without cache-busting.
**Right:** After deploying a new `koha-rfid.js`, always use cache-busting to load the new version:
```bash
uvx rodney newpage "https://ffzg.koha-dev.rot13.org:8443/cgi-bin/koha/circ/circulation.pl?cb=$(date +%s)"
```
Chrome caches the page HTML including the inlined script. Even a hard reload doesn't always clear it.

### Mistake 8: `nav_page()` calling `tab_switch()` inside it
**Problem:** `nav_page()` navigated to the page AND activated the tab. But scenarios that call `nav_page` then call `tab_switch` again, causing the tab to be clicked twice. The second click can trigger unintended behavior.
**Fix:** `nav_page()` should only navigate to the page. `tab_switch()` should be called separately by the scenario flow. This gives scenarios full control over when tabs are activated.

### Mistake 9: Patron card resubmission not allowed
**Problem:** When a patron card is scanned, koha-rfid.js sets `sec: 'patron'` in the AFI map. On subsequent scans, the patron entry already exists so the form is never submitted. This breaks Phase 2 scenarios where the same patron needs to be scanned again.
**Fix:** Add time-based dedup for patron cards (same as returns page):
```js
// Non-book barcode (patron card) — allow resubmission after dedup window
var patronEntry = rfid_afi_get(t.content);
now = Date.now();
var canResubmit = !patronEntry || (patronEntry.sec == 'patron' && now - (patronEntry.submit || 0) > RFID_DEDUP_MS) || patronEntry.sec != 'patron';
if ( canResubmit ) {
    // submit form
}
```

### Key Rule
**Always edit local files, then deploy.** Never edit server files directly. The deploy pipeline exists for a reason: syntax validation, linting, SCP, and Plack restart.

