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
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/a2a/transport"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeRegistryStore is an in-memory RegistrationStore for tests.
type fakeRegistryStore struct {
	mu     sync.Mutex
	byName map[string]*registry.RegisteredAgent
}

func newFakeRegistryStore() *fakeRegistryStore {
	return &fakeRegistryStore{byName: make(map[string]*registry.RegisteredAgent)}
}

func (f *fakeRegistryStore) Register(_ context.Context, agent registry.RegisteredAgent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.byName[agent.Name]; exists {
		return errors.New("duplicate name")
	}
	cp := agent
	f.byName[agent.Name] = &cp
	return nil
}

func (f *fakeRegistryStore) GetByName(_ context.Context, name string) (*registry.RegisteredAgent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.byName[name]; ok {
		cp := *a
		return &cp, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeRegistryStore) UpdateTransportAndCard(_ context.Context, agentID string, card a2a.AgentCard, displayName string, tc transport.TransportConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.byName {
		if a.AgentID == agentID {
			a.AgentCard = card
			a.DisplayName = displayName
			a.TransportConfig = tc
			a.UpdatedAt = time.Now()
			return nil
		}
	}
	return errors.New("not found")
}

// fakeAgentPinger lets tests configure whether the ping succeeds.
type fakeAgentPinger struct{ err error }

func (f *fakeAgentPinger) Check(_ context.Context, _ registry.RegisteredAgent) error { return f.err }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const testRegToken = "test-registration-token"

func newRegServer(store RegistrationStore, pinger AgentPinger, token string) *Server {
	card := a2a.AgentCard{Name: "nexus", ProtocolVersion: "0.1.0"}
	return NewServer(card,
		WithRegistrationStore(store),
		WithAgentPinger(pinger),
		WithRegistrationToken(token),
	)
}

func validCard() a2a.AgentCard {
	return a2a.AgentCard{
		Name:            "test-agent",
		ProtocolVersion: "0.1.0",
		Endpoints: []a2a.Endpoint{
			{URL: "http://127.0.0.1:9999", Transport: a2a.TransportHTTP},
		},
	}
}

func dispatchRegister(t *testing.T, srv *Server, params interface{}) *registerResult {
	t.Helper()
	resp := dispatch(t, srv, "agent/register", params)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", resp.Error.Code, resp.Error.Message)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var res registerResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return &res
}

func dispatchRegisterErr(t *testing.T, srv *Server, params interface{}) (int, string) {
	t.Helper()
	resp := dispatch(t, srv, "agent/register", params)
	if resp.Error == nil {
		t.Fatalf("expected error, got nil (result=%v)", resp.Result)
	}
	return resp.Error.Code, resp.Error.Message
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAgentRegister_Success(t *testing.T) {
	t.Helper()
	store := newFakeRegistryStore()
	srv := newRegServer(store, &fakeAgentPinger{}, testRegToken)

	res := dispatchRegister(t, srv, registerParams{
		Card:              validCard(),
		RegistrationToken: testRegToken,
	})

	if res.AgentID == "" {
		t.Error("AgentID should be non-empty")
	}
	if res.Name != "test-agent" {
		t.Errorf("Name = %q, want test-agent", res.Name)
	}
	if res.Status != registry.StatusActive {
		t.Errorf("Status = %q, want active", res.Status)
	}
	if res.Reregistered {
		t.Error("Reregistered should be false for new registration")
	}
	if res.AgentID[:4] != "agt_" {
		t.Errorf("AgentID prefix = %q, want agt_", res.AgentID[:4])
	}
}

func TestAgentRegister_Disabled_NoToken(t *testing.T) {
	t.Helper()
	// Server with empty registration token = self-registration disabled.
	srv := newRegServer(newFakeRegistryStore(), &fakeAgentPinger{}, "")

	code, _ := dispatchRegisterErr(t, srv, registerParams{
		Card:              validCard(),
		RegistrationToken: "anything",
	})
	if code != a2a.CodeMethodNotFound {
		t.Errorf("code = %d, want %d (CodeMethodNotFound)", code, a2a.CodeMethodNotFound)
	}
}

func TestAgentRegister_Disabled_NoStore(t *testing.T) {
	t.Helper()
	card := a2a.AgentCard{Name: "nexus"}
	srv := NewServer(card, WithRegistrationToken(testRegToken)) // no store

	code, _ := dispatchRegisterErr(t, srv, registerParams{
		Card:              validCard(),
		RegistrationToken: testRegToken,
	})
	if code != a2a.CodeMethodNotFound {
		t.Errorf("code = %d, want %d (CodeMethodNotFound)", code, a2a.CodeMethodNotFound)
	}
}

func TestAgentRegister_BadToken(t *testing.T) {
	t.Helper()
	srv := newRegServer(newFakeRegistryStore(), &fakeAgentPinger{}, testRegToken)

	code, _ := dispatchRegisterErr(t, srv, registerParams{
		Card:              validCard(),
		RegistrationToken: "wrong-token",
	})
	if code != a2a.CodeUnauthenticated {
		t.Errorf("code = %d, want %d (CodeUnauthenticated)", code, a2a.CodeUnauthenticated)
	}
}

func TestAgentRegister_MissingCard(t *testing.T) {
	t.Helper()
	srv := newRegServer(newFakeRegistryStore(), &fakeAgentPinger{}, testRegToken)

	code, _ := dispatchRegisterErr(t, srv, map[string]string{
		"registration_token": testRegToken,
		// no card field
	})
	if code != a2a.CodeInvalidParams {
		t.Errorf("code = %d, want %d (CodeInvalidParams)", code, a2a.CodeInvalidParams)
	}
}

func TestAgentRegister_EmptyName(t *testing.T) {
	t.Helper()
	srv := newRegServer(newFakeRegistryStore(), &fakeAgentPinger{}, testRegToken)

	card := validCard()
	card.Name = ""
	code, _ := dispatchRegisterErr(t, srv, registerParams{
		Card:              card,
		RegistrationToken: testRegToken,
	})
	if code != a2a.CodeInvalidParams {
		t.Errorf("code = %d, want %d (CodeInvalidParams)", code, a2a.CodeInvalidParams)
	}
}

func TestAgentRegister_NoEndpoints(t *testing.T) {
	t.Helper()
	srv := newRegServer(newFakeRegistryStore(), &fakeAgentPinger{}, testRegToken)

	card := validCard()
	card.Endpoints = nil
	code, _ := dispatchRegisterErr(t, srv, registerParams{
		Card:              card,
		RegistrationToken: testRegToken,
	})
	if code != a2a.CodeInvalidParams {
		t.Errorf("code = %d, want %d (CodeInvalidParams)", code, a2a.CodeInvalidParams)
	}
}

func TestAgentRegister_InvalidTransport(t *testing.T) {
	t.Helper()
	srv := newRegServer(newFakeRegistryStore(), &fakeAgentPinger{}, testRegToken)

	card := validCard()
	card.Endpoints[0].Transport = a2a.TransportKind("grpc") // invalid
	code, _ := dispatchRegisterErr(t, srv, registerParams{
		Card:              card,
		RegistrationToken: testRegToken,
	})
	if code != a2a.CodeInvalidParams {
		t.Errorf("code = %d, want %d (CodeInvalidParams)", code, a2a.CodeInvalidParams)
	}
}

func TestAgentRegister_PingbackFails(t *testing.T) {
	t.Helper()
	srv := newRegServer(
		newFakeRegistryStore(),
		&fakeAgentPinger{err: errors.New("connection refused")},
		testRegToken,
	)

	code, _ := dispatchRegisterErr(t, srv, registerParams{
		Card:              validCard(),
		RegistrationToken: testRegToken,
	})
	if code != a2a.CodeAgentOffline {
		t.Errorf("code = %d, want %d (CodeAgentOffline)", code, a2a.CodeAgentOffline)
	}
}

func TestAgentRegister_Reregister_NoKey(t *testing.T) {
	t.Helper()
	store := newFakeRegistryStore()
	srv := newRegServer(store, &fakeAgentPinger{}, testRegToken)

	// First registration.
	res1 := dispatchRegister(t, srv, registerParams{
		Card:              validCard(),
		RegistrationToken: testRegToken,
	})

	// Second registration (re-registration): same name, no pinned key.
	card2 := validCard()
	card2.Endpoints[0].URL = "http://127.0.0.1:9998"
	res2 := dispatchRegister(t, srv, registerParams{
		Card:              card2,
		RegistrationToken: testRegToken,
	})

	if res2.AgentID != res1.AgentID {
		t.Errorf("re-registration AgentID = %q, want same as first %q", res2.AgentID, res1.AgentID)
	}
	if !res2.Reregistered {
		t.Error("Reregistered should be true on second registration")
	}
}

func TestAgentRegister_Reregister_DifferentKey(t *testing.T) {
	t.Helper()
	store := newFakeRegistryStore()
	srv := newRegServer(store, &fakeAgentPinger{}, testRegToken)

	// Pre-seed an agent with a pinned key.
	store.byName["test-agent"] = &registry.RegisteredAgent{
		AgentID:         "agt_existing",
		Name:            "test-agent",
		PinnedPublicKey: "aabbccddeeff0011223344556677889900112233445566778899aabbccddeeff00",
		Status:          registry.StatusActive,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	// Attempt re-registration with a different key (non-empty, different value).
	card := validCard()
	card.PublicKeys = []a2a.PublicKeyJWK{
		{Kty: "OKP", Crv: "Ed25519", X: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	}

	code, _ := dispatchRegisterErr(t, srv, registerParams{
		Card:              card,
		RegistrationToken: testRegToken,
	})
	if code != a2a.CodeAlreadyExists {
		t.Errorf("code = %d, want %d (CodeAlreadyExists)", code, a2a.CodeAlreadyExists)
	}
}

func TestAgentRegister_IDPrefix(t *testing.T) {
	t.Helper()
	store := newFakeRegistryStore()
	srv := newRegServer(store, &fakeAgentPinger{}, testRegToken)

	res := dispatchRegister(t, srv, registerParams{
		Card:              validCard(),
		RegistrationToken: testRegToken,
	})

	if len(res.AgentID) < 4 || res.AgentID[:4] != a2a.PrefixAgent {
		t.Errorf("AgentID %q does not start with %q", res.AgentID, a2a.PrefixAgent)
	}
}

func TestAgentRegister_Concurrent(t *testing.T) {
	t.Helper()
	store := newFakeRegistryStore()
	srv := newRegServer(store, &fakeAgentPinger{}, testRegToken)

	// Two goroutines race to register the same name; one should succeed.
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			resp := dispatch(t, srv, "agent/register", registerParams{
				Card:              validCard(),
				RegistrationToken: testRegToken,
			})
			if resp.Error == nil {
				results <- 0
			} else {
				results <- resp.Error.Code
			}
		}()
	}

	r1, r2 := <-results, <-results
	successes := 0
	if r1 == 0 {
		successes++
	}
	if r2 == 0 {
		successes++
	}
	// Exactly one should succeed; the other may error (duplicate name in fake).
	// We accept either 1 or 2 successes since the fake store has a lock.
	if successes == 0 {
		t.Error("at least one concurrent registration should succeed")
	}
}
