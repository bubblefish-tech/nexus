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

package registry

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/bubblefish-tech/nexus/internal/a2a"
)

const (
	sigAlgorithm = "Ed25519"
)

// SignAgentCard signs the agent card using the given Ed25519 private key.
// It canonicalizes the card (with the Signature field omitted), signs
// the canonical bytes, and sets the Signature field on the card.
func SignAgentCard(card *a2a.AgentCard, key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return fmt.Errorf("registry: invalid Ed25519 private key size: %d", len(key))
	}

	pubKey := key.Public().(ed25519.PublicKey)
	keyID := hex.EncodeToString(pubKey[:8])

	// Ensure the public key is in the card's PublicKeys list.
	ensurePublicKey(card, pubKey, keyID)

	canonical, err := canonicalCardBytes(card)
	if err != nil {
		return fmt.Errorf("registry: canonicalize card: %w", err)
	}

	sig := ed25519.Sign(key, canonical)

	card.Signature = &a2a.CardSignature{
		Algorithm: sigAlgorithm,
		KeyID:     keyID,
		Value:     base64.StdEncoding.EncodeToString(sig),
	}
	return nil
}

// VerifyAgentCard verifies the signature on an agent card.
// If pinnedKey is non-empty, the signature must match that specific key.
// Otherwise, the signature is verified against the public key embedded in the card.
func VerifyAgentCard(card *a2a.AgentCard, pinnedKey string) error {
	if card.Signature == nil {
		return fmt.Errorf("registry: card has no signature")
	}
	if card.Signature.Algorithm != sigAlgorithm {
		return fmt.Errorf("registry: unsupported signature algorithm %q", card.Signature.Algorithm)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(card.Signature.Value)
	if err != nil {
		return fmt.Errorf("registry: decode signature: %w", err)
	}

	canonical, err := canonicalCardBytes(card)
	if err != nil {
		return fmt.Errorf("registry: canonicalize card: %w", err)
	}

	// If a pinned key is provided, use that.
	if pinnedKey != "" {
		pubKeyBytes, err := hex.DecodeString(pinnedKey)
		if err != nil {
			return fmt.Errorf("registry: decode pinned key: %w", err)
		}
		if len(pubKeyBytes) != ed25519.PublicKeySize {
			return fmt.Errorf("registry: invalid pinned key size: %d", len(pubKeyBytes))
		}
		if !ed25519.Verify(ed25519.PublicKey(pubKeyBytes), canonical, sigBytes) {
			return fmt.Errorf("registry: signature verification failed against pinned key")
		}
		return nil
	}

	// No pinned key: verify against the card's embedded public key.
	slog.Warn("registry: no pinned key configured, verifying against embedded key",
		"keyId", card.Signature.KeyID)

	pubKey, err := findPublicKey(card, card.Signature.KeyID)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pubKey, canonical, sigBytes) {
		return fmt.Errorf("registry: signature verification failed against embedded key")
	}
	return nil
}

// canonicalCardBytes returns the RFC 8785 canonical JSON of the card
// with the Signature field stripped.
func canonicalCardBytes(card *a2a.AgentCard) ([]byte, error) {
	// Make a copy without the signature.
	tmp := *card
	tmp.Signature = nil

	raw, err := json.Marshal(tmp)
	if err != nil {
		return nil, fmt.Errorf("marshal card: %w", err)
	}
	return a2a.Canonicalize(raw)
}

// ensurePublicKey adds the public key to the card's PublicKeys if not already present.
func ensurePublicKey(card *a2a.AgentCard, pub ed25519.PublicKey, keyID string) {
	xEncoded := base64.RawURLEncoding.EncodeToString(pub)

	for _, pk := range card.PublicKeys {
		if pk.Kid == keyID {
			return // already present
		}
	}
	card.PublicKeys = append(card.PublicKeys, a2a.PublicKeyJWK{
		Kty: "OKP",
		Crv: "Ed25519",
		X:   xEncoded,
		Kid: keyID,
		Alg: "EdDSA",
		Use: "sig",
	})
}

// findPublicKey locates the Ed25519 public key with the given keyID in the card.
func findPublicKey(card *a2a.AgentCard, keyID string) (ed25519.PublicKey, error) {
	for _, pk := range card.PublicKeys {
		if pk.Kid != keyID {
			continue
		}
		if pk.Kty != "OKP" || pk.Crv != "Ed25519" {
			continue
		}
		raw, err := base64.RawURLEncoding.DecodeString(pk.X)
		if err != nil {
			return nil, fmt.Errorf("registry: decode public key X: %w", err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("registry: invalid public key size: %d", len(raw))
		}
		return ed25519.PublicKey(raw), nil
	}
	return nil, fmt.Errorf("registry: public key %q not found in card", keyID)
}
