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

package registry_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/a2a/transport"
	"github.com/bubblefish-tech/nexus/internal/crypto"
)

func newEncMKM(t *testing.T, password string) *crypto.MasterKeyManager {
	t.Helper()
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager(password, saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	return mkm
}

func newEncStore(t *testing.T) *registry.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test_registry.db")
	s, err := registry.NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func testEncAgent(id, name string) registry.RegisteredAgent {
	return registry.RegisteredAgent{
		AgentID:     id,
		Name:        name,
		DisplayName: "Enc Agent " + name,
		AgentCard: a2a.AgentCard{
			Name:            name,
			URL:             "http://localhost:9090",
			ProtocolVersion: "0.1",
			Endpoints: []a2a.Endpoint{
				{URL: "http://localhost:9090/a2a", Transport: a2a.TransportHTTP},
			},
		},
		TransportConfig: transport.TransportConfig{
			Kind: "http",
			URL:  "http://localhost:9090",
		},
		Status:    registry.StatusActive,
		LastError: "",
	}
}

func TestRegistryEncryption_RoundTrip(t *testing.T) {
	s := newEncStore(t)
	s.SetEncryption(newEncMKM(t, "testpw"))
	ctx := context.Background()

	agent := testEncAgent("enc-agt-1", "enc-alpha")
	agent.LastError = "transient connect error"
	if err := s.Register(ctx, agent); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := s.Get(ctx, "enc-agt-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentCard.Name != agent.AgentCard.Name {
		t.Errorf("AgentCard.Name: got %q, want %q", got.AgentCard.Name, agent.AgentCard.Name)
	}
	if got.AgentCard.URL != agent.AgentCard.URL {
		t.Errorf("AgentCard.URL: got %q, want %q", got.AgentCard.URL, agent.AgentCard.URL)
	}
	if got.TransportConfig.Kind != agent.TransportConfig.Kind {
		t.Errorf("TransportConfig.Kind: got %q, want %q", got.TransportConfig.Kind, agent.TransportConfig.Kind)
	}
	if got.TransportConfig.URL != agent.TransportConfig.URL {
		t.Errorf("TransportConfig.URL: got %q, want %q", got.TransportConfig.URL, agent.TransportConfig.URL)
	}
	if got.LastError != agent.LastError {
		t.Errorf("LastError: got %q, want %q", got.LastError, agent.LastError)
	}
}

func TestRegistryEncryption_PlaintextColumnsEmpty(t *testing.T) {
	s := newEncStore(t)
	s.SetEncryption(newEncMKM(t, "pw"))
	ctx := context.Background()

	agent := testEncAgent("enc-agt-2", "enc-beta")
	if err := s.Register(ctx, agent); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var rawCard, rawTransport string
	err := s.DB().QueryRowContext(ctx,
		`SELECT agent_card_json, transport_toml FROM a2a_agents WHERE agent_id = ?`,
		"enc-agt-2",
	).Scan(&rawCard, &rawTransport)
	if err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if rawCard != "{}" {
		t.Errorf("plaintext agent_card_json should be placeholder '{}', got %q", rawCard)
	}
	if rawTransport != "" {
		t.Errorf("plaintext transport_toml should be empty, got %q", rawTransport)
	}
}

func TestRegistryEncryption_WrongKeyFails(t *testing.T) {
	s := newEncStore(t)
	s.SetEncryption(newEncMKM(t, "key-A"))
	ctx := context.Background()

	if err := s.Register(ctx, testEncAgent("enc-agt-3", "enc-gamma")); err != nil {
		t.Fatalf("Register: %v", err)
	}

	sB := registry.NewStoreFromDB(s.DB())
	sB.SetEncryption(newEncMKM(t, "key-B"))
	_, err := sB.Get(ctx, "enc-agt-3")
	if err == nil {
		t.Fatal("expected decrypt error with wrong key, got nil")
	}
}

func TestRegistryEncryption_BackwardCompat(t *testing.T) {
	s := newEncStore(t)
	ctx := context.Background()

	// Register without encryption (legacy plaintext row).
	agent := testEncAgent("enc-agt-4", "enc-delta")
	if err := s.Register(ctx, agent); err != nil {
		t.Fatalf("Register (plaintext): %v", err)
	}

	// Encrypted store must still read old plaintext rows transparently.
	s.SetEncryption(newEncMKM(t, "pw"))
	got, err := s.Get(ctx, "enc-agt-4")
	if err != nil {
		t.Fatalf("Get (encrypted store, old row): %v", err)
	}
	if got.AgentCard.Name != agent.AgentCard.Name {
		t.Errorf("backward compat: AgentCard.Name: got %q, want %q", got.AgentCard.Name, agent.AgentCard.Name)
	}
	if got.TransportConfig.Kind != agent.TransportConfig.Kind {
		t.Errorf("backward compat: TransportConfig.Kind: got %q, want %q", got.TransportConfig.Kind, agent.TransportConfig.Kind)
	}
}

func TestRegistryEncryption_UpdateLastSeen_Encrypted(t *testing.T) {
	s := newEncStore(t)
	s.SetEncryption(newEncMKM(t, "pw"))
	ctx := context.Background()

	if err := s.Register(ctx, testEncAgent("enc-agt-5", "enc-epsilon")); err != nil {
		t.Fatalf("Register: %v", err)
	}

	wantErr := "connection refused: dial tcp"
	if err := s.UpdateLastSeen(ctx, "enc-agt-5", time.Now(), wantErr); err != nil {
		t.Fatalf("UpdateLastSeen: %v", err)
	}

	// Verify plaintext last_error is empty in DB.
	var rawLastError string
	if err := s.DB().QueryRowContext(ctx,
		`SELECT last_error FROM a2a_agents WHERE agent_id = ?`, "enc-agt-5",
	).Scan(&rawLastError); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if rawLastError != "" {
		t.Errorf("plaintext last_error should be empty after encryption, got %q", rawLastError)
	}

	// Verify decrypted value is restored on Get.
	got, err := s.Get(ctx, "enc-agt-5")
	if err != nil {
		t.Fatalf("Get after UpdateLastSeen: %v", err)
	}
	if got.LastError != wantErr {
		t.Errorf("LastError: got %q, want %q", got.LastError, wantErr)
	}
}

func TestRegistryEncryption_DifferentAgentsDifferentCiphertext(t *testing.T) {
	s := newEncStore(t)
	s.SetEncryption(newEncMKM(t, "pw"))
	ctx := context.Background()

	// Two agents with the same AgentCard name but different IDs.
	a1 := testEncAgent("enc-agt-6a", "same-name")
	a2 := testEncAgent("enc-agt-6b", "same-name")
	// Give a2 a unique name to satisfy UNIQUE constraint.
	a2.Name = "same-name-2"
	if err := s.Register(ctx, a1); err != nil {
		t.Fatalf("Register a1: %v", err)
	}
	if err := s.Register(ctx, a2); err != nil {
		t.Fatalf("Register a2: %v", err)
	}

	var blob1, blob2 []byte
	if err := s.DB().QueryRowContext(ctx,
		`SELECT agent_card_json_encrypted FROM a2a_agents WHERE agent_id = ?`, "enc-agt-6a",
	).Scan(&blob1); err != nil {
		t.Fatalf("raw query a1: %v", err)
	}
	if err := s.DB().QueryRowContext(ctx,
		`SELECT agent_card_json_encrypted FROM a2a_agents WHERE agent_id = ?`, "enc-agt-6b",
	).Scan(&blob2); err != nil {
		t.Fatalf("raw query a2: %v", err)
	}
	if string(blob1) == string(blob2) {
		t.Error("different agents produced identical ciphertext — per-row keys must differ")
	}
}

func TestRegistryEncryption_DisabledMKMNoOp(t *testing.T) {
	s := newEncStore(t)
	// Disabled MKM: empty password → IsEnabled() == false.
	mkm := newEncMKM(t, "")
	s.SetEncryption(mkm)
	ctx := context.Background()

	agent := testEncAgent("enc-agt-7", "enc-zeta")
	if err := s.Register(ctx, agent); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Plaintext columns must contain real data (not placeholders).
	var rawCard string
	if err := s.DB().QueryRowContext(ctx,
		`SELECT agent_card_json FROM a2a_agents WHERE agent_id = ?`, "enc-agt-7",
	).Scan(&rawCard); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if rawCard == "{}" {
		t.Error("disabled MKM should not encrypt: agent_card_json must contain real JSON, not placeholder '{}'")
	}

	got, err := s.Get(ctx, "enc-agt-7")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentCard.Name != agent.AgentCard.Name {
		t.Errorf("AgentCard.Name: got %q, want %q", got.AgentCard.Name, agent.AgentCard.Name)
	}
}

func TestRegistryEncryption_List_Encrypted(t *testing.T) {
	s := newEncStore(t)
	s.SetEncryption(newEncMKM(t, "pw"))
	ctx := context.Background()

	names := []string{"enc-list-1", "enc-list-2", "enc-list-3"}
	for i, name := range names {
		id := "enc-lst-" + name
		a := testEncAgent(id, name)
		_ = i
		if err := s.Register(ctx, a); err != nil {
			t.Fatalf("Register %s: %v", name, err)
		}
	}

	agents, err := s.List(ctx, registry.ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(agents) != len(names) {
		t.Fatalf("List: got %d agents, want %d", len(agents), len(names))
	}
	for _, a := range agents {
		if a.AgentCard.URL == "" {
			t.Errorf("List: agent %q has empty URL — decrypt may have failed", a.AgentID)
		}
	}
}
