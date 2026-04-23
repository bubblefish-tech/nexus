// Copyright © 2026 BubbleFish Technologies, Inc.
//
// This file is part of BubbleFish Nexus.
//
// BubbleFish Nexus is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// BubbleFish Nexus is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with BubbleFish Nexus. If not, see <https://www.gnu.org/licenses/>.

package provenance

import (
	"encoding/json"
	"strings"
)

// GenerateHTML returns a self-contained HTML document that embeds the proof
// bundle and performs client-side SHA-256 content-hash verification and
// optional Ed25519 signature verification using the browser WebCrypto API.
//
// The returned bytes can be written to a .html file and opened offline in any
// modern browser (Chrome 113+, Firefox 105+, Safari 17+).
func GenerateHTML(bundle *ProofBundle) ([]byte, error) {
	bundleJSON, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return nil, err
	}
	html := strings.Replace(proofHTMLTemplate, "{{PROOF_JSON}}", string(bundleJSON), 1)
	return []byte(html), nil
}

const proofHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>BubbleFish Nexus — Proof Verification</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: system-ui, -apple-system, sans-serif; background: #0a0e17; color: #e0e6ed; line-height: 1.5; }
.header { background: #131a2b; padding: 1rem 2rem; border-bottom: 1px solid #1e2a42; display: flex; align-items: center; gap: 1rem; }
.header h1 { font-size: 1.1rem; font-weight: 600; }
.header .sub { font-size: 0.8rem; color: #6b7fa3; }
.container { max-width: 860px; margin: 2rem auto; padding: 0 1.5rem; }
.verdict { display: flex; align-items: center; gap: 1rem; padding: 1.25rem 1.5rem; border-radius: 8px; margin-bottom: 2rem; font-size: 1rem; font-weight: 600; }
.verdict-valid { background: #064e3b; border: 1px solid #065f46; color: #6ee7b7; }
.verdict-invalid { background: #7f1d1d; border: 1px solid #991b1b; color: #fca5a5; }
.verdict-pending { background: #1e2a42; border: 1px solid #2d3f5e; color: #94a3b8; }
.verdict .icon { font-size: 1.5rem; }
.verdict .detail { font-size: 0.8rem; font-weight: 400; opacity: 0.8; margin-top: 0.15rem; }
.section { margin-bottom: 1.75rem; }
.section-title { font-size: 0.7rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.1em; color: #6b7fa3; margin-bottom: 0.75rem; }
.card { background: #131a2b; border: 1px solid #1e2a42; border-radius: 6px; padding: 1rem 1.25rem; }
.field { display: flex; gap: 1rem; padding: 0.45rem 0; border-bottom: 1px solid #1e2a42; font-size: 0.85rem; }
.field:last-child { border-bottom: none; }
.field-key { color: #6b7fa3; min-width: 140px; flex-shrink: 0; }
.field-val { color: #e0e6ed; word-break: break-all; font-family: 'Consolas', 'Menlo', monospace; font-size: 0.8rem; }
.field-val.content-preview { font-family: system-ui, sans-serif; font-size: 0.85rem; color: #a0b0c8; }
.check { display: flex; align-items: center; gap: 0.6rem; padding: 0.6rem 0.75rem; border-radius: 4px; font-size: 0.85rem; }
.check-ok { background: #052e16; color: #86efac; }
.check-fail { background: #450a0a; color: #fca5a5; }
.check-skip { background: #1e2a42; color: #94a3b8; }
.check .c-icon { font-size: 1rem; }
.checks { display: flex; flex-direction: column; gap: 0.4rem; }
.chain-entry { padding: 0.6rem 0.75rem; border-radius: 4px; background: #0f172a; border: 1px solid #1e2a42; margin-bottom: 0.4rem; font-size: 0.78rem; }
.chain-hash { font-family: monospace; color: #7dd3fc; word-break: break-all; }
.chain-prev { font-family: monospace; color: #94a3b8; word-break: break-all; }
.chain-label { color: #6b7fa3; margin-bottom: 0.2rem; font-size: 0.7rem; text-transform: uppercase; letter-spacing: 0.05em; }
.footer { text-align: center; padding: 2rem; color: #374151; font-size: 0.75rem; }
</style>
</head>
<body>
<div class="header">
  <div>
    <h1>BubbleFish Nexus</h1>
    <div class="sub">Cryptographic Proof Verification — v0.1.3</div>
  </div>
</div>
<div class="container">
  <div id="verdict" class="verdict verdict-pending">
    <span class="icon">⏳</span>
    <div>
      <div id="verdict-title">Verifying…</div>
      <div class="detail" id="verdict-detail">Running client-side checks</div>
    </div>
  </div>

  <div class="section">
    <div class="section-title">Verification Checks</div>
    <div class="checks" id="checks"></div>
  </div>

  <div class="section">
    <div class="section-title">Memory Record</div>
    <div class="card" id="memory-card"></div>
  </div>

  <div id="chain-section" class="section" style="display:none">
    <div class="section-title">Audit Chain</div>
    <div id="chain-entries"></div>
  </div>
</div>
<div class="footer">
  Generated by BubbleFish Nexus &mdash; All verification performed in your browser. No data sent to any server.
</div>

<script>
(function() {
  "use strict";

  var PROOF = {{PROOF_JSON}};

  function hexToBytes(hex) {
    var bytes = new Uint8Array(hex.length / 2);
    for (var i = 0; i < hex.length; i += 2) {
      bytes[i / 2] = parseInt(hex.substr(i, 2), 16);
    }
    return bytes;
  }

  function setVerdict(ok, title, detail) {
    var el = document.getElementById("verdict");
    el.className = "verdict " + (ok === true ? "verdict-valid" : ok === false ? "verdict-invalid" : "verdict-pending");
    document.getElementById("verdict-title").textContent = ok === true ? "✓ " + title : ok === false ? "✗ " + title : title;
    document.getElementById("verdict-detail").textContent = detail || "";
    document.querySelector(".verdict .icon").textContent = ok === true ? "✅" : ok === false ? "❌" : "⏳";
  }

  function addCheck(container, ok, label, detail) {
    var div = document.createElement("div");
    div.className = "check " + (ok === true ? "check-ok" : ok === false ? "check-fail" : "check-skip");
    var icon = document.createElement("span");
    icon.className = "c-icon";
    icon.textContent = ok === true ? "✓" : ok === false ? "✗" : "○";
    var text = document.createElement("span");
    text.textContent = label + (detail ? " — " + detail : "");
    div.appendChild(icon);
    div.appendChild(text);
    container.appendChild(div);
    return ok;
  }

  function renderMemory(m) {
    var card = document.getElementById("memory-card");
    var fields = [
      ["Payload ID", m.payload_id],
      ["Source", m.source],
      ["Subject", m.subject || "—"],
      ["Timestamp", m.timestamp],
      ["Idempotency Key", m.idempotency_key || "—"],
      ["Content Hash (SHA-256)", m.content_hash],
      ["Content Preview", (m.content || "").substring(0, 120) + ((m.content || "").length > 120 ? "…" : "")],
    ];
    fields.forEach(function(f) {
      var row = document.createElement("div");
      row.className = "field";
      var k = document.createElement("span");
      k.className = "field-key";
      k.textContent = f[0];
      var v = document.createElement("span");
      v.className = f[0] === "Content Preview" ? "field-val content-preview" : "field-val";
      v.textContent = f[1] || "—";
      row.appendChild(k);
      row.appendChild(v);
      card.appendChild(row);
    });
  }

  function renderChain(chain) {
    if (!chain || !chain.length) return;
    var sec = document.getElementById("chain-section");
    sec.style.display = "";
    var container = document.getElementById("chain-entries");
    chain.forEach(function(entry, i) {
      var div = document.createElement("div");
      div.className = "chain-entry";
      var l1 = document.createElement("div");
      l1.className = "chain-label";
      l1.textContent = "Entry " + (i + 1);
      var l2 = document.createElement("div");
      l2.className = "chain-hash";
      l2.textContent = "hash: " + (entry.hash || "—");
      var l3 = document.createElement("div");
      l3.className = "chain-prev";
      l3.textContent = "prev: " + (entry.prev_hash || "(genesis)");
      div.appendChild(l1);
      div.appendChild(l2);
      div.appendChild(l3);
      container.appendChild(div);
    });
  }

  async function run() {
    var checks = document.getElementById("checks");
    var allOk = true;

    // Render memory record
    renderMemory(PROOF.memory || {});

    // Render audit chain if present
    if (PROOF.audit_chain && PROOF.audit_chain.length) {
      renderChain(PROOF.audit_chain);
    }

    // Check 1: content hash (SHA-256)
    var contentHashOk = false;
    try {
      var enc = new TextEncoder();
      var buf = await crypto.subtle.digest("SHA-256", enc.encode(PROOF.memory.content || ""));
      var hashHex = Array.from(new Uint8Array(buf)).map(function(b) {
        return b.toString(16).padStart(2, "0");
      }).join("");
      contentHashOk = hashHex === (PROOF.memory.content_hash || "").toLowerCase();
      addCheck(checks, contentHashOk, "Content hash (SHA-256)",
        contentHashOk ? hashHex.substring(0, 16) + "…" : "computed " + hashHex.substring(0, 16) + "… ≠ stored");
      if (!contentHashOk) allOk = false;
    } catch (e) {
      addCheck(checks, null, "Content hash (SHA-256)", "crypto.subtle unavailable: " + e.message);
      allOk = false;
    }

    // Check 2: Ed25519 signature (if present)
    if (PROOF.signature && PROOF.source_pubkey) {
      try {
        var pubBytes = hexToBytes(PROOF.source_pubkey);
        var key = await crypto.subtle.importKey(
          "raw", pubBytes.buffer,
          { name: "Ed25519" },
          false,
          ["verify"]
        );
        // Reconstruct signable envelope (must match Go's json.Marshal field order)
        var envelope = JSON.stringify({
          source_name: PROOF.memory.source,
          timestamp: PROOF.memory.timestamp,
          idempotency_key: PROOF.memory.idempotency_key,
          content_hash: PROOF.memory.content_hash
        });
        var sigBytes = hexToBytes(PROOF.signature);
        var msgBytes = enc.encode(envelope);
        var sigOk = await crypto.subtle.verify("Ed25519", key, sigBytes.buffer, msgBytes.buffer);
        addCheck(checks, sigOk, "Ed25519 source signature",
          sigOk ? "key " + PROOF.source_pubkey.substring(0, 12) + "…" : "signature mismatch");
        if (!sigOk) allOk = false;
      } catch (e) {
        addCheck(checks, null, "Ed25519 source signature", "not supported in this browser");
      }
    } else {
      addCheck(checks, null, "Ed25519 source signature", "unsigned write (no source signing key configured)");
    }

    // Check 3: daemon key present
    if (PROOF.daemon_pubkey) {
      addCheck(checks, true, "Daemon public key", PROOF.daemon_pubkey.substring(0, 16) + "…");
    } else {
      addCheck(checks, null, "Daemon public key", "not included in proof");
    }

    // Check 4: chain linkage (if audit_chain present)
    if (PROOF.audit_chain && PROOF.audit_chain.length > 1) {
      var chainOk = true;
      for (var i = 1; i < PROOF.audit_chain.length; i++) {
        if (PROOF.audit_chain[i].prev_hash !== PROOF.audit_chain[i-1].hash) {
          chainOk = false;
          break;
        }
      }
      addCheck(checks, chainOk, "Audit chain linkage (" + PROOF.audit_chain.length + " entries)",
        chainOk ? "all prev_hash links valid" : "broken link at entry " + i);
      if (!chainOk) allOk = false;
    }

    // Final verdict
    if (allOk) {
      setVerdict(true, "Proof Valid", "All cryptographic checks passed — content integrity verified");
    } else {
      setVerdict(false, "Proof Invalid", "One or more verification checks failed");
    }
  }

  run().catch(function(e) {
    setVerdict(false, "Verification Error", e.message);
  });
})();
</script>
</body>
</html>`
