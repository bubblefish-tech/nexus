#!/bin/bash
# Template for an agent that connects to Nexus via curl
# Replace AGENT_TOKEN with the token from nexus agent create

NEXUS_URL="http://localhost:7474/mcp"
AGENT_TOKEN="your-token-here"

# Search for relevant context
CONTEXT=$(curl -s -X POST "$NEXUS_URL" \
  -H "Authorization: Bearer $AGENT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"nexus_search","arguments":{"query":"recent competitor updates","limit":5}},"id":1}')

# ... agent logic here using $CONTEXT ...

# Write results back to shared memory
curl -s -X POST "$NEXUS_URL" \
  -H "Authorization: Bearer $AGENT_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"jsonrpc\":\"2.0\",\"method\":\"tools/call\",\"params\":{\"name\":\"nexus_write\",\"arguments\":{\"content\":\"$RESULT\",\"metadata\":{\"type\":\"competitor-update\"}}},\"id\":2}"
