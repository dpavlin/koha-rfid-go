#!/bin/bash
# Load current RFID reader state into mock server on port 9001
# Run this after starting ./cmd/mock-rfid/mock-rfid -port 9001
#
# Usage: bash docs/setup-mock.sh

MOCK=http://localhost:9001

echo "=== Loading real RFID tag data into mock server ==="

# Clear first
curl -s -X POST $MOCK/mock/clear

# Add patron card
curl -s -X POST -d '{"sid":"e00401001f77fb98","content":"200000000042","security":"DA"}' $MOCK/mock/tag

# Add books
curl -s -X POST -d '{"sid":"e004010031269117","content":"1302099999","security":"D7"}' $MOCK/mock/tag
curl -s -X POST -d '{"sid":"e00401001f7812ed","content":"1301111111","security":"DA"}' $MOCK/mock/tag
curl -s -X POST -d '{"sid":"e00401003126a0c8","content":"1302079605","security":"DA"}' $MOCK/mock/tag

echo ""
echo "=== Mock loaded ==="
curl -s $MOCK/mock/status | python3 -m json.tool
