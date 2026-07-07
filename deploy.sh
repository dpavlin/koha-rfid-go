#!/bin/bash
# Deploy koha-rfid.js to Koha plugin directory on koha-dev.rot13.org
# Plugin: Koha::Plugin::Rot13::RFID
# Target: /var/lib/koha/ffzg/plugins/Koha/Plugin/Rot13/RFID/koha-rfid.js

set -e

SRC="/home/dpavlin/koha-rfid-go/koha-rfid.js"
DST_HOST="koha-dev.rot13.org"
DST_PATH="/var/lib/koha/ffzg/plugins/Koha/Plugin/Rot13/RFID/koha-rfid.js"

echo "=== Deploying koha-rfid.js ==="
echo "  Source: $SRC"
echo "  Target: $DST_HOST:$DST_PATH"

if [ ! -f "$SRC" ]; then
    echo "ERROR: source file $SRC not found!"
    exit 1
fi

echo "  Syntax check:"
node --check "$SRC" && echo "    OK — no syntax errors" || { echo "    FAIL — syntax errors found, aborting"; exit 1; }

scp "$SRC" "$DST_HOST:$DST_PATH"
ssh "$DST_HOST" "sudo chown ffzg-koha:ffzg-koha '$DST_PATH'"
ssh "$DST_HOST" "sudo systemctl restart koha-plack"

echo ""
echo "=== Deploy complete ==="
echo ""
echo "=== NEXT STEPS (run with rodney) ==="
echo ""
echo "1. Open returns page with cache-busting param:"
echo '   uvx rodney newpage "https://ffzg.koha-dev.rot13.org:8443/cgi-bin/koha/circ/returns.pl?cb=$(date +%s)"'
echo ""
echo "2. Log in if needed, then place a book on the RFID reader."
echo "   Watch the RFID popup in the browser."
echo ""
echo "3. Verify the fix using the audit log (docs/RODNEY.md § RFID-specific debugging):"
echo '   uvx rodney js "window.rfidDebug.storageEvents().slice(-3)"'
echo "   Look for submit-checkin event — that means Koha received the book."
echo ""
echo "4. The old problem: returns page didn't submit when tag was already DA."
echo "   Fix: always submit on returns, time-based dedup prevents rapid re-submits."
echo ""
echo "5. After test passes, commit locally:"
echo "   cd /home/dpavlin/koha-rfid-go && git add koha-rfid.js deploy.sh && git commit -m 'fix returns always-submit, add rfidDebug'"
echo ""
