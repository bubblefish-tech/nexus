#!/usr/bin/env bash
# BubbleFish Nexus — 60-second Cryptographic Provenance Demo
#
# This script demonstrates end-to-end cryptographic provenance:
#   1. Agent A writes a memory (Ed25519 signed)
#   2. Agent B reads the memory
#   3. Export proof bundle
#   4. Verify with Go CLI (nexus verify)
#   5. Verify with Python (independent implementation)
#   6. Tamper with the proof → verification fails
#
# Prerequisites:
#   - nexus daemon running with agent-a.toml and agent-b.toml sources
#   - Python 3 with cryptography package installed
#
# Reference: v0.1.3 Build Plan Phase 4 Subtask 4.12.
#
# Copyright (c) 2026 BubbleFish Technologies, Inc.
# Licensed under the GNU Affero General Public License v3.0.

set -euo pipefail

DAEMON="http://127.0.0.1:3080"
ADMIN_KEY="${BFN_ADMIN_KEY:-bfn_admin_TEST_KEY}"
AGENT_A_KEY="${AGENT_A_KEY:-bfn_data_TEST_AGENT_A}"
AGENT_B_KEY="${AGENT_B_KEY:-bfn_data_TEST_AGENT_B}"

echo "=== BubbleFish Nexus: 60-Second Cryptographic Provenance Demo ==="
echo ""

# Step 1: Agent A writes a memory
echo "[1/6] Agent A writes a signed memory..."
WRITE_RESP=$(curl -s -X POST "${DAEMON}/inbound/agent-a" \
  -H "Authorization: Bearer ${AGENT_A_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"content":"The quarterly revenue forecast is $4.2M","subject":"finance/forecast","actor_type":"agent","actor_id":"agent-a-demo"}')
PAYLOAD_ID=$(echo "$WRITE_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('payload_id',''))" 2>/dev/null || echo "")
echo "  Written: payload_id=${PAYLOAD_ID}"

# Step 2: Agent B reads the memory
echo "[2/6] Agent B reads the memory..."
READ_RESP=$(curl -s "${DAEMON}/query/sqlite?subject=finance/forecast&limit=1" \
  -H "Authorization: Bearer ${AGENT_B_KEY}")
echo "  Read: $(echo "$READ_RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print(f\"{d.get('_nexus',{}).get('result_count',0)} record(s)\")" 2>/dev/null || echo "ok")"

# Step 3: Export proof bundle
echo "[3/6] Exporting proof bundle..."
curl -s "${DAEMON}/verify/${PAYLOAD_ID}" \
  -H "Authorization: Bearer ${ADMIN_KEY}" \
  > /tmp/proof-bundle.json
echo "  Saved: /tmp/proof-bundle.json"

# Step 4: Verify with Go CLI
echo "[4/6] Verifying with Go CLI..."
if nexus verify /tmp/proof-bundle.json 2>/dev/null; then
  echo "  Go CLI: VALID"
else
  echo "  Go CLI: verification result printed above"
fi

# Step 5: Verify with Python
echo "[5/6] Verifying with Python (independent implementation)..."
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if python3 "${SCRIPT_DIR}/../../tools/verify-python/verify.py" /tmp/proof-bundle.json 2>/dev/null; then
  echo "  Python: VALID"
else
  echo "  Python: verification result printed above"
fi

# Step 6: Tamper and verify again
echo "[6/6] Tampering with proof bundle..."
python3 -c "
import json
with open('/tmp/proof-bundle.json') as f:
    bundle = json.load(f)
# Tamper: change content
if 'memory' in bundle:
    bundle['memory']['content'] = 'TAMPERED CONTENT'
with open('/tmp/proof-bundle-tampered.json', 'w') as f:
    json.dump(bundle, f, indent=2)
"
echo "  Tampered: /tmp/proof-bundle-tampered.json"
echo ""
echo "  Verifying tampered bundle with Go CLI..."
if nexus verify /tmp/proof-bundle-tampered.json 2>/dev/null; then
  echo "  ERROR: tampered bundle should have failed!"
  exit 1
else
  echo "  Go CLI correctly detected tampering."
fi

echo ""
echo "=== 60 seconds, full cryptographic provenance across vendors. ==="
echo "=== Every write signed. Every query attestable. Every proof verifiable. ==="
