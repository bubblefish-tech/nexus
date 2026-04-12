#!/usr/bin/env python3
"""
BubbleFish Nexus — Independent Proof Bundle Verifier (Python)

This is a standalone reimplementation of `bubblefish verify` in Python.
It proves the proof bundle format is a spec, not a trick — any language
can verify Nexus provenance independently.

Usage:
    python3 verify.py <proof-bundle.json>

Exit codes:
    0 = valid
    1 = invalid (signature, chain, or daemon identity failure)
    2 = usage error

Reference: v0.1.3 Build Plan Phase 4 Subtask 4.7.

Copyright (c) 2026 BubbleFish Technologies, Inc.
Licensed under the GNU Affero General Public License v3.0.
"""

import hashlib
import json
import sys

try:
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
    from cryptography.exceptions import InvalidSignature
except ImportError:
    print("ERROR: 'cryptography' package required. Install: pip install cryptography>=41.0", file=sys.stderr)
    sys.exit(2)


def content_hash(content: str) -> str:
    """SHA-256 of content string, hex-encoded."""
    return hashlib.sha256(content.encode("utf-8")).hexdigest()


def sha256_hex(data: bytes) -> str:
    """SHA-256 of raw bytes, hex-encoded."""
    return hashlib.sha256(data).hexdigest()


def verify_content_hash(bundle: dict) -> tuple[bool, str]:
    """Check that memory content matches the declared content_hash."""
    memory = bundle.get("memory", {})
    c = memory.get("content", "")
    ch = memory.get("content_hash", "")
    if c and ch:
        computed = content_hash(c)
        if computed != ch:
            return False, f"content hash mismatch: computed {computed}, declared {ch}"
    return True, ""


def verify_signature(bundle: dict) -> tuple[bool | None, str]:
    """Verify Ed25519 signature over the signable envelope.

    Returns (None, "") if the bundle is unsigned.
    Returns (True, "") if the signature is valid.
    Returns (False, reason) if the signature is invalid.
    """
    sig_hex = bundle.get("signature", "")
    pubkey_hex = bundle.get("source_pubkey", "")

    if not sig_hex or not pubkey_hex:
        return None, ""  # unsigned — valid state

    try:
        sig_bytes = bytes.fromhex(sig_hex)
        pub_bytes = bytes.fromhex(pubkey_hex)
    except ValueError as e:
        return False, f"invalid hex encoding: {e}"

    if len(pub_bytes) != 32:
        return False, f"invalid public key size: {len(pub_bytes)} (expected 32)"

    # Reconstruct the signable envelope with sorted keys (Go's json.Marshal default).
    memory = bundle.get("memory", {})
    envelope = {
        "content_hash": memory.get("content_hash", ""),
        "idempotency_key": memory.get("idempotency_key", ""),
        "source_name": memory.get("source", ""),
        "timestamp": memory.get("timestamp", ""),
    }
    # json.dumps with sort_keys matches Go's json.Marshal canonical ordering.
    envelope_json = json.dumps(envelope, separators=(",", ":"), sort_keys=True).encode("utf-8")

    try:
        pub = Ed25519PublicKey.from_public_bytes(pub_bytes)
        pub.verify(sig_bytes, envelope_json)
        return True, ""
    except InvalidSignature:
        return False, "Ed25519 signature does not match the signable envelope"
    except Exception as e:
        return False, f"signature verification error: {e}"


def verify_daemon_identity(bundle: dict) -> tuple[bool, str]:
    """Check that daemon pubkey in genesis matches the bundle's daemon_pubkey."""
    daemon_key = bundle.get("daemon_pubkey", "")
    genesis_raw = bundle.get("genesis_entry")

    if not daemon_key or not genesis_raw:
        return True, ""  # no daemon info — skip

    if isinstance(genesis_raw, str):
        try:
            genesis = json.loads(genesis_raw)
        except json.JSONDecodeError:
            return False, "cannot parse genesis entry"
    else:
        genesis = genesis_raw

    genesis_daemon_key = genesis.get("daemon_pubkey", "")
    if genesis_daemon_key != daemon_key:
        return False, f"daemon pubkey mismatch: genesis has {genesis_daemon_key}, bundle has {daemon_key}"

    return True, ""


