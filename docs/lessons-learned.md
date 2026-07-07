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
