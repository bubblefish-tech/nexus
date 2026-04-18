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
	"strings"
	"testing"
)

func TestValidateMessageValid(t *testing.T) {
	msg := NewMessage(RoleUser, NewTextPart("hello"))
	if err := msg.Validate(); err != nil {
		t.Errorf("valid message rejected: %v", err)
	}
}

func TestValidateMessageWithContext(t *testing.T) {
	msg := NewMessage(RoleAgent, NewTextPart("reply"))
	msg.ContextID = NewContextID()
	if err := msg.Validate(); err != nil {
		t.Errorf("valid message with context rejected: %v", err)
	}
}

func TestValidateMessageErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Message)
		wantMsg string
	}{
		{
			name:    "wrong kind",
			mutate:  func(m *Message) { m.Kind = "task" },
			wantMsg: "kind must be",
		},
		{
			name:    "empty messageId",
			mutate:  func(m *Message) { m.MessageID = "" },
			wantMsg: "messageId is required",
		},
		{
			name:    "invalid messageId",
			mutate:  func(m *Message) { m.MessageID = "bad_id" },
			wantMsg: "invalid ID",
		},
		{
			name:    "invalid role",
			mutate:  func(m *Message) { m.Role = "system" },
			wantMsg: "invalid role",
		},
		{
			name:    "no parts",
			mutate:  func(m *Message) { m.Parts = nil },
			wantMsg: "at least one part",
		},
		{
			name:    "nil part",
			mutate:  func(m *Message) { m.Parts = []PartWrapper{{Part: nil}} },
			wantMsg: "is nil",
		},
		{
			name:    "invalid contextId",
			mutate:  func(m *Message) { m.ContextID = "bad" },
			wantMsg: "invalid ID",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := NewMessage(RoleUser, NewTextPart("hi"))
			tt.mutate(&msg)
			err := msg.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Message, tt.wantMsg) {
				t.Errorf("error = %q, want substring %q", err.Message, tt.wantMsg)
			}
			if err.Code != CodeInvalidParams {
				t.Errorf("code = %d, want %d", err.Code, CodeInvalidParams)
			}
		})
	}
}

