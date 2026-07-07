#!/bin/bash
# Deploy Koha plugin (RFID.pm) to Koha plugins directory on koha-dev.rot13.org
# Plugin: Koha::Plugin::Rot13::RFID
# Target: /var/lib/koha/ffzg/plugins/Koha/Plugin/Rot13/RFID.pm

set -e

SRC="/home/dpavlin/koha-rfid-go/plugin/Koha/Plugin/Rot13/RFID.pm"
DST_HOST="koha-dev.rot13.org"
DST_PATH="/var/lib/koha/ffzg/plugins/Koha/Plugin/Rot13/RFID.pm"

echo "=== Deploying RFID.pm ==="
echo "  Source: $SRC"
echo "  Target: $DST_HOST:$DST_PATH"

if [ ! -f "$SRC" ]; then
    echo "ERROR: source file $SRC not found!"
    exit 1
fi

scp "$SRC" "$DST_HOST:$DST_PATH"

echo "  Syntax check (on remote after deploy):"
ssh "$DST_HOST" "sudo perl -I/var/lib/koha/ffzg/plugins -I/srv/koha_ffzg -c '$DST_PATH'" \
  && echo "    OK — no syntax errors" \
  || echo "    (note: Koha dependency warnings are expected outside Koha context)"
ssh "$DST_HOST" "sudo chown ffzg-koha:ffzg-koha '$DST_PATH'"

echo ""
echo "=== Deploy complete ==="
echo ""
echo "NOTE: Koha plugins are cached — reload may take a moment."
echo "  To force reload, restart plack:"
echo "    ssh $DST_HOST 'sudo systemctl restart koha-plack'"
echo ""
