# TLA+ Specifications for BubbleFish Nexus

Formal specifications of three critical invariants using TLA+ and the TLC
model checker.

## Specifications

| File                | What it models                          | Parameters                        |
|---------------------|-----------------------------------------|-----------------------------------|
| `AuditChain.tla`    | Audit log hash chain integrity          | 3 entries, 2 concurrent writers   |
| `Ratchet.tla`       | Forward-secure ratchet key rotation     | 4 epochs, 2 participants          |
| `WALConsistency.tla`| WAL write + replay crash consistency    | 3 entries, 1 crash point          |

## Installing TLC

### Option A: TLA+ Toolbox (GUI)

Download the TLA+ Toolbox from https://lamport.azurewebsites.net/tla/toolbox.html.
It bundles TLC, TLAPS, and the PlusCal translator.

### Option B: Command-line TLC

1. Install Java 11+ (JDK or JRE).
2. Download `tla2tools.jar` from
   https://github.com/tlaplus/tlaplus/releases (look for the latest release).
3. Place `tla2tools.jar` somewhere on your system (e.g. `~/tla/tla2tools.jar`).

## Running Model Checks

### AuditChain

```bash
java -cp tla2tools.jar tlc2.TLC AuditChain.tla \
  -config AuditChain.cfg
```

Create `AuditChain.cfg`:

```
CONSTANTS
    MaxEntries = 3
    Writers = {"w1", "w2"}

INIT Init
NEXT Next

INVARIANTS
    SafetyInv

PROPERTIES
    ChainMonotonic
```

### Ratchet

```bash
java -cp tla2tools.jar tlc2.TLC Ratchet.tla \
  -config Ratchet.cfg
```

Create `Ratchet.cfg`:

```
CONSTANTS
    MaxEpoch = 4
    Participants = {"p1", "p2"}

INIT Init
NEXT Next

INVARIANTS
    SafetyInv

PROPERTIES
    EpochMonotonic
    ShredMonotonic
```

### WALConsistency

```bash
java -cp tla2tools.jar tlc2.TLC WALConsistency.tla \
  -config WALConsistency.cfg
```

Create `WALConsistency.cfg`:

```
CONSTANTS
    MaxEntries = 3
    MaxCrashes = 1

INIT Init
NEXT Next

INVARIANTS
    SafetyInv

PROPERTIES
    WALMonotonic
    LivenessProperty
```

## TLA+ Toolbox Instructions

1. Open TLA+ Toolbox.
2. File > Open Spec > Add New Spec... > select a `.tla` file.
3. TLC Model Checker > New Model...
4. Under "What is the behavior spec?" set Init and Next.
5. Under "What to check?" > Invariants, add `SafetyInv`.
6. Set constants as listed above.
7. Click "Run TLC" (green play button).

## Expected Results

All three specifications should produce **no errors** when model-checked
with the parameters above. The state spaces are intentionally small to keep
model checking under a few seconds on commodity hardware:

- AuditChain: ~50 states (3 entries x 2 writers)
- Ratchet: ~200 states (4 epochs x 2 participants)
- WALConsistency: ~100 states (3 entries x 1 crash point)

## What These Specs Prove

### AuditChain

- Hash chain integrity: every entry's `prevHash` points to the prior entry's `hash`.
- Genesis has `prevHash = 0`.
- Removing any entry breaks the chain (via uniqueness + linkage).
- Concurrent writers produce a valid chain (linearizable append).

### Ratchet

- Forward security: once a key epoch is rotated, its material is irrecoverably
  destroyed. No action can restore it.
- Epoch monotonicity: participant epochs only increase.
- No backward keys: participants never hold keys for epochs before their current one.

### WALConsistency

- Every entry written to the WAL is replayed exactly once.
- Replay order matches write order.
- A crash during writing discards only the in-flight entry; all completed
  entries survive.
- No duplicate replays.
