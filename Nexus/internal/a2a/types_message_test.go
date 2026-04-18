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

func TestTextPartRoundtrip(t *testing.T) {
	t.Helper()
	p := NewTextPart("hello world")
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TextPart
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != "text" {
		t.Errorf("kind = %q, want %q", got.Kind, "text")
	}
	if got.Text != "hello world" {
		t.Errorf("text = %q, want %q", got.Text, "hello world")
	}
}

func TestFilePartRoundtrip(t *testing.T) {
	b := "aGVsbG8="
	p := NewFilePart(FileRef{Name: "test.txt", MimeType: "text/plain", Bytes: &b})
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got FilePart
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != "file" {
		t.Errorf("kind = %q, want %q", got.Kind, "file")
	}
	if got.File.Name != "test.txt" {
		t.Errorf("name = %q, want %q", got.File.Name, "test.txt")
	}
	if got.File.Bytes == nil || *got.File.Bytes != b {
		t.Error("bytes mismatch")
	}
}

func TestFilePartWithURI(t *testing.T) {
	uri := "https://example.com/file.pdf"
	p := NewFilePart(FileRef{Name: "file.pdf", MimeType: "application/pdf", URI: &uri})
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"uri":"https://example.com/file.pdf"`) {
		t.Errorf("expected uri in JSON: %s", data)
	}
	var got FilePart
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.File.URI == nil || *got.File.URI != uri {
		t.Error("uri mismatch")
	}
}

func TestDataPartRoundtrip(t *testing.T) {
	raw := json.RawMessage(`{"key":"value","num":42}`)
	p := NewDataPart(raw)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got DataPart
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != "data" {
		t.Errorf("kind = %q, want %q", got.Kind, "data")
	}
	var m map[string]interface{}
	if err := json.Unmarshal(got.Data, &m); err != nil {
		t.Fatalf("data unmarshal: %v", err)
	}
	if m["key"] != "value" {
		t.Errorf("data key = %v, want value", m["key"])
	}
}

func TestPartWrapperPolymorphicText(t *testing.T) {
	input := `{"kind":"text","text":"hello"}`
	var pw PartWrapper
	if err := json.Unmarshal([]byte(input), &pw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tp, ok := pw.Part.(TextPart)
	if !ok {
		t.Fatalf("expected TextPart, got %T", pw.Part)
	}
	if tp.Text != "hello" {
		t.Errorf("text = %q, want %q", tp.Text, "hello")
	}
}

func TestPartWrapperPolymorphicFile(t *testing.T) {
	input := `{"kind":"file","file":{"name":"f.txt","mimeType":"text/plain","bytes":"abc","uri":null}}`
	var pw PartWrapper
	if err := json.Unmarshal([]byte(input), &pw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	fp, ok := pw.Part.(FilePart)
	if !ok {
		t.Fatalf("expected FilePart, got %T", pw.Part)
	}
	if fp.File.Name != "f.txt" {
		t.Errorf("name = %q, want %q", fp.File.Name, "f.txt")
	}
}

func TestPartWrapperPolymorphicData(t *testing.T) {
	input := `{"kind":"data","data":{"x":1}}`
	var pw PartWrapper
	if err := json.Unmarshal([]byte(input), &pw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	dp, ok := pw.Part.(DataPart)
	if !ok {
		t.Fatalf("expected DataPart, got %T", pw.Part)
	}
	if string(dp.Data) != `{"x":1}` {
		t.Errorf("data = %s, want {\"x\":1}", dp.Data)
	}
}

func TestPartWrapperUnknownKind(t *testing.T) {
	input := `{"kind":"video","url":"x"}`
	var pw PartWrapper
	err := json.Unmarshal([]byte(input), &pw)
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if !strings.Contains(err.Error(), "unknown part kind") {
		t.Errorf("error = %v, want 'unknown part kind'", err)
	}
}

func TestPartWrapperInvalidJSON(t *testing.T) {
	var pw PartWrapper
	err := json.Unmarshal([]byte(`not json`), &pw)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestPartWrapperMarshalRoundtrip(t *testing.T) {
	pw := PartWrapper{Part: NewTextPart("round trip")}
	data, err := json.Marshal(pw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var pw2 PartWrapper
	if err := json.Unmarshal(data, &pw2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tp, ok := pw2.Part.(TextPart)
	if !ok {
		t.Fatalf("expected TextPart, got %T", pw2.Part)
	}
	if tp.Text != "round trip" {
		t.Errorf("text = %q, want %q", tp.Text, "round trip")
	}
}

func TestMessageRoundtrip(t *testing.T) {
	msg := NewMessage(RoleUser, NewTextPart("hello"))
	msg.ContextID = NewContextID()

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Kind != "message" {
		t.Errorf("kind = %q, want %q", got.Kind, "message")
	}
	if got.MessageID != msg.MessageID {
		t.Errorf("messageId mismatch: %q vs %q", got.MessageID, msg.MessageID)
	}
	if got.Role != RoleUser {
		t.Errorf("role = %q, want %q", got.Role, RoleUser)
	}
	if got.ContextID != msg.ContextID {
		t.Errorf("contextId mismatch: %q vs %q", got.ContextID, msg.ContextID)
	}
	if len(got.Parts) != 1 {
		t.Fatalf("parts len = %d, want 1", len(got.Parts))
	}
}

func TestMessageWithMetadata(t *testing.T) {
	msg := NewMessage(RoleAgent, NewTextPart("hi"))
	msg.Metadata = json.RawMessage(`{"source":"test"}`)
	msg.Extensions = json.RawMessage(`{"ext/v1":{"foo":"bar"}}`)

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(got.Metadata) != `{"source":"test"}` {
		t.Errorf("metadata = %s", got.Metadata)
	}
	if string(got.Extensions) != `{"ext/v1":{"foo":"bar"}}` {
		t.Errorf("extensions = %s", got.Extensions)
	}
}

func TestMessageMultipleParts(t *testing.T) {
	b := "data=="
	msg := NewMessage(RoleUser,
		NewTextPart("see attached"),
		NewFilePart(FileRef{Name: "doc.pdf", Bytes: &b}),
		NewDataPart(json.RawMessage(`{"action":"review"}`)),
	)
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Parts) != 3 {
		t.Fatalf("parts len = %d, want 3", len(got.Parts))
	}
	kinds := []string{"text", "file", "data"}
	for i, pw := range got.Parts {
		if pw.Part.PartKind() != kinds[i] {
			t.Errorf("parts[%d].PartKind() = %q, want %q", i, pw.Part.PartKind(), kinds[i])
		}
	}
}

func TestMessageJSONFieldNames(t *testing.T) {
	msg := NewMessage(RoleUser, NewTextPart("test"))
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	for _, field := range []string{`"kind"`, `"messageId"`, `"role"`, `"parts"`} {
		if !strings.Contains(s, field) {
			t.Errorf("expected field %s in JSON: %s", field, s)
		}
	}
}

func TestRoleValues(t *testing.T) {
	if !ValidRole(RoleUser) {
		t.Error("user should be valid")
	}
	if !ValidRole(RoleAgent) {
		t.Error("agent should be valid")
	}
	if ValidRole("system") {
		t.Error("system should be invalid")
	}
	if ValidRole("") {
		t.Error("empty should be invalid")
	}
}

func TestTextPartMetadata(t *testing.T) {
	p := NewTextPart("hi")
	p.Metadata = json.RawMessage(`{"lang":"en"}`)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TextPart
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(got.Metadata) != `{"lang":"en"}` {
		t.Errorf("metadata = %s", got.Metadata)
	}
}

func TestFileRefNullFields(t *testing.T) {
	// Both bytes and uri null
	input := `{"kind":"file","file":{"name":"x","mimeType":"text/plain","bytes":null,"uri":null}}`
	var pw PartWrapper
	if err := json.Unmarshal([]byte(input), &pw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	fp := pw.Part.(FilePart)
	if fp.File.Bytes != nil {
		t.Error("bytes should be nil")
	}
	if fp.File.URI != nil {
		t.Error("uri should be nil")
	}
}
