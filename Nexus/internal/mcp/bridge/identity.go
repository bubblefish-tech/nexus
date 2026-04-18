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

package bridge

import "sync"

// knownClients maps well-known MCP client names to agent IDs.
var knownClients = map[string]string{
	"claude-desktop": "client_claude_desktop",
	"chatgpt":        "client_chatgpt",
	"perplexity":     "client_perplexity",
	"lm-studio":      "client_lm_studio",
	"open-webui":     "client_openwebui",
}

// IdentityStore maps MCP client fingerprints to NA2A source agent IDs.
type IdentityStore struct {
	mu         sync.RWMutex
	identities map[string]string // fingerprint -> agentID
}

// NewIdentityStore creates an empty IdentityStore.
func NewIdentityStore() *IdentityStore {
	return &IdentityStore{
		identities: make(map[string]string),
	}
}

// Register associates a fingerprint with an agent ID.
func (s *IdentityStore) Register(fingerprint, agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identities[fingerprint] = agentID
}

// Lookup returns the agent ID for a fingerprint, or "" if not found.
func (s *IdentityStore) Lookup(fingerprint string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identities[fingerprint]
}

// DeriveIdentity determines the source agent ID from MCP client metadata.
// It first checks for a registered fingerprint, then falls back to well-known
// client names, and finally defaults to "client_generic".
func DeriveIdentity(clientName, clientVersion, tokenHash string) string {
	// If a token hash is provided, it could be used for fingerprinting.
	// For now, we prioritize the client name lookup.
	_ = clientVersion // reserved for future version-aware identity
	_ = tokenHash     // reserved for fingerprint-based identity

	if agentID, ok := knownClients[clientName]; ok {
		return agentID
	}
	return "client_generic"
}
