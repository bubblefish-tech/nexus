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
	"fmt"
)

// Role represents the sender of a Message.
type Role string

const (
	RoleUser  Role = "user"
	RoleAgent Role = "agent"
)

// ValidRole returns true if r is a valid Role value.
func ValidRole(r Role) bool {
	return r == RoleUser || r == RoleAgent
}

// Part is the interface satisfied by all message part types.
type Part interface {
	// PartKind returns the discriminator string for JSON serialization.
	PartKind() string
}

// TextPart is a plain-text message part.
type TextPart struct {
	Kind     string          `json:"kind"`
	Text     string          `json:"text"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// PartKind implements Part.
func (p TextPart) PartKind() string { return "text" }

// FilePart is a file-bearing message part.
type FilePart struct {
	Kind     string          `json:"kind"`
	File     FileRef         `json:"file"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// PartKind implements Part.
func (p FilePart) PartKind() string { return "file" }

// FileRef describes a file by name, MIME type, and either inline bytes or URI.
type FileRef struct {
	Name     string  `json:"name,omitempty"`
	MimeType string  `json:"mimeType,omitempty"`
	Bytes    *string `json:"bytes"`
	URI      *string `json:"uri"`
}

// DataPart is a structured-data message part.
type DataPart struct {
	Kind     string          `json:"kind"`
	Data     json.RawMessage `json:"data"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// PartKind implements Part.
func (p DataPart) PartKind() string { return "data" }

// PartWrapper is used for polymorphic JSON deserialization of Part values.
type PartWrapper struct {
	Part Part
}

// MarshalJSON delegates to the wrapped Part.
func (pw PartWrapper) MarshalJSON() ([]byte, error) {
	return json.Marshal(pw.Part)
}

// UnmarshalJSON dispatches on the "kind" field.
func (pw *PartWrapper) UnmarshalJSON(data []byte) error {
	var probe struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("a2a: cannot determine part kind: %w", err)
	}
	switch probe.Kind {
	case "text":
		var p TextPart
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		pw.Part = p
	case "file":
		var p FilePart
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		pw.Part = p
	case "data":
		var p DataPart
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		pw.Part = p
	default:
		return fmt.Errorf("a2a: unknown part kind %q", probe.Kind)
	}
	return nil
}

// Message is the fundamental communication unit in the A2A protocol.
type Message struct {
	Kind       string          `json:"kind"`
	MessageID  string          `json:"messageId"`
	ContextID  string          `json:"contextId,omitempty"`
	Role       Role            `json:"role"`
	Parts      []PartWrapper   `json:"parts"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	Extensions json.RawMessage `json:"extensions,omitempty"`
}

// NewTextPart creates a TextPart with the kind field set.
func NewTextPart(text string) TextPart {
	return TextPart{Kind: "text", Text: text}
}

// NewFilePart creates a FilePart with the kind field set.
func NewFilePart(file FileRef) FilePart {
	return FilePart{Kind: "file", File: file}
}

// NewDataPart creates a DataPart with the kind field set.
func NewDataPart(data json.RawMessage) DataPart {
	return DataPart{Kind: "data", Data: data}
}

// NewMessage creates a Message with generated ID and kind set.
func NewMessage(role Role, parts ...Part) Message {
	wrappers := make([]PartWrapper, len(parts))
	for i, p := range parts {
		wrappers[i] = PartWrapper{Part: p}
	}
	return Message{
		Kind:      "message",
		MessageID: NewMessageID(),
		Role:      role,
		Parts:     wrappers,
	}
}
