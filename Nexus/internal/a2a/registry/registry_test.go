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
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
	"github.com/BubbleFish-Nexus/internal/a2a/transport"
)

// -----------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test_registry.db")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func testAgent(id, name string) RegisteredAgent {
	return RegisteredAgent{
		AgentID:     id,
		Name:        name,
		DisplayName: "Test Agent " + name,
		AgentCard: a2a.AgentCard{
			Name:            name,
			URL:             "http://localhost:8080",
			ProtocolVersion: "0.1",
			Endpoints: []a2a.Endpoint{
				{URL: "http://localhost:8080/a2a", Transport: a2a.TransportHTTP},
			},
			Capabilities: a2a.AgentCapabilities{Streaming: true},
		},
		TransportConfig: transport.TransportConfig{
			Kind: "http",
			URL:  "http://localhost:8080",
		},
		Status: StatusActive,
	}
}

func genKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

// -----------------------------------------------------------------------
// Store CRUD tests
// -----------------------------------------------------------------------

func TestStoreRegisterAndGet(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	if err := s.Register(ctx, agent); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := s.Get(ctx, "agent-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentID != "agent-1" {
		t.Errorf("AgentID=%q, want %q", got.AgentID, "agent-1")
	}
	if got.Name != "alpha" {
		t.Errorf("Name=%q, want %q", got.Name, "alpha")
	}
	if got.DisplayName != "Test Agent alpha" {
		t.Errorf("DisplayName=%q, want %q", got.DisplayName, "Test Agent alpha")
	}
	if got.Status != StatusActive {
		t.Errorf("Status=%q, want %q", got.Status, StatusActive)
	}
	if got.AgentCard.Name != "alpha" {
		t.Errorf("AgentCard.Name=%q, want %q", got.AgentCard.Name, "alpha")
	}
	if got.TransportConfig.Kind != "http" {
		t.Errorf("TransportConfig.Kind=%q, want %q", got.TransportConfig.Kind, "http")
	}
}

