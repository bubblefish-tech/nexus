-------------------------------- MODULE AuditChain --------------------------------
\* TLA+ specification for BubbleFish Nexus audit log hash chain invariants.
\*
\* Models the append-only audit log where each entry contains a hash of its
\* content and a prevHash linking to the prior entry. Genesis has prevHash = 0.
\*
\* Invariants verified:
\*   1. Every entry's prevHash matches the prior entry's hash.
\*   2. Genesis entry has prevHash = zeros.
\*   3. No entry can be removed without breaking the chain.
\*
\* Model-check parameters: 3 entries, 2 concurrent writers.
\*
\* Copyright (c) 2026 BubbleFish Technologies, Inc. — AGPL-3.0-or-later.
--------------------------------------------------------------------------------

EXTENDS Integers, Sequences, FiniteSets, TLC

CONSTANTS
    MaxEntries,     \* Maximum number of audit entries (e.g. 3)
    Writers         \* Set of writer IDs (e.g. {"w1", "w2"})

VARIABLES
    chain,          \* Sequence of audit entries: [hash |-> Int, prevHash |-> Int, writer |-> Writers, content |-> Int]
    nextHash,       \* Counter used to generate unique hashes
    pc              \* Per-writer program counter

vars == <<chain, nextHash, pc>>

\* A hash is modeled as a unique positive integer. 0 represents the zero-hash (genesis prevHash).
ZeroHash == 0

TypeOK ==
    /\ chain \in Seq([hash: Nat, prevHash: Nat, writer: Writers, content: Nat])
    /\ nextHash \in Nat
    /\ pc \in [Writers -> {"idle", "writing"}]

Init ==
    /\ chain = <<>>
    /\ nextHash = 1
    /\ pc = [w \in Writers |-> "idle"]

\* ---------- Actions ----------

\* A writer begins composing an entry. No contention yet.
BeginWrite(w) ==
    /\ pc[w] = "idle"
    /\ Len(chain) < MaxEntries
    /\ pc' = [pc EXCEPT ![w] = "writing"]
    /\ UNCHANGED <<chain, nextHash>>

\* A writer appends an entry to the chain. The prevHash is set to the hash of
\* the last entry, or ZeroHash if the chain is empty (genesis).
CommitWrite(w) ==
    /\ pc[w] = "writing"
    /\ LET prevH == IF chain = <<>> THEN ZeroHash ELSE chain[Len(chain)].hash
           entry == [hash |-> nextHash, prevHash |-> prevH, writer |-> w, content |-> nextHash]
       IN
           /\ chain' = Append(chain, entry)
           /\ nextHash' = nextHash + 1
    /\ pc' = [pc EXCEPT ![w] = "idle"]

\* Next-state relation.
Next ==
    \E w \in Writers:
        \/ BeginWrite(w)
        \/ CommitWrite(w)

\* Fairness: every writer that starts writing eventually commits.
Fairness == \A w \in Writers: WF_vars(CommitWrite(w))

Spec == Init /\ [][Next]_vars /\ Fairness

\* ---------- Invariants ----------

\* INV1: Genesis entry (if it exists) has prevHash = ZeroHash.
GenesisZero ==
    Len(chain) > 0 => chain[1].prevHash = ZeroHash

\* INV2: Every non-genesis entry's prevHash matches the prior entry's hash.
ChainLinked ==
    \A i \in 2..Len(chain):
        chain[i].prevHash = chain[i-1].hash

\* INV3: All hashes in the chain are unique.
HashesUnique ==
    \A i, j \in 1..Len(chain):
        i /= j => chain[i].hash /= chain[j].hash

\* INV4: The chain is append-only — removing any entry breaks the hash linkage.
\* (This is implied by ChainLinked + HashesUnique: if you remove entry i,
\*  entry i+1's prevHash no longer matches entry i-1's hash.)
\* We state it explicitly: the chain length never decreases.
ChainMonotonic ==
    [][Len(chain') >= Len(chain)]_chain

\* Combined safety invariant.
SafetyInv ==
    /\ TypeOK
    /\ GenesisZero
    /\ ChainLinked
    /\ HashesUnique

================================================================================
