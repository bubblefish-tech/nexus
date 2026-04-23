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

package jsonrpc

import (
	"encoding/json"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/a2a"
)

func TestID_MarshalString(t *testing.T) {
	t.Helper()
	id := StringID("abc-123")
	data, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"abc-123"` {
		t.Fatalf("got %s, want %q", data, "abc-123")
	}
}

func TestID_MarshalNumber(t *testing.T) {
	t.Helper()
	id := NumberID(42)
	data, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "42" {
		t.Fatalf("got %s, want 42", data)
	}
}

func TestID_MarshalNull(t *testing.T) {
	t.Helper()
	id := NullID()
	data, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "null" {
		t.Fatalf("got %s, want null", data)
	}
}

func TestID_UnmarshalString(t *testing.T) {
	t.Helper()
	var id ID
	if err := json.Unmarshal([]byte(`"req-1"`), &id); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	s, ok := id.StringValue()
	if !ok || s != "req-1" {
		t.Fatalf("got (%q, %v), want (req-1, true)", s, ok)
	}
}

func TestID_UnmarshalNumber(t *testing.T) {
	t.Helper()
	var id ID
	if err := json.Unmarshal([]byte(`7`), &id); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	n, ok := id.NumberValue()
	if !ok || n != 7 {
		t.Fatalf("got (%d, %v), want (7, true)", n, ok)
	}
}

func TestID_UnmarshalNull(t *testing.T) {
	t.Helper()
	var id ID
	if err := json.Unmarshal([]byte(`null`), &id); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !id.IsNull {
		t.Fatal("expected null ID")
	}
}

func TestID_UnmarshalInvalid(t *testing.T) {
	t.Helper()
	var id ID
	if err := json.Unmarshal([]byte(`true`), &id); err == nil {
		t.Fatal("expected error for boolean id")
	}
}

func TestID_RoundtripString(t *testing.T) {
	t.Helper()
	orig := StringID("round-trip")
	data, _ := json.Marshal(orig)
	var got ID
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	s, ok := got.StringValue()
	if !ok || s != "round-trip" {
		t.Fatalf("roundtrip failed: got (%q, %v)", s, ok)
	}
}

func TestID_RoundtripNumber(t *testing.T) {
	t.Helper()
	orig := NumberID(999)
	data, _ := json.Marshal(orig)
	var got ID
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	n, ok := got.NumberValue()
	if !ok || n != 999 {
		t.Fatalf("roundtrip failed: got (%d, %v)", n, ok)
	}
}

func TestID_String(t *testing.T) {
	t.Helper()
	tests := []struct {
		id   ID
		want string
	}{
		{StringID("x"), "x"},
		{NumberID(5), "5"},
		{NullID(), "null"},
	}
	for _, tt := range tests {
		if got := tt.id.String(); got != tt.want {
			t.Errorf("ID.String() = %q, want %q", got, tt.want)
		}
	}
}

func TestNewRequest(t *testing.T) {
	t.Helper()
	req, err := NewRequest(StringID("r1"), "test/method", map[string]string{"key": "val"})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if req.JSONRPC != Version {
		t.Fatalf("version = %q, want %q", req.JSONRPC, Version)
	}
	if req.Method != "test/method" {
		t.Fatalf("method = %q", req.Method)
	}
	if req.Params == nil {
		t.Fatal("params is nil")
	}
}

func TestNewRequest_NilParams(t *testing.T) {
	t.Helper()
	req, err := NewRequest(NumberID(1), "foo", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if req.Params != nil {
		t.Fatalf("expected nil params, got %s", req.Params)
	}
}

func TestNewResponse(t *testing.T) {
	t.Helper()
	resp, err := NewResponse(StringID("r1"), "ok")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	if resp.JSONRPC != Version {
		t.Fatalf("version = %q", resp.JSONRPC)
	}
	if resp.Error != nil {
		t.Fatal("unexpected error field")
	}
	if resp.Result == nil {
		t.Fatal("result is nil")
	}
}

func TestNewErrorResponse(t *testing.T) {
	t.Helper()
	errObj := &ErrorObject{Code: -32600, Message: "bad"}
	resp := NewErrorResponse(NumberID(2), errObj)
	if resp.JSONRPC != Version {
		t.Fatalf("version = %q", resp.JSONRPC)
	}
	if resp.Error == nil {
		t.Fatal("error is nil")
	}
	if resp.Error.Code != -32600 {
		t.Fatalf("error code = %d", resp.Error.Code)
	}
	if resp.Result != nil {
		t.Fatal("unexpected result field")
	}
}

func TestNewNotification(t *testing.T) {
	t.Helper()
	n, err := NewNotification("event/update", map[string]int{"count": 1})
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	if n.JSONRPC != Version {
		t.Fatalf("version = %q", n.JSONRPC)
	}
	if n.Method != "event/update" {
		t.Fatalf("method = %q", n.Method)
	}
}

func TestNewNotification_NilParams(t *testing.T) {
	t.Helper()
	n, err := NewNotification("ping", nil)
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	if n.Params != nil {
		t.Fatalf("expected nil params, got %s", n.Params)
	}
}

func TestRequest_Roundtrip(t *testing.T) {
	t.Helper()
	req, _ := NewRequest(StringID("rt"), "do/thing", []int{1, 2})
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Request
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.JSONRPC != Version || got.Method != "do/thing" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	s, ok := got.ID.StringValue()
	if !ok || s != "rt" {
		t.Fatalf("id roundtrip failed")
	}
}

func TestResponse_Roundtrip(t *testing.T) {
	t.Helper()
	resp, _ := NewResponse(NumberID(10), map[string]bool{"ok": true})
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Response
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	n, ok := got.ID.NumberValue()
	if !ok || n != 10 {
		t.Fatalf("id roundtrip failed")
	}
}

func TestErrorObject_Serialization(t *testing.T) {
	t.Helper()
	e := &ErrorObject{Code: -32601, Message: "not found", Data: "extra"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ErrorObject
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Code != -32601 || got.Message != "not found" {
		t.Fatalf("roundtrip failed: %+v", got)
	}
}

func TestErrorObject_NoData(t *testing.T) {
	t.Helper()
	e := &ErrorObject{Code: -32600, Message: "invalid"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// "data" should be omitted
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal map: %v", err)
	}
	if _, ok := m["data"]; ok {
		t.Fatal("data field should be omitted when nil")
	}
}

func TestFromA2AError(t *testing.T) {
	t.Helper()
	ae := a2a.NewErrorWithData(a2a.CodeTaskNotFound, "task gone", "trace-id-1")
	obj := FromA2AError(ae)
	if obj.Code != a2a.CodeTaskNotFound {
		t.Fatalf("code = %d", obj.Code)
	}
	if obj.Message != "task gone" {
		t.Fatalf("message = %q", obj.Message)
	}
	if obj.Data != "trace-id-1" {
		t.Fatalf("data = %v", obj.Data)
	}
}

func TestFromA2AError_Nil(t *testing.T) {
	t.Helper()
	if got := FromA2AError(nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestNotification_Roundtrip(t *testing.T) {
	t.Helper()
	n, _ := NewNotification("notify/test", "hello")
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Notification
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Method != "notify/test" || got.JSONRPC != Version {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestVersionConstant(t *testing.T) {
	t.Helper()
	if Version != "2.0" {
		t.Fatalf("Version = %q, want 2.0", Version)
	}
}

func TestErrorObject_ErrorInterface(t *testing.T) {
	t.Helper()
	e := &ErrorObject{Code: -32600, Message: "bad request"}
	s := e.Error()
	if s == "" {
		t.Fatal("Error() returned empty string")
	}
}
