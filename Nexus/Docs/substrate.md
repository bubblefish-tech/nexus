# BF-Sketch Substrate

BubbleFish Nexus v0.1.3 includes an experimental binary quantization sketch substrate for compact embedding representation and forward-secure deletion.

## Overview

The substrate operates as a parallel storage tier alongside SQLite. It provides:

- **Sketch-based compact representation**: ~160 bytes per memory (vs 4-12 KB for full-precision embeddings)
- **Per-memory encryption**: every embedding encrypted at rest with AES-256-GCM using a per-memory key derived via HKDF-SHA-256
- **Forward-secure deletion**: a hash ratchet ensures deleted memories cannot be reconstructed from the current state
- **Deletion oracle**: a cuckoo filter tracks live memories for defense-in-depth set membership
- **Audit composition**: every substrate operation is logged in the hash-chained audit log

## Configuration

The substrate is controlled by the `[substrate]` section in `daemon.toml`. It is **disabled by default** in v0.1.3.

```toml
[substrate]
enabled = false                     # Enable BF-Sketch substrate
sketch_bits = 1                     # Bits per coordinate (only 1 supported)
ratchet_rotation_period = "24h"     # Automatic ratchet advance period
prefilter_threshold = 200           # Min candidates for Stage 3.5 activation
prefilter_top_k = 100               # Candidates to keep after prefiltering
cuckoo_capacity = 1000000           # Expected live memory count
cuckoo_rebuild_threshold = 0.75     # Load factor trigger for cuckoo rebuild
encryption_enabled = true           # Per-memory AES-256-GCM encryption

[canonical]
enabled = false                     # Enable embedding canonicalization
canonical_dim = 1024                # Target dimension (power of 2, 64-8192)
whitening_warmup = 1000             # Samples before whitening engages
query_cache_ttl_seconds = 60        # Query canonicalization cache TTL
```

When `[substrate] enabled = true`, `[canonical]` is automatically enabled.

## CLI Commands

### Status

```
nexus substrate status [--json]
```

Shows whether substrate is enabled, ratchet state, sketch count, and cuckoo filter statistics.

### Manual Ratchet Rotation

```
nexus substrate rotate-ratchet
```

Manually advances the ratchet to a new state. The old state is shredded (zeroed in the database and on disk).

### Deletion Proof

```
nexus substrate prove-deletion <memory_id>
```

Produces a signed proof bundle demonstrating that a memory has been cryptographically shredded. The proof contains evidence from four sources: cuckoo filter, canonical store, ratchet state, and audit chain.

### Forward-Secure Delete

```
nexus memory delete --shred-seed <memory_id>
```

Deletes a memory and advances the ratchet, ensuring the sketch projection and encryption key used for that memory cannot be reconstructed.

## The Deletion Claim

After a seed is advanced past and the associated memory is deleted, sketch-based retrieval of that memory becomes mathematically impossible because the random projection used to compute its sketch cannot be reconstructed from the new seed. The content encryption key derived from the advanced-past ratchet state cannot be reconstructed either. The original embedding is independently subject to the standard delete operation.

## Architecture

The substrate composes six production-proven primitives:

1. **Canonicalization**: SRHT-based dimension normalization + L2 normalization + per-source whitening
2. **Binary quantization**: 1-bit sign sketch with correction factors (~160 bytes at 1024 dimensions)
3. **Per-memory encryption**: AES-256-GCM with HKDF-SHA-256 key derivation from ratchet state
4. **Cuckoo filter**: defense-in-depth set membership with O(1) deletion
5. **Forward-secure ratchet**: HMAC-SHA-256 hash chain where advancing destroys prior states
6. **Audit composition**: every operation logged in the Phase 4 hash-chained audit log