func TestValidatePartText(t *testing.T) {
	tests := []struct {
		name    string
		part    Part
		wantErr bool
	}{
		{"valid text", NewTextPart("hello"), false},
		{"empty text", TextPart{Kind: "text", Text: ""}, true},
		{"wrong kind", TextPart{Kind: "wrong", Text: "hi"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePart(tt.part)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePart() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePartFile(t *testing.T) {
	b := "data"
	uri := "https://example.com/f"
	tests := []struct {
		name    string
		part    Part
		wantErr bool
	}{
		{"with bytes", FilePart{Kind: "file", File: FileRef{Bytes: &b}}, false},
		{"with uri", FilePart{Kind: "file", File: FileRef{URI: &uri}}, false},
		{"no bytes or uri", FilePart{Kind: "file", File: FileRef{}}, true},
		{"wrong kind", FilePart{Kind: "text", File: FileRef{Bytes: &b}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePart(tt.part)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePart() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePartData(t *testing.T) {
	tests := []struct {
		name    string
		part    Part
		wantErr bool
	}{
		{"valid data", NewDataPart(json.RawMessage(`{"x":1}`)), false},
		{"empty data", DataPart{Kind: "data", Data: nil}, true},
		{"wrong kind", DataPart{Kind: "text", Data: json.RawMessage(`{}`)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePart(tt.part)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePart() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTaskValid(t *testing.T) {
	task := NewTask()
	if err := task.Validate(); err != nil {
		t.Errorf("valid task rejected: %v", err)
	}
}

func TestValidateTaskErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Task)
		wantMsg string
	}{
		{
			name:    "wrong kind",
			mutate:  func(tk *Task) { tk.Kind = "message" },
			wantMsg: "kind must be",
		},
		{
			name:    "empty taskId",
			mutate:  func(tk *Task) { tk.TaskID = "" },
			wantMsg: "taskId is required",
		},
		{
			name:    "invalid taskId",
			mutate:  func(tk *Task) { tk.TaskID = "bad" },
			wantMsg: "invalid ID",
		},
		{
			name:    "invalid state",
			mutate:  func(tk *Task) { tk.Status.State = "running" },
			wantMsg: "invalid status.state",
		},
		{
			name:    "empty timestamp",
			mutate:  func(tk *Task) { tk.Status.Timestamp = "" },
			wantMsg: "timestamp is required",
		},
		{
			name:    "bad timestamp",
			mutate:  func(tk *Task) { tk.Status.Timestamp = "not-a-time" },
			wantMsg: "invalid status.timestamp",
		},
		{
			name:    "invalid contextId",
			mutate:  func(tk *Task) { tk.ContextID = "bad" },
			wantMsg: "invalid ID",
		},
		{
			name: "invalid artifact",
			mutate: func(tk *Task) {
				tk.Artifacts = []Artifact{{ArtifactID: ""}}
			},
			wantMsg: "artifactId is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := NewTask()
			tt.mutate(&task)
			err := task.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Message, tt.wantMsg) {
				t.Errorf("error = %q, want substring %q", err.Message, tt.wantMsg)
			}
		})
	}
}

func TestValidateArtifactValid(t *testing.T) {
	art := NewArtifact("result", NewTextPart("done"))
	if err := art.Validate(); err != nil {
		t.Errorf("valid artifact rejected: %v", err)
	}
}

func TestValidateArtifactErrors(t *testing.T) {
	tests := []struct {
		name    string
		art     Artifact
		wantMsg string
	}{
		{"empty id", Artifact{ArtifactID: ""}, "artifactId is required"},
		{"invalid id", Artifact{ArtifactID: "bad"}, "invalid ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.art.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Message, tt.wantMsg) {
				t.Errorf("error = %q, want substring %q", err.Message, tt.wantMsg)
			}
		})
	}
}

func TestValidateAgentCardValid(t *testing.T) {
	card := AgentCard{
		Name:            "test",
		URL:             "https://example.com",
		ProtocolVersion: ProtocolVersion,
		Endpoints: []Endpoint{
			{URL: "https://example.com/a2a", Transport: TransportHTTP},
		},
	}
	if err := card.Validate(); err != nil {
		t.Errorf("valid agent card rejected: %v", err)
	}
}

func TestValidateAgentCardErrors(t *testing.T) {
	base := func() AgentCard {
		return AgentCard{
			Name:            "test",
			URL:             "https://example.com",
			ProtocolVersion: ProtocolVersion,
			Endpoints:       []Endpoint{{URL: "https://example.com/a2a", Transport: TransportHTTP}},
		}
	}
	tests := []struct {
		name    string
		mutate  func(*AgentCard)
		wantMsg string
	}{
		{
			name:    "empty name",
			mutate:  func(ac *AgentCard) { ac.Name = "" },
			wantMsg: "name is required",
		},
		{
			name:    "whitespace name",
			mutate:  func(ac *AgentCard) { ac.Name = "   " },
			wantMsg: "name is required",
		},
		{
			name:    "empty url",
			mutate:  func(ac *AgentCard) { ac.URL = "" },
			wantMsg: "url is required",
		},
		{
			name:    "empty protocolVersion",
			mutate:  func(ac *AgentCard) { ac.ProtocolVersion = "" },
			wantMsg: "protocolVersion is required",
		},
		{
			name:    "no endpoints",
			mutate:  func(ac *AgentCard) { ac.Endpoints = nil },
			wantMsg: "at least one endpoint",
		},
		{
			name: "endpoint missing url",
			mutate: func(ac *AgentCard) {
				ac.Endpoints = []Endpoint{{URL: "", Transport: TransportHTTP}}
			},
			wantMsg: "url is required",
		},
		{
			name: "invalid transport",
			mutate: func(ac *AgentCard) {
				ac.Endpoints = []Endpoint{{URL: "https://x.com", Transport: "grpc"}}
			},
			wantMsg: "is invalid",
		},
		{
			name: "skill missing id",
			mutate: func(ac *AgentCard) {
				ac.Skills = []Skill{{ID: "", Name: "test"}}
			},
			wantMsg: "id is required",
		},
		{
			name: "skill missing name",
			mutate: func(ac *AgentCard) {
				ac.Skills = []Skill{{ID: "sk1", Name: ""}}
			},
			wantMsg: "name is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := base()
			tt.mutate(&card)
			err := card.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Message, tt.wantMsg) {
				t.Errorf("error = %q, want substring %q", err.Message, tt.wantMsg)
			}
		})
	}
}

func TestValidateGovernanceValid(t *testing.T) {
	gov := GovernanceExtension{
		SourceAgentID: "agent-a",
		TargetAgentID: "agent-b",
		Decision:      GovernanceAllow,
		ChainDepth:    1,
		MaxChainDepth: 5,
	}
	if err := gov.Validate(); err != nil {
		t.Errorf("valid governance rejected: %v", err)
	}
}

func TestValidateGovernanceErrors(t *testing.T) {
	base := func() GovernanceExtension {
		return GovernanceExtension{
			SourceAgentID: "a",
			TargetAgentID: "b",
			Decision:      GovernanceAllow,
		}
	}
	tests := []struct {
		name    string
		mutate  func(*GovernanceExtension)
		wantMsg string
	}{
		{
			name:    "missing source",
			mutate:  func(g *GovernanceExtension) { g.SourceAgentID = "" },
			wantMsg: "sourceAgentId is required",
		},
		{
			name:    "missing target",
			mutate:  func(g *GovernanceExtension) { g.TargetAgentID = "" },
			wantMsg: "targetAgentId is required",
		},
		{
			name:    "invalid decision",
			mutate:  func(g *GovernanceExtension) { g.Decision = "maybe" },
			wantMsg: "invalid decision",
		},
		{
			name:    "negative chain depth",
			mutate:  func(g *GovernanceExtension) { g.ChainDepth = -1 },
			wantMsg: "chainDepth must be >= 0",
		},
		{
			name:    "negative max chain depth",
			mutate:  func(g *GovernanceExtension) { g.MaxChainDepth = -1 },
			wantMsg: "maxChainDepth must be >= 0",
		},
		{
			name: "chain exceeds max",
			mutate: func(g *GovernanceExtension) {
				g.ChainDepth = 6
				g.MaxChainDepth = 5
			},
			wantMsg: "exceeds maxChainDepth",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gov := base()
			tt.mutate(&gov)
			err := gov.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Message, tt.wantMsg) {
				t.Errorf("error = %q, want substring %q", err.Message, tt.wantMsg)
			}
		})
	}
}

func TestGovernanceDecisionValues(t *testing.T) {
	if !ValidGovernanceDecision(GovernanceAllow) {
		t.Error("allow should be valid")
	}
	if !ValidGovernanceDecision(GovernanceDeny) {
		t.Error("deny should be valid")
	}
	if !ValidGovernanceDecision(GovernanceEscalate) {
		t.Error("escalate should be valid")
	}
	if ValidGovernanceDecision("maybe") {
		t.Error("maybe should be invalid")
	}
}
