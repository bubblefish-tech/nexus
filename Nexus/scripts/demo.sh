#!/usr/bin/env bash
# BubbleFish Nexus — 60-Second Show-Off Demo (Bash)
#
# Demonstrates core Nexus capabilities in under 60 seconds:
#   1. Daemon health check
#   2. Write 3 memories
#   3. Search memories
#   4. Fetch + verify cryptographic proof (browser-verifiable HTML)
#   5. Print memory graph URL
#   6. Print summary
#
# Prerequisites:
#   - bubblefish binary built and on PATH
#   - Nexus daemon running  (run `bubblefish start` first)
#   - NEXUS_API_KEY   set to a data-plane API key
#   - NEXUS_ADMIN_KEY set to the admin token
#
# Run from the repo root:
#   bash scripts/demo.sh

set -euo pipefail

BASE="${NEXUS_URL:-http://localhost:8080}"
DASH_BASE="${NEXUS_DASH_URL:-http://localhost:8081}"
API_KEY="${NEXUS_API_KEY:-}"
ADMIN_KEY="${NEXUS_ADMIN_KEY:-}"
START_TS=$(date +%s)
PASSES=0
FAILURES=0

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[0;33m'
NC='\033[0m'

step()  { echo; echo -e "${CYAN}=== $* ===${NC}"; }
pass()  { echo -e "  ${GREEN}PASS${NC}  $*"; PASSES=$((PASSES+1)); }
fail()  { echo -e "  ${RED}FAIL${NC}  $*"; FAILURES=$((FAILURES+1)); }
warn()  { echo -e "  ${YELLOW}WARN${NC}  $*"; }
elapsed() { echo "$(( $(date +%s) - START_TS ))s"; }

auth_hdr() { echo "Authorization: Bearer ${ADMIN_KEY}"; }
data_hdr() { echo "Authorization: Bearer ${API_KEY}"; }

echo
echo "BubbleFish Nexus — Show-Off Demo"
echo "==================================="

# Step 1: Health check
step "1. Daemon health"
if status=$(curl -sf --max-time 5 "${BASE}/health" 2>/dev/null); then
    s=$(echo "${status}" | grep -o '"status":"[^"]*"' | cut -d'"' -f4 || echo "?")
    pass "Daemon is up (${s}) — $(elapsed)"
else
    fail "Daemon unreachable at ${BASE} — is \`bubblefish start\` running?"
    echo "  Hint: run 'bubblefish start' in another terminal, then retry."
    exit 1
fi

# Step 2: Write 3 memories
step "2. Write memories"
SOURCE="demo-nexus"
WRITTEN_ID=""
declare -a SUBJECTS=("nexus-demo-1" "nexus-demo-2" "nexus-demo-3")
declare -a CONTENTS=(
    "BubbleFish Nexus stores AI memories with cryptographic provenance."
    "Every write is WAL-durability-first: the journal survives crashes before the DB."
    "The substrate BF-Sketch provides forward-secure deletion proofs."
)

for i in 0 1 2; do
    SUBJ="${SUBJECTS[$i]}"
    CONT="${CONTENTS[$i]}"
    BODY=$(printf '{"subject":"%s","content":"%s","source":"%s","destination":"sqlite","idempotency_key":"demo-%s-v1"}' \
        "${SUBJ}" "${CONT}" "${SOURCE}" "${SUBJ}")
    if resp=$(curl -sf --max-time 10 -X POST "${BASE}/inbound/${SOURCE}" \
        -H "$(data_hdr)" -H "Content-Type: application/json" \
        -d "${BODY}" 2>/dev/null); then
        pid=$(echo "${resp}" | grep -o '"payload_id":"[^"]*"' | cut -d'"' -f4 || echo "")
        [ -z "${WRITTEN_ID}" ] && WRITTEN_ID="${pid}"
        pass "Written: ${SUBJ} → ${pid}"
    else
        fail "Write failed for ${SUBJ}"
    fi
done

# Step 3: Search
step "3. Search memories"
if q=$(curl -sf --max-time 10 "${BASE}/query/sqlite?q=WAL-durability&limit=5" \
    -H "$(data_hdr)" 2>/dev/null); then
    cnt=$(echo "${q}" | grep -o '"payload_id"' | wc -l | tr -d ' ')
    if [ "${cnt}" -gt 0 ]; then
        pass "Search returned ${cnt} result(s) — $(elapsed)"
    else
        warn "Search returned 0 results (index may still be warm)"
    fi
else
    fail "Search request failed"
fi

# Step 4: Cryptographic proof
step "4. Cryptographic proof"
PROOF_HTML="${TMPDIR:-/tmp}/nexus-proof-demo.html"
if [ -n "${WRITTEN_ID}" ] && [ -n "${ADMIN_KEY}" ]; then
    if proof=$(curl -sf --max-time 10 "${DASH_BASE}/verify/${WRITTEN_ID}" \
        -H "Authorization: Bearer ${ADMIN_KEY}" 2>/dev/null); then
        PROOF_JSON="${TMPDIR:-/tmp}/nexus-proof-demo.json"
        echo "${proof}" > "${PROOF_JSON}"
        pass "Proof bundle fetched for ${WRITTEN_ID} — $(elapsed)"
        # Generate HTML via CLI if available
        if command -v bubblefish &>/dev/null; then
            if bubblefish verify --proof "${WRITTEN_ID}" \
                --url "${DASH_BASE}" --token "${ADMIN_KEY}" \
                --output "${PROOF_HTML}" 2>/dev/null; then
                pass "Browser-verifiable HTML written to ${PROOF_HTML}"
                # Try to open in browser (macOS + Linux)
                if command -v open &>/dev/null; then open "${PROOF_HTML}"
                elif command -v xdg-open &>/dev/null; then xdg-open "${PROOF_HTML}"
                fi
            fi
        else
            warn "bubblefish CLI not on PATH — raw proof JSON at ${PROOF_JSON}"
        fi
    else
        warn "Proof fetch failed (check NEXUS_ADMIN_KEY)"
    fi
elif [ -z "${ADMIN_KEY}" ]; then
    warn "No NEXUS_ADMIN_KEY — skipping proof verification"
else
    warn "No memory ID from step 2 — skipping proof"
fi

# Step 5: Memory graph URL
step "5. Memory graph dashboard"
if [ -n "${ADMIN_KEY}" ]; then
    GRAPH_URL="${DASH_BASE}/dashboard/memgraph?token=${ADMIN_KEY}"
    if curl -sf --max-time 5 "${GRAPH_URL}" -o /dev/null 2>/dev/null; then
        pass "Memory graph page OK — ${GRAPH_URL}"
        if command -v open &>/dev/null; then open "${GRAPH_URL}"
        elif command -v xdg-open &>/dev/null; then xdg-open "${GRAPH_URL}"
        fi
    else
        warn "Memory graph unavailable (dashboard may not be running)"
    fi
else
    warn "No NEXUS_ADMIN_KEY — skipping dashboard open"
fi

# Summary
echo
echo -e "${CYAN}=== Summary ===${NC}"
echo "  Elapsed : $(elapsed)"
echo -e "  Passed  : ${GREEN}${PASSES}${NC}"
if [ "${FAILURES}" -gt 0 ]; then
    echo -e "  Failed  : ${RED}${FAILURES}${NC}"
    exit 1
else
    echo -e "  ${GREEN}All checks passed${NC}"
    exit 0
fi
