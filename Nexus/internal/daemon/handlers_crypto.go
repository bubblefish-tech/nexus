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

package daemon

import (
	"crypto/sha256"
	"fmt"
	"net/http"
)

type signingStatusDTO struct {
	Enabled        bool   `json:"enabled"`
	Reason         string `json:"reason,omitempty"`
	ConfigHint     string `json:"config_hint,omitempty"`
	PublicKeyHash  string `json:"public_key_hash,omitempty"`
	SignedCount    int64  `json:"signed_count"`
	VerifyFailures int64  `json:"verify_failures"`
}

// handleCryptoSigning serves GET /api/crypto/signing — audit signing subsystem status.
func (d *Daemon) handleCryptoSigning(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/crypto/signing").Inc()

	ss := signingStatusDTO{}
	if d.auditLogger != nil && d.daemonKeyPair != nil {
		ss.Enabled = true
		hash := sha256.Sum256(d.daemonKeyPair.PublicKey)
		ss.PublicKeyHash = fmt.Sprintf("%x", hash)[:16]
		if d.chainState != nil {
			ss.SignedCount = d.chainState.EntryCount()
		}
	} else {
		if d.daemonKeyPair == nil {
			ss.Reason = "password not set"
			ss.ConfigHint = "Set NEXUS_PASSWORD environment variable and restart the daemon"
		} else {
			ss.Reason = "audit logger not initialized"
			ss.ConfigHint = "See logs: check 'provenance' subsystem startup messages"
		}
	}
	d.writeJSON(w, http.StatusOK, ss)
}

type cryptoProfileDTO struct {
	Symmetric string `json:"symmetric"`
	Signing   string `json:"signing"`
	KDF       string `json:"kdf"`
	Hash      string `json:"hash"`
	Ratchet   string `json:"ratchet"`
}

// handleCryptoProfile serves GET /api/crypto/profile — active algorithm set.
func (d *Daemon) handleCryptoProfile(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/crypto/profile").Inc()
	d.writeJSON(w, http.StatusOK, cryptoProfileDTO{
		Symmetric: "AES-256-GCM",
		Signing:   "Ed25519",
		KDF:       "Argon2id + HKDF-SHA-256",
		Hash:      "SHA-256",
		Ratchet:   "HMAC-SHA-256",
	})
}

type masterKeyStatusDTO struct {
	Derived   bool   `json:"derived"`
	Algorithm string `json:"algorithm"`
	Reason    string `json:"reason,omitempty"`
}

// handleCryptoMaster serves GET /api/crypto/master — master key derivation status.
func (d *Daemon) handleCryptoMaster(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/crypto/master").Inc()
	ms := masterKeyStatusDTO{Algorithm: "Argon2id"}
	if d.mkm != nil && d.mkm.IsEnabled() {
		ms.Derived = true
	} else {
		ms.Reason = "NEXUS_PASSWORD not set"
	}
	d.writeJSON(w, http.StatusOK, ms)
}

type ratchetStatusDTO struct {
	Position       int64  `json:"position"`
	DestroyedCount int64  `json:"destroyed_count"`
	Algorithm      string `json:"algorithm"`
}

// handleCryptoRatchet serves GET /api/crypto/ratchet — forward-secure ratchet status.
func (d *Daemon) handleCryptoRatchet(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/crypto/ratchet").Inc()
	rs := ratchetStatusDTO{Algorithm: "HMAC-SHA-256"}
	if d.chainState != nil {
		rs.Position = d.chainState.EntryCount()
	}
	d.writeJSON(w, http.StatusOK, rs)
}
