# BubbleFish Nexus Threat Model

This document describes the threat model for BubbleFish Nexus v0.1.0.
It defines which attack classes Nexus defends against and which are
explicitly out of scope.

**Reference:** Tech Spec Section 6.8.

## In Scope

### Local Network Attackers

**Threat:** An attacker on the same local network attempts to access or
tamper with the Nexus API.

**Mitigations:**
- Default bind address is `127.0.0.1` (localhost only). Traffic never
  leaves the machine unless explicitly configured otherwise.
- Per-source API keys with constant-time comparison
  (`subtle.ConstantTimeCompare`) prevent timing-based token extraction.
- Optional TLS/mTLS when network exposure is required.
- Rate limiting bounds brute-force attempts.

### Network Eavesdroppers

**Threat:** An attacker passively captures traffic between an AI client
and Nexus to read memory content or extract API keys.

**Mitigations:**
- Default localhost binding means traffic never traverses a network.
- When configured for non-localhost access, TLS/mTLS encrypts all
  traffic in transit.
- API keys are never included in response bodies or log output.

### Disk Theft

**Threat:** An attacker gains physical access to the storage device
containing Nexus data files (WAL, database, config).

**Mitigations:**
- Optional WAL encryption (AES-256-GCM) protects data at rest.
- File permissions enforced: 0600 for all data files, 0700 for
  directories.
- Key files stored with 0600 permissions in a 0700 secrets directory.
- OS-level full-disk encryption recommended as defense-in-depth.

### WAL and Config Tampering

**Threat:** An attacker with filesystem access modifies WAL entries or
configuration files to inject malicious data or alter daemon behavior.

**Mitigations:**
- CRC32 checksums detect accidental corruption. Tampered entries are
  skipped with a WARN log.
- HMAC integrity mode (`mac`) provides cryptographic tamper detection.
  Entries failing HMAC verification are skipped and a
  `wal_tamper_detected` security event is emitted.
- Config signing: when enabled, Nexus verifies compiled config
  signatures at startup and on hot reload. Modified configs are rejected
  with a `config_signature_invalid` security event.

### Accidental Secret Exposure

**Threat:** API keys or tokens are accidentally written to log files,
error messages, or debug output.

**Mitigations:**
- Secret values are NEVER logged at any level, including DEBUG.
- Config supports `env:` and `file:` references so that literal keys
  need not appear in config files.
- Config lint warns on literal keys in non-simple mode.
- Admin and source tokens are resolved once at startup; the raw
  references are kept for display, resolved values for comparison only.

## Out of Scope

### Compromised Host

If an attacker has root or administrator access to the host machine,
Nexus cannot protect data. The attacker can read process memory, key
files, and database contents directly. Defense against a compromised
host requires OS-level and hardware-level security controls outside the
scope of an application-layer daemon.

### Hostile Hypervisor

Side-channel attacks from a compromised hypervisor (e.g., speculative
execution, memory bus snooping) are not mitigated. Nexus runs in
user-space and has no visibility into or control over the hypervisor
layer.

### Supply Chain Attacks

Nexus pins dependencies and audits licenses but does not defend against
compromised Go modules or build toolchain attacks. Users should verify
release checksums and use reproducible builds where possible.

### DDoS

Rate limiting provides basic protection against request floods. Volumetric
DDoS attacks (bandwidth exhaustion, SYN floods) require upstream network
mitigation (firewalls, CDN, cloud provider DDoS protection) and are
outside the scope of an application-layer daemon.
