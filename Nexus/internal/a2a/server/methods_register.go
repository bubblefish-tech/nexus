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

package server

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/jsonrpc"
	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/a2a/transport"
)

// registerParams is the wire format for agent/register.
type registerParams struct {
	Card              a2a.AgentCard `json:"card"`
	RegistrationToken string        `json:"registration_token"`
}

// registerResult is the wire response for a successful agent/register.
type registerResult struct {
	AgentID      string `json:"agent_id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	RegisteredAt string `json:"registered_at"`
	Reregistered bool   `json:"reregistered,omitempty"`
}

// handleAgentRegister handles the agent/register JSON-RPC method.
//
// Security: method returns -32601 (method not found) when self-registration is
// disabled, hiding the endpoint's existence. When enabled, the caller must
// supply the correct registration token (constant-time compare). Ping-back
// verification prevents ghost-agent registrations.
func (s *Server) handleAgentRegister(ctx context.Context, method string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	// Gate: return method-not-found when self-registration is disabled.
	if s.registrationToken == "" || s.registrationStore == nil {
		return nil, &jsonrpc.ErrorObject{Code: a2a.CodeMethodNotFound, Message: "method not found"}
	}

	// Unmarshal params.
	var p registerParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpc.ErrorObject{Code: a2a.CodeInvalidParams, Message: "invalid params: " + err.Error()}
	}

	// Authenticate with constant-time compare.
	if subtle.ConstantTimeCompare([]byte(p.RegistrationToken), []byte(s.registrationToken)) != 1 {
		return nil, &jsonrpc.ErrorObject{Code: a2a.CodeUnauthenticated, Message: "invalid registration token"}
	}

	// Validate the agent card.
	if p.Card.Name == "" {
		return nil, &jsonrpc.ErrorObject{Code: a2a.CodeInvalidParams, Message: "card.name is required"}
	}
	if len(p.Card.Endpoints) == 0 {
		return nil, &jsonrpc.ErrorObject{Code: a2a.CodeInvalidParams, Message: "card.endpoints must have at least one entry"}
	}
	ep := p.Card.Endpoints[0]
	if ep.URL == "" {
		return nil, &jsonrpc.ErrorObject{Code: a2a.CodeInvalidParams, Message: "card.endpoints[0].url is required"}
	}
	if !a2a.ValidTransportKind(ep.Transport) {
		return nil, &jsonrpc.ErrorObject{Code: a2a.CodeInvalidParams, Message: "card.endpoints[0].transport is invalid"}
	}

	// Verify card signature if present.
	if p.Card.Signature != nil {
		if err := registry.VerifyAgentCard(&p.Card, ""); err != nil {
			return nil, &jsonrpc.ErrorObject{Code: a2a.CodeInvalidParams, Message: "card signature invalid: " + err.Error()}
		}
	}

	tc := transport.TransportConfig{
		Kind: string(ep.Transport),
		URL:  ep.URL,
	}

	// Check for existing registration.
	existing, _ := s.registrationStore.GetByName(ctx, p.Card.Name)
	if existing != nil {
		// Re-registration: verify pinned key consistency.
		incomingKey := extractPinnedKey(p.Card)
		if existing.PinnedPublicKey != "" && incomingKey != "" && existing.PinnedPublicKey != incomingKey {
			return nil, &jsonrpc.ErrorObject{
				Code:    a2a.CodeAlreadyExists,
				Message: "agent with this name is registered with a different public key",
			}
		}

		// Ping-back before accepting updated config.
		if s.agentPinger != nil {
			probe := registry.RegisteredAgent{
				AgentID:         existing.AgentID,
				Name:            existing.Name,
				TransportConfig: tc,
				Status:          registry.StatusActive,
			}
			if err := s.agentPinger.Check(ctx, probe); err != nil {
				return nil, &jsonrpc.ErrorObject{
					Code:    a2a.CodeAgentOffline,
					Message: "agent not reachable at declared URL: " + err.Error(),
				}
			}
		}

		card := a2a.AgentCard{
			Name:      p.Card.Name,
			Methods:   p.Card.Methods,
			URL:       ep.URL,
			Endpoints: p.Card.Endpoints,
			Skills:    p.Card.Skills,
		}
		if err := s.registrationStore.UpdateTransportAndCard(ctx, existing.AgentID, card, p.Card.Name, tc); err != nil {
			s.logger.Error("agent/register: update failed", "name", p.Card.Name, "error", err)
			return nil, &jsonrpc.ErrorObject{Code: a2a.CodeInternalError, Message: "failed to update registration"}
		}

		s.logger.Info("agent/register: re-registered agent", "name", p.Card.Name, "agent_id", existing.AgentID)
		return &registerResult{
			AgentID:      existing.AgentID,
			Name:         p.Card.Name,
			Status:       registry.StatusActive,
			RegisteredAt: time.Now().UTC().Format(time.RFC3339),
			Reregistered: true,
		}, nil
	}

	// New registration: ping-back first.
	agentID := a2a.NewAgentID()
	if s.agentPinger != nil {
		probe := registry.RegisteredAgent{
			AgentID:         agentID,
			Name:            p.Card.Name,
			TransportConfig: tc,
			Status:          registry.StatusActive,
		}
		if err := s.agentPinger.Check(ctx, probe); err != nil {
			return nil, &jsonrpc.ErrorObject{
				Code:    a2a.CodeAgentOffline,
				Message: "agent not reachable at declared URL: " + err.Error(),
			}
		}
	}

	now := time.Now()
	agent := registry.RegisteredAgent{
		AgentID:         agentID,
		Name:            p.Card.Name,
		DisplayName:     p.Card.Name,
		AgentCard:       p.Card,
		TransportConfig: tc,
		PinnedPublicKey: extractPinnedKey(p.Card),
		Status:          registry.StatusActive,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.registrationStore.Register(ctx, agent); err != nil {
		s.logger.Error("agent/register: insert failed", "name", p.Card.Name, "error", err)
		return nil, &jsonrpc.ErrorObject{Code: a2a.CodeInternalError, Message: "failed to persist registration"}
	}

	s.logger.Info("agent/register: registered new agent", "name", p.Card.Name, "agent_id", agentID, "url", ep.URL)
	return &registerResult{
		AgentID:      agentID,
		Name:         p.Card.Name,
		Status:       registry.StatusActive,
		RegisteredAt: now.UTC().Format(time.RFC3339),
	}, nil
}

// extractPinnedKey returns a hex-encoded fingerprint of the first public key
// in the card's PublicKeys list, or "" if none. Used to detect key changes on
// re-registration (impersonation prevention).
func extractPinnedKey(card a2a.AgentCard) string {
	if len(card.PublicKeys) == 0 {
		return ""
	}
	pk := card.PublicKeys[0]
	// JWK OKP (Ed25519): key material is in the X field (base64url, unpadded).
	if pk.Kty == "OKP" && pk.X != "" {
		raw, err := base64.RawURLEncoding.DecodeString(pk.X)
		if err != nil {
			return ""
		}
		return hex.EncodeToString(raw)
	}
	// RSA: fingerprint the N field.
	if pk.Kty == "RSA" && pk.N != "" {
		raw, err := base64.RawURLEncoding.DecodeString(pk.N)
		if err != nil {
			return ""
		}
		return hex.EncodeToString(raw)
	}
	return ""
}
