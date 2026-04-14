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

package a2a

import (
	"encoding/json"
	"testing"
)

func TestAgentCardRoundtrip(t *testing.T) {
	card := AgentCard{
		Name:            "test-agent",
		Description:     "A test agent",
		URL:             "https://agent.example.com",
		Version:         "1.0.0",
		ProtocolVersion: ProtocolVersion,
		Implementation:  ImplementationName,
		Endpoints: []Endpoint{
			{URL: "https://agent.example.com/a2a", Transport: TransportHTTP},
			{URL: "https://agent.example.com/sse", Transport: TransportSSE},
		},
		Capabilities: AgentCapabilities{
			Streaming:        true,
			PushNotifications: false,
			StateTransitions: true,
		},
		Skills: []Skill{
			{
				ID:                   "memory-read",
				Name:                 "Read Memory",
				Description:          "Reads stored memories",
				Tags:                 []string{"memory", "read"},
				RequiredCapabilities: []string{CapMemoryRead},
				Destructive:          false,
				InputSchema:          json.RawMessage(`{"type":"object"}`),
				OutputSchema:         json.RawMessage(`{"type":"string"}`),
			},
		},
		Extensions: []ExtensionDecl{
			{URI: GovernanceExtensionURI, Required: true},
		},
		PublicKeys: []PublicKeyJWK{
			{Kty: "EC", Crv: "P-256", X: "abc", Y: "def", Kid: "key-1"},
		},
	}

	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got AgentCard
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Name != "test-agent" {
		t.Errorf("name = %q", got.Name)
	}
	if got.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocolVersion = %q", got.ProtocolVersion)
	}
	if len(got.Endpoints) != 2 {
		t.Fatalf("endpoints len = %d, want 2", len(got.Endpoints))
	}
	if got.Endpoints[0].Transport != TransportHTTP {
		t.Errorf("transport = %q, want %q", got.Endpoints[0].Transport, TransportHTTP)
	}
	if !got.Capabilities.Streaming {
		t.Error("streaming should be true")
	}
	if len(got.Skills) != 1 {
		t.Fatalf("skills len = %d, want 1", len(got.Skills))
	}
	if got.Skills[0].ID != "memory-read" {
		t.Errorf("skill id = %q", got.Skills[0].ID)
	}
	if len(got.Skills[0].RequiredCapabilities) != 1 {
		t.Errorf("requiredCapabilities len = %d", len(got.Skills[0].RequiredCapabilities))
	}
	if len(got.Extensions) != 1 {
		t.Fatalf("extensions len = %d", len(got.Extensions))
	}
	if !got.Extensions[0].Required {
		t.Error("extension should be required")
	}
	if len(got.PublicKeys) != 1 {
		t.Fatalf("publicKeys len = %d", len(got.PublicKeys))
	}
	if got.PublicKeys[0].Kid != "key-1" {
		t.Errorf("kid = %q", got.PublicKeys[0].Kid)
	}
}

func TestTransportKindString(t *testing.T) {
	tests := []struct {
		kind TransportKind
		want string
	}{
		{TransportHTTP, "http"},
		{TransportJSONRPC, "jsonrpc"},
		{TransportSSE, "sse"},
		{TransportStdio, "stdio"},
		{TransportTunnel, "tunnel"},
		{TransportWSL, "wsl"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidTransportKind(t *testing.T) {
	for _, tk := range AllTransportKinds() {
		if !ValidTransportKind(tk) {
			t.Errorf("ValidTransportKind(%q) = false", tk)
		}
	}
	if ValidTransportKind("grpc") {
		t.Error("grpc should be invalid")
	}
}

func TestAllTransportKindsCount(t *testing.T) {
	kinds := AllTransportKinds()
	if len(kinds) != 6 {
		t.Errorf("expected 6 transport kinds, got %d", len(kinds))
	}
}

func TestAgentCardWithAuth(t *testing.T) {
	card := AgentCard{
		Name:            "auth-agent",
		URL:             "https://a.example.com",
		ProtocolVersion: ProtocolVersion,
		Endpoints:       []Endpoint{{URL: "https://a.example.com/a2a", Transport: TransportHTTP}},
		Auth: &AuthConfig{
			Schemes:     []string{"bearer", "oauth2"},
			Credentials: json.RawMessage(`{"tokenUrl":"https://auth.example.com/token"}`),
		},
	}
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AgentCard
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Auth == nil {
		t.Fatal("auth should not be nil")
	}
	if len(got.Auth.Schemes) != 2 {
		t.Errorf("schemes len = %d", len(got.Auth.Schemes))
	}
}

func TestAgentCardSignature(t *testing.T) {
	card := AgentCard{
		Name:            "signed-agent",
		URL:             "https://s.example.com",
		ProtocolVersion: ProtocolVersion,
		Endpoints:       []Endpoint{{URL: "https://s.example.com/a2a", Transport: TransportHTTP}},
		Signature: &CardSignature{
			Algorithm: "Ed25519",
			KeyID:     "key-1",
			Value:     "base64signature==",
		},
	}
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AgentCard
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Signature == nil {
		t.Fatal("signature should not be nil")
	}
	if got.Signature.Algorithm != "Ed25519" {
		t.Errorf("algorithm = %q", got.Signature.Algorithm)
	}
}

func TestSkillWithSchemas(t *testing.T) {
	skill := Skill{
		ID:           "complex-skill",
		Name:         "Complex Skill",
		Description:  "Does complex things",
		Tags:         []string{"complex", "skill"},
		Destructive:  true,
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		OutputSchema: json.RawMessage(`{"type":"array","items":{"type":"string"}}`),
	}
	data, err := json.Marshal(skill)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Skill
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Destructive {
		t.Error("destructive should be true")
	}
	if got.InputSchema == nil {
		t.Error("inputSchema should not be nil")
	}
}

func TestEndpointRoundtrip(t *testing.T) {
	ep := Endpoint{URL: "stdio://local", Transport: TransportStdio}
	data, err := json.Marshal(ep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Endpoint
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.URL != "stdio://local" {
		t.Errorf("url = %q", got.URL)
	}
	if got.Transport != TransportStdio {
		t.Errorf("transport = %q", got.Transport)
	}
}