def verify_chain(bundle: dict) -> tuple[bool, str]:
    """Verify the audit hash chain from genesis to tip."""
    chain = bundle.get("audit_chain", [])
    if not chain:
        return True, ""

    # Verify genesis hash.
    genesis_payload = json.dumps(chain[0]["payload"], separators=(",", ":"), sort_keys=True).encode("utf-8") \
        if isinstance(chain[0]["payload"], dict) else chain[0]["payload"]
    if isinstance(genesis_payload, str):
        genesis_payload = genesis_payload.encode("utf-8")

    genesis_hash = sha256_hex(genesis_payload)
    declared_hash = chain[0].get("hash", "")
    if genesis_hash != declared_hash:
        return False, f"genesis hash mismatch: computed {genesis_hash}, declared {declared_hash}"

    for i in range(1, len(chain)):
        entry = chain[i]
        prev_entry = chain[i - 1]

        # Check chain link.
        if entry.get("prev_hash", "") != prev_entry.get("hash", ""):
            return False, (
                f"chain break at entry {i}: prev_hash={entry.get('prev_hash', '')} "
                f"expected={prev_entry.get('hash', '')}"
            )

        # Check payload hash.
        payload = entry.get("payload")
        if isinstance(payload, dict):
            payload_bytes = json.dumps(payload, separators=(",", ":"), sort_keys=True).encode("utf-8")
        elif isinstance(payload, str):
            payload_bytes = payload.encode("utf-8")
        else:
            payload_bytes = json.dumps(payload, separators=(",", ":"), sort_keys=True).encode("utf-8")

        computed = sha256_hex(payload_bytes)
        if computed != entry.get("hash", ""):
            return False, f"hash mismatch at entry {i}: computed {computed}, declared {entry.get('hash', '')}"

    return True, ""


def verify_bundle(bundle: dict) -> dict:
    """Full verification of a proof bundle. Returns a result dict."""
    result = {
        "valid": False,
        "chain_valid": True,
        "daemon_known": True,
    }

    # Check version.
    version = bundle.get("version", 0)
    if version != 1:
        result["error_code"] = "invalid_bundle"
        result["error_message"] = f"unsupported proof bundle version {version}"
        return result

    # Step 0: Content hash.
    ok, msg = verify_content_hash(bundle)
    if not ok:
        result["error_code"] = "invalid_signature"
        result["error_message"] = msg
        return result

    # Step 1: Signature.
    sig_valid, msg = verify_signature(bundle)
    if sig_valid is not None:
        result["signature_valid"] = sig_valid
        if not sig_valid:
            result["error_code"] = "invalid_signature"
            result["error_message"] = msg
            return result

    # Step 2: Daemon identity.
    ok, msg = verify_daemon_identity(bundle)
    if not ok:
        result["daemon_known"] = False
        result["error_code"] = "unknown_daemon"
        result["error_message"] = msg
        return result

    # Step 3: Chain integrity.
    ok, msg = verify_chain(bundle)
    if not ok:
        result["chain_valid"] = False
        result["error_code"] = "chain_mismatch"
        result["error_message"] = msg
        return result

    result["valid"] = True
    return result


def main():
    if len(sys.argv) < 2:
        print("usage: python3 verify.py <proof-bundle.json>", file=sys.stderr)
        sys.exit(2)

    path = sys.argv[1]
    try:
        with open(path, "r", encoding="utf-8") as f:
            bundle = json.load(f)
    except (OSError, json.JSONDecodeError) as e:
        print(f"ERROR: cannot read proof bundle: {e}", file=sys.stderr)
        sys.exit(2)

    result = verify_bundle(bundle)
    print(json.dumps(result, indent=2))

    if result["valid"]:
        print("\nverify.py: VALID")
        sys.exit(0)
    else:
        code = result.get("error_code", "unknown")
        msg = result.get("error_message", "")
        print(f"\nverify.py: INVALID — {code}: {msg}")
        sys.exit(1)


if __name__ == "__main__":
    main()