func TestStoreGetNotFound(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	_, err := s.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestStoreGetByName(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-2", "beta")
	if err := s.Register(ctx, agent); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := s.GetByName(ctx, "beta")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.AgentID != "agent-2" {
		t.Errorf("AgentID=%q, want %q", got.AgentID, "agent-2")
	}
}

func TestStoreGetByNameNotFound(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	_, err := s.GetByName(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStoreNameUniqueness(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent1 := testAgent("agent-1", "unique-name")
	if err := s.Register(ctx, agent1); err != nil {
		t.Fatalf("Register first: %v", err)
	}

	agent2 := testAgent("agent-2", "unique-name")
	err := s.Register(ctx, agent2)
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestStoreIDUniqueness(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent1 := testAgent("same-id", "name1")
	if err := s.Register(ctx, agent1); err != nil {
		t.Fatalf("Register first: %v", err)
	}

	agent2 := testAgent("same-id", "name2")
	err := s.Register(ctx, agent2)
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
}

func TestStoreInvalidStatus(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	agent.Status = "invalid"
	err := s.Register(ctx, agent)
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestStoreList(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		agent := testAgent(fmt.Sprintf("agent-%d", i), fmt.Sprintf("agent%d", i))
		if i%2 == 0 {
			agent.Status = StatusSuspended
		}
		if err := s.Register(ctx, agent); err != nil {
			t.Fatalf("Register %d: %v", i, err)
		}
	}

	// List all.
	all, err := s.List(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 5 {
		t.Errorf("len(all)=%d, want 5", len(all))
	}

	// List active only.
	active, err := s.List(ctx, ListFilter{Status: StatusActive})
	if err != nil {
		t.Fatalf("List active: %v", err)
	}
	if len(active) != 2 {
		t.Errorf("len(active)=%d, want 2", len(active))
	}

	// List suspended only.
	suspended, err := s.List(ctx, ListFilter{Status: StatusSuspended})
	if err != nil {
		t.Fatalf("List suspended: %v", err)
	}
	if len(suspended) != 3 {
		t.Errorf("len(suspended)=%d, want 3", len(suspended))
	}
}

func TestStoreListEmpty(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	agents, err := s.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected empty list, got %d", len(agents))
	}
}

func TestStoreListSorted(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	names := []string{"charlie", "alpha", "beta"}
	for i, name := range names {
		if err := s.Register(ctx, testAgent(fmt.Sprintf("a%d", i), name)); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	agents, _ := s.List(ctx, ListFilter{})
	if agents[0].Name != "alpha" || agents[1].Name != "beta" || agents[2].Name != "charlie" {
		t.Errorf("not sorted: %v, %v, %v", agents[0].Name, agents[1].Name, agents[2].Name)
	}
}

func TestStoreUpdateStatus(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	if err := s.Register(ctx, agent); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := s.UpdateStatus(ctx, "agent-1", StatusSuspended); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, _ := s.Get(ctx, "agent-1")
	if got.Status != StatusSuspended {
		t.Errorf("Status=%q, want %q", got.Status, StatusSuspended)
	}
}

func TestStoreUpdateStatusInvalid(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	s.Register(ctx, agent)

	err := s.UpdateStatus(ctx, "agent-1", "bogus")
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestStoreUpdateStatusNotFound(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	err := s.UpdateStatus(context.Background(), "nonexistent", StatusActive)
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestStoreStatusTransitions(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	s.Register(ctx, agent)

	transitions := []string{StatusSuspended, StatusActive, StatusRetired}
	for _, status := range transitions {
		if err := s.UpdateStatus(ctx, "agent-1", status); err != nil {
			t.Fatalf("transition to %q: %v", status, err)
		}
		got, _ := s.Get(ctx, "agent-1")
		if got.Status != status {
			t.Errorf("after transition: Status=%q, want %q", got.Status, status)
		}
	}
}

func TestStoreUpdateLastSeen(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	s.Register(ctx, agent)

	now := time.Now()
	if err := s.UpdateLastSeen(ctx, "agent-1", now, ""); err != nil {
		t.Fatalf("UpdateLastSeen: %v", err)
	}

	got, _ := s.Get(ctx, "agent-1")
	if got.LastSeenAt == nil {
		t.Fatal("LastSeenAt is nil")
	}
	if got.LastError != "" {
		t.Errorf("LastError=%q, want empty", got.LastError)
	}
}

func TestStoreUpdateLastSeenWithError(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	s.Register(ctx, agent)

	now := time.Now()
	if err := s.UpdateLastSeen(ctx, "agent-1", now, "connection refused"); err != nil {
		t.Fatalf("UpdateLastSeen: %v", err)
	}

	got, _ := s.Get(ctx, "agent-1")
	if got.LastError != "connection refused" {
		t.Errorf("LastError=%q, want %q", got.LastError, "connection refused")
	}
}

func TestStoreUpdateLastSeenNotFound(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	err := s.UpdateLastSeen(context.Background(), "nonexistent", time.Now(), "")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestStoreDelete(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	s.Register(ctx, agent)

	if err := s.Delete(ctx, "agent-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := s.Get(ctx, "agent-1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestStoreDeleteNotFound(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	err := s.Delete(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestStoreDeleteThenReRegister(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	s.Register(ctx, agent)
	s.Delete(ctx, "agent-1")

	// Re-register with same name.
	agent2 := testAgent("agent-2", "alpha")
	if err := s.Register(ctx, agent2); err != nil {
		t.Fatalf("re-register: %v", err)
	}
}

func TestStoreAgentCardRoundtrip(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	agent.AgentCard.Skills = []a2a.Skill{
		{ID: "skill-1", Name: "code-review", Description: "Reviews code"},
	}
	agent.AgentCard.Capabilities = a2a.AgentCapabilities{
		Streaming:        true,
		PushNotifications: true,
		StateTransitions: true,
	}

	s.Register(ctx, agent)
	got, _ := s.Get(ctx, "agent-1")

	if len(got.AgentCard.Skills) != 1 {
		t.Fatalf("Skills len=%d, want 1", len(got.AgentCard.Skills))
	}
	if got.AgentCard.Skills[0].Name != "code-review" {
		t.Errorf("Skill name=%q, want %q", got.AgentCard.Skills[0].Name, "code-review")
	}
	if !got.AgentCard.Capabilities.Streaming {
		t.Error("Capabilities.Streaming=false, want true")
	}
	if !got.AgentCard.Capabilities.PushNotifications {
		t.Error("Capabilities.PushNotifications=false, want true")
	}
}

func TestStoreTransportConfigRoundtrip(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	agent.TransportConfig = transport.TransportConfig{
		Kind:      "http",
		URL:       "http://localhost:9090",
		AuthType:  "bearer",
		AuthToken: "secret-token",
		TimeoutMs: 5000,
	}

	s.Register(ctx, agent)
	got, _ := s.Get(ctx, "agent-1")

	if got.TransportConfig.Kind != "http" {
		t.Errorf("Kind=%q, want %q", got.TransportConfig.Kind, "http")
	}
	if got.TransportConfig.URL != "http://localhost:9090" {
		t.Errorf("URL=%q", got.TransportConfig.URL)
	}
	if got.TransportConfig.AuthType != "bearer" {
		t.Errorf("AuthType=%q", got.TransportConfig.AuthType)
	}
	if got.TransportConfig.TimeoutMs != 5000 {
		t.Errorf("TimeoutMs=%d", got.TransportConfig.TimeoutMs)
	}
}

func TestStorePinnedPublicKey(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	pub, _ := genKeyPair(t)
	agent := testAgent("agent-1", "alpha")
	agent.PinnedPublicKey = hex.EncodeToString(pub)

	s.Register(ctx, agent)
	got, _ := s.Get(ctx, "agent-1")

	if got.PinnedPublicKey != hex.EncodeToString(pub) {
		t.Errorf("PinnedPublicKey mismatch")
	}
}

// -----------------------------------------------------------------------
// Signing tests
// -----------------------------------------------------------------------

func TestSignAndVerifyAgentCard(t *testing.T) {
	t.Helper()
	pub, priv := genKeyPair(t)

	card := &a2a.AgentCard{
		Name:            "test-agent",
		URL:             "http://localhost:8080",
		ProtocolVersion: "0.1",
		Endpoints: []a2a.Endpoint{
			{URL: "http://localhost:8080/a2a", Transport: a2a.TransportHTTP},
		},
	}

	if err := SignAgentCard(card, priv); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if card.Signature == nil {
		t.Fatal("Signature is nil after signing")
	}
	if card.Signature.Algorithm != "Ed25519" {
		t.Errorf("Algorithm=%q, want Ed25519", card.Signature.Algorithm)
	}
	if card.Signature.Value == "" {
		t.Error("empty signature value")
	}

	// Verify with pinned key.
	pinnedKey := hex.EncodeToString(pub)
	if err := VerifyAgentCard(card, pinnedKey); err != nil {
		t.Fatalf("Verify with pinned key: %v", err)
	}
}

func TestVerifyAgentCardNoPinnedKey(t *testing.T) {
	t.Helper()
	_, priv := genKeyPair(t)

	card := &a2a.AgentCard{
		Name:            "test-agent",
		URL:             "http://localhost:8080",
		ProtocolVersion: "0.1",
	}

	SignAgentCard(card, priv)
	if err := VerifyAgentCard(card, ""); err != nil {
		t.Fatalf("Verify without pinned key: %v", err)
	}
}

func TestVerifyAgentCardWrongKey(t *testing.T) {
	t.Helper()
	_, priv1 := genKeyPair(t)
	pub2, _ := genKeyPair(t)

	card := &a2a.AgentCard{
		Name:            "test-agent",
		URL:             "http://localhost:8080",
		ProtocolVersion: "0.1",
	}

	SignAgentCard(card, priv1)

	// Verify against the wrong key.
	wrongPinned := hex.EncodeToString(pub2)
	err := VerifyAgentCard(card, wrongPinned)
	if err == nil {
		t.Fatal("expected verification failure with wrong key")
	}
}

func TestVerifyAgentCardNoSignature(t *testing.T) {
	t.Helper()
	card := &a2a.AgentCard{
		Name: "unsigned",
	}
	err := VerifyAgentCard(card, "")
	if err == nil {
		t.Fatal("expected error for unsigned card")
	}
}

func TestVerifyAgentCardBadAlgorithm(t *testing.T) {
	t.Helper()
	card := &a2a.AgentCard{
		Name: "test",
		Signature: &a2a.CardSignature{
			Algorithm: "RSA256",
			Value:     "dGVzdA==",
		},
	}
	err := VerifyAgentCard(card, "")
	if err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
}

func TestVerifyAgentCardBadSignatureEncoding(t *testing.T) {
	t.Helper()
	card := &a2a.AgentCard{
		Name: "test",
		Signature: &a2a.CardSignature{
			Algorithm: "Ed25519",
			Value:     "not-valid-base64!!!",
		},
	}
	err := VerifyAgentCard(card, "")
	if err == nil {
		t.Fatal("expected error for bad encoding")
	}
}

func TestVerifyAgentCardBadPinnedKey(t *testing.T) {
	t.Helper()
	_, priv := genKeyPair(t)
	card := &a2a.AgentCard{
		Name:            "test",
		ProtocolVersion: "0.1",
	}
	SignAgentCard(card, priv)

	err := VerifyAgentCard(card, "not-hex")
	if err == nil {
		t.Fatal("expected error for bad pinned key encoding")
	}
}

func TestVerifyAgentCardShortPinnedKey(t *testing.T) {
	t.Helper()
	_, priv := genKeyPair(t)
	card := &a2a.AgentCard{
		Name:            "test",
		ProtocolVersion: "0.1",
	}
	SignAgentCard(card, priv)

	err := VerifyAgentCard(card, "aabb")
	if err == nil {
		t.Fatal("expected error for short pinned key")
	}
}

func TestSignAgentCardInvalidKey(t *testing.T) {
	t.Helper()
	card := &a2a.AgentCard{Name: "test"}
	err := SignAgentCard(card, []byte("too-short"))
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestSignAgentCardPublicKeyAdded(t *testing.T) {
	t.Helper()
	_, priv := genKeyPair(t)

	card := &a2a.AgentCard{
		Name:            "test-agent",
		ProtocolVersion: "0.1",
	}

	SignAgentCard(card, priv)
	if len(card.PublicKeys) != 1 {
		t.Fatalf("PublicKeys len=%d, want 1", len(card.PublicKeys))
	}
	if card.PublicKeys[0].Kty != "OKP" {
		t.Errorf("Kty=%q, want OKP", card.PublicKeys[0].Kty)
	}
	if card.PublicKeys[0].Crv != "Ed25519" {
		t.Errorf("Crv=%q, want Ed25519", card.PublicKeys[0].Crv)
	}
}

func TestSignAgentCardIdempotentPublicKey(t *testing.T) {
	t.Helper()
	_, priv := genKeyPair(t)
	card := &a2a.AgentCard{Name: "test", ProtocolVersion: "0.1"}

	SignAgentCard(card, priv)
	SignAgentCard(card, priv) // sign again
	if len(card.PublicKeys) != 1 {
		t.Errorf("PublicKeys len=%d after double sign, want 1", len(card.PublicKeys))
	}
}

func TestSignatureRoundtripThroughJSON(t *testing.T) {
	t.Helper()
	pub, priv := genKeyPair(t)

	card := &a2a.AgentCard{
		Name:            "json-roundtrip",
		URL:             "http://example.com",
		ProtocolVersion: "0.1",
		Endpoints: []a2a.Endpoint{
			{URL: "http://example.com/a2a", Transport: a2a.TransportHTTP},
		},
	}

	SignAgentCard(card, priv)

	// Marshal to JSON and back.
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var card2 a2a.AgentCard
	if err := json.Unmarshal(data, &card2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if err := VerifyAgentCard(&card2, hex.EncodeToString(pub)); err != nil {
		t.Fatalf("verify after JSON roundtrip: %v", err)
	}
}

func TestCardTamperingDetected(t *testing.T) {
	t.Helper()
	pub, priv := genKeyPair(t)
	card := &a2a.AgentCard{
		Name:            "original",
		ProtocolVersion: "0.1",
	}

	SignAgentCard(card, priv)
	card.Name = "tampered"

	err := VerifyAgentCard(card, hex.EncodeToString(pub))
	if err == nil {
		t.Fatal("expected verification failure after tampering")
	}
}

// -----------------------------------------------------------------------
// ValidStatus tests
// -----------------------------------------------------------------------

func TestValidStatus(t *testing.T) {
	t.Helper()
	tests := []struct {
		status string
		valid  bool
	}{
		{StatusActive, true},
		{StatusSuspended, true},
		{StatusRetired, true},
		{"", false},
		{"unknown", false},
		{"Active", false}, // case sensitive
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := ValidStatus(tt.status); got != tt.valid {
				t.Errorf("ValidStatus(%q)=%v, want %v", tt.status, got, tt.valid)
			}
		})
	}
}

// -----------------------------------------------------------------------
// Health check tests
// -----------------------------------------------------------------------

func TestHealthCheckSuccess(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start a minimal HTTP server that responds to agent/ping.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonrpc.Request
			json.NewDecoder(r.Body).Decode(&req)
			resp, _ := jsonrpc.NewResponse(req.ID, "pong")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}),
	}
	go srv.Serve(ln)
	defer srv.Shutdown(ctx)

	s := newTestStore(t)
	agent := testAgent("agent-1", "alpha")
	agent.TransportConfig = transport.TransportConfig{
		Kind: "http",
		URL:  "http://" + ln.Addr().String(),
	}
	s.Register(ctx, agent)

	hc := NewHealthChecker(s, WithHealthTimeout(5*time.Second))
	if err := hc.Check(ctx, agent); err != nil {
		t.Fatalf("Check: %v", err)
	}

	got, _ := s.Get(ctx, "agent-1")
	if got.LastSeenAt == nil {
		t.Error("LastSeenAt should be set after successful check")
	}
	if got.LastError != "" {
		t.Errorf("LastError=%q, want empty", got.LastError)
	}
}

func TestHealthCheckFailure(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s := newTestStore(t)
	agent := testAgent("agent-1", "alpha")
	agent.TransportConfig = transport.TransportConfig{
		Kind:      "http",
		URL:       "http://127.0.0.1:1", // unreachable
		TimeoutMs: 500,
	}
	s.Register(ctx, agent)

	hc := NewHealthChecker(s, WithHealthTimeout(1*time.Second))
	err := hc.Check(ctx, agent)
	if err == nil {
		t.Fatal("expected error for unreachable agent")
	}

	got, _ := s.Get(ctx, "agent-1")
	if got.LastError == "" {
		t.Error("LastError should be set after failed check")
	}
}

func TestHealthCheckSkipsSuspended(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	agent.Status = StatusSuspended
	s.Register(ctx, agent)

	hc := NewHealthChecker(s)
	// Should return nil (no check performed) for suspended agents.
	if err := hc.Check(ctx, agent); err != nil {
		t.Fatalf("Check on suspended agent: %v", err)
	}
}

func TestHealthCheckSkipsRetired(t *testing.T) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()

	agent := testAgent("agent-1", "alpha")
	agent.Status = StatusRetired
	s.Register(ctx, agent)

	hc := NewHealthChecker(s)
	if err := hc.Check(ctx, agent); err != nil {
		t.Fatalf("Check on retired agent: %v", err)
	}
}

func TestHealthCheckAll(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start a server.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonrpc.Request
			json.NewDecoder(r.Body).Decode(&req)
			resp, _ := jsonrpc.NewResponse(req.ID, "pong")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}),
	}
	go srv.Serve(ln)
	defer srv.Shutdown(ctx)

	s := newTestStore(t)
	addr := "http://" + ln.Addr().String()

	// Register 3 active agents and 1 suspended.
	for i := 0; i < 3; i++ {
		agent := testAgent(fmt.Sprintf("agent-%d", i), fmt.Sprintf("agent%d", i))
		agent.TransportConfig = transport.TransportConfig{Kind: "http", URL: addr}
		s.Register(ctx, agent)
	}
	suspended := testAgent("agent-3", "agent3")
	suspended.Status = StatusSuspended
	s.Register(ctx, suspended)

	hc := NewHealthChecker(s, WithHealthTimeout(5*time.Second))
	err = hc.CheckAll(ctx)
	if err != nil {
		t.Fatalf("CheckAll: %v", err)
	}

	// Verify all active agents have last_seen_at set.
	for i := 0; i < 3; i++ {
		got, _ := s.Get(ctx, fmt.Sprintf("agent-%d", i))
		if got.LastSeenAt == nil {
			t.Errorf("agent-%d: LastSeenAt should be set", i)
		}
	}
}

func TestHealthCheckerOptions(t *testing.T) {
	t.Helper()
	s := newTestStore(t)

	hc := NewHealthChecker(s,
		WithHealthTimeout(30*time.Second),
		WithHealthLogger(nil), // nil logger test
	)
	if hc.timeout != 30*time.Second {
		t.Errorf("timeout=%v, want 30s", hc.timeout)
	}
}

func TestHealthCheckPingError(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Server that returns a JSON-RPC error.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonrpc.Request
			json.NewDecoder(r.Body).Decode(&req)
			resp := jsonrpc.NewErrorResponse(req.ID, &jsonrpc.ErrorObject{
				Code:    -32603,
				Message: "internal error",
			})
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}),
	}
	go srv.Serve(ln)
	defer srv.Shutdown(ctx)

	s := newTestStore(t)
	agent := testAgent("agent-1", "alpha")
	agent.TransportConfig = transport.TransportConfig{
		Kind: "http",
		URL:  "http://" + ln.Addr().String(),
	}
	s.Register(ctx, agent)

	hc := NewHealthChecker(s, WithHealthTimeout(5*time.Second))
	err = hc.Check(ctx, agent)
	if err == nil {
		t.Fatal("expected error for ping that returns error")
	}

	got, _ := s.Get(ctx, "agent-1")
	if got.LastError == "" {
		t.Error("LastError should be set")
	}
}
