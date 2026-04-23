# Cryptographic Provenance Demo

**60 seconds to full cross-vendor cryptographic provenance.**

This demo shows BubbleFish Nexus's Phase 4 provenance layer in action:

1. **Agent A** writes a memory with Ed25519 signing enabled
2. **Agent B** reads the memory (different vendor, different key)
3. Export a **proof bundle** containing the memory, signature, and audit chain
4. **Verify with Go** — `nexus verify proof.json`
5. **Verify with Python** — independent implementation, same result
6. **Tamper** with the proof — verification correctly fails

## Prerequisites

- BubbleFish Nexus daemon running
- Agent A and Agent B source configs (see `agent-a.toml`, `agent-b.toml`)
- Python 3 with `cryptography` package (`pip install cryptography`)

## Run

```bash
# Linux/macOS
./demo.sh

# Windows PowerShell
.\demo.ps1
```

## What This Proves

- Every write is Ed25519 signed by the source
- The audit log is hash-chained from genesis (tamper-evident)
- Proof bundles are self-contained and independently verifiable
- Two independent implementations (Go + Python) agree on validity
- A single bit of tampering is detected immediately

## Files

| File | Purpose |
|------|---------|
| `agent-a.toml` | Source config with `[source.signing] mode = "local"` |
| `agent-b.toml` | Read-only source for cross-vendor verification |
| `demo.sh` | Automated demo script (bash) |
| `demo.ps1` | Automated demo script (PowerShell) |

---

*Copyright (c) 2026 BubbleFish Technologies, Inc. Licensed under AGPL-3.0.*
