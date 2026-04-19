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
	"context"
	"encoding/json"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/a2a"
)

// echoHandler returns the params as the result.
func echoHandler() HandlerFunc {
	return func(_ context.Context, _ string, params json.RawMessage) (interface{}, *ErrorObject) {
		return json.RawMessage(params), nil
	}
}

// errorHandler always returns an error.
func errorHandler(code int, msg string) HandlerFunc {
	return func(_ context.Context, _ string, _ json.RawMessage) (interface{}, *ErrorObject) {
		return nil, &ErrorObject{Code: code, Message: msg}
	}
}

func TestDispatch_RegisteredHandler(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	r.RegisterFunc("echo", echoHandler())

	req, _ := NewRequest(StringID("1"), "echo", map[string]string{"msg": "hi"})
	resp := r.Dispatch(context.Background(), req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("result is nil")
	}
	s, ok := resp.ID.StringValue()
	if !ok || s != "1" {
		t.Fatalf("ID mismatch: got %v", resp.ID)
	}
}

func TestDispatch_UnknownMethod(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	req, _ := NewRequest(StringID("2"), "nonexistent", nil)
	resp := r.Dispatch(context.Background(), req)

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != a2a.CodeMethodNotFound {
		t.Fatalf("error code = %d, want %d", resp.Error.Code, a2a.CodeMethodNotFound)
	}
}

func TestDispatch_HandlerReturnsError(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	r.RegisterFunc("fail", errorHandler(-32005, "bad input"))

	req, _ := NewRequest(NumberID(3), "fail", nil)
	resp := r.Dispatch(context.Background(), req)

	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != -32005 {
		t.Fatalf("error code = %d", resp.Error.Code)
	}
	n, ok := resp.ID.NumberValue()
	if !ok || n != 3 {
		t.Fatal("ID not preserved")
	}
}

func TestDispatch_HandlerReturnsResult(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	r.RegisterFunc("greet", func(_ context.Context, _ string, _ json.RawMessage) (interface{}, *ErrorObject) {
		return map[string]string{"greeting": "hello"}, nil
	})

	req, _ := NewRequest(StringID("g1"), "greet", nil)
	resp := r.Dispatch(context.Background(), req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["greeting"] != "hello" {
		t.Fatalf("result = %v", result)
	}
}

func TestDispatch_StringID(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	r.RegisterFunc("noop", func(_ context.Context, _ string, _ json.RawMessage) (interface{}, *ErrorObject) {
		return "ok", nil
	})

	req, _ := NewRequest(StringID("str-id"), "noop", nil)
	resp := r.Dispatch(context.Background(), req)
	s, ok := resp.ID.StringValue()
	if !ok || s != "str-id" {
		t.Fatalf("string ID not preserved: %v", resp.ID)
	}
}

func TestDispatch_NumberID(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	r.RegisterFunc("noop", func(_ context.Context, _ string, _ json.RawMessage) (interface{}, *ErrorObject) {
		return "ok", nil
	})

	req, _ := NewRequest(NumberID(42), "noop", nil)
	resp := r.Dispatch(context.Background(), req)
	n, ok := resp.ID.NumberValue()
	if !ok || n != 42 {
		t.Fatalf("number ID not preserved: %v", resp.ID)
	}
}

func TestDispatch_NullID(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	r.RegisterFunc("noop", func(_ context.Context, _ string, _ json.RawMessage) (interface{}, *ErrorObject) {
		return "ok", nil
	})

	req, _ := NewRequest(NullID(), "noop", nil)
	resp := r.Dispatch(context.Background(), req)
	if !resp.ID.IsNull {
		t.Fatalf("null ID not preserved: %v", resp.ID)
	}
}

func TestDispatchBatch_MultipleRequests(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	r.RegisterFunc("echo", echoHandler())

	reqs := make([]*Request, 3)
	for i := range reqs {
		reqs[i], _ = NewRequest(NumberID(int64(i)), "echo", map[string]int{"n": i})
	}

	resps := r.DispatchBatch(context.Background(), reqs)
	if len(resps) != 3 {
		t.Fatalf("got %d responses, want 3", len(resps))
	}
	for i, resp := range resps {
		if resp.Error != nil {
			t.Fatalf("response %d has error: %+v", i, resp.Error)
		}
		n, ok := resp.ID.NumberValue()
		if !ok || n != int64(i) {
			t.Fatalf("response %d ID mismatch: %v", i, resp.ID)
		}
	}
}

func TestDispatchBatch_MixedMethods(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	r.RegisterFunc("echo", echoHandler())
	// "unknown" is NOT registered.

	req1, _ := NewRequest(NumberID(1), "echo", "hello")
	req2, _ := NewRequest(NumberID(2), "unknown", nil)
	req3, _ := NewRequest(NumberID(3), "echo", "world")

	resps := r.DispatchBatch(context.Background(), []*Request{req1, req2, req3})
	if len(resps) != 3 {
		t.Fatalf("got %d responses", len(resps))
	}

	// req1: success
	if resps[0].Error != nil {
		t.Fatalf("resp 0 error: %+v", resps[0].Error)
	}
	// req2: method not found
	if resps[1].Error == nil || resps[1].Error.Code != a2a.CodeMethodNotFound {
		t.Fatalf("resp 1 should be method not found: %+v", resps[1].Error)
	}
	// req3: success
	if resps[2].Error != nil {
		t.Fatalf("resp 2 error: %+v", resps[2].Error)
	}
}

func TestDispatch_Register_Interface(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	var h Handler = HandlerFunc(func(_ context.Context, _ string, _ json.RawMessage) (interface{}, *ErrorObject) {
		return "via-interface", nil
	})
	r.Register("iface", h)

	req, _ := NewRequest(StringID("i1"), "iface", nil)
	resp := r.Dispatch(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestDispatchBatch_Empty(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	resps := r.DispatchBatch(context.Background(), nil)
	if len(resps) != 0 {
		t.Fatalf("expected empty responses, got %d", len(resps))
	}
}

// Test full decode -> dispatch -> encode roundtrip.
func TestDecode_Dispatch_Roundtrip(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	r.RegisterFunc("add", func(_ context.Context, _ string, params json.RawMessage) (interface{}, *ErrorObject) {
		var args struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, &ErrorObject{Code: a2a.CodeInvalidParams, Message: err.Error()}
		}
		return map[string]int{"sum": args.A + args.B}, nil
	})

	raw := []byte(`{"jsonrpc":"2.0","method":"add","params":{"a":2,"b":3},"id":"sum1"}`)
	msgs, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	req, ok := msgs[0].(*Request)
	if !ok {
		t.Fatalf("expected *Request, got %T", msgs[0])
	}

	resp := r.Dispatch(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("dispatch error: %+v", resp.Error)
	}

	var result map[string]int
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["sum"] != 5 {
		t.Fatalf("sum = %d, want 5", result["sum"])
	}
}

// Test batch decode -> dispatch -> encode roundtrip.
func TestBatchDecode_Dispatch_Roundtrip(t *testing.T) {
	t.Helper()
	r := NewMethodRouter()
	r.RegisterFunc("double", func(_ context.Context, _ string, params json.RawMessage) (interface{}, *ErrorObject) {
		var n int
		if err := json.Unmarshal(params, &n); err != nil {
			return nil, &ErrorObject{Code: a2a.CodeInvalidParams, Message: err.Error()}
		}
		return n * 2, nil
	})

	raw := []byte(`[
		{"jsonrpc":"2.0","method":"double","params":5,"id":1},
		{"jsonrpc":"2.0","method":"double","params":10,"id":2}
	]`)
	msgs, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	reqs := make([]*Request, len(msgs))
	for i, m := range msgs {
		req, ok := m.(*Request)
		if !ok {
			t.Fatalf("element %d: expected *Request, got %T", i, m)
		}
		reqs[i] = req
	}

	resps := r.DispatchBatch(context.Background(), reqs)
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}

	// Encode the batch response.
	batch := make([]interface{}, len(resps))
	for i, resp := range resps {
		batch[i] = resp
	}
	data, err := EncodeBatch(batch)
	if err != nil {
		t.Fatalf("encode batch: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty batch output")
	}
}

func TestDecode_Notification(t *testing.T) {
	t.Helper()
	raw := []byte(`{"jsonrpc":"2.0","method":"event/fire","params":{"x":1}}`)
	msgs, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1, got %d", len(msgs))
	}
	n, ok := msgs[0].(*Notification)
	if !ok {
		t.Fatalf("expected *Notification, got %T", msgs[0])
	}
	if n.Method != "event/fire" {
		t.Fatalf("method = %q", n.Method)
	}
}

func TestDecode_Response(t *testing.T) {
	t.Helper()
	raw := []byte(`{"jsonrpc":"2.0","result":"ok","id":"r1"}`)
	msgs, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp, ok := msgs[0].(*Response)
	if !ok {
		t.Fatalf("expected *Response, got %T", msgs[0])
	}
	s, ok := resp.ID.StringValue()
	if !ok || s != "r1" {
		t.Fatalf("id = %v", resp.ID)
	}
}

func TestDecode_ErrorResponse(t *testing.T) {
	t.Helper()
	raw := []byte(`{"jsonrpc":"2.0","error":{"code":-32601,"message":"not found"},"id":99}`)
	msgs, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp, ok := msgs[0].(*Response)
	if !ok {
		t.Fatalf("expected *Response, got %T", msgs[0])
	}
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("error = %+v", resp.Error)
	}
}

func TestDecode_EmptyInput(t *testing.T) {
	t.Helper()
	_, err := Decode([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestDecode_MalformedJSON(t *testing.T) {
	t.Helper()
	_, err := Decode([]byte(`{not valid json`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestDecode_EmptyBatch(t *testing.T) {
	t.Helper()
	_, err := Decode([]byte(`[]`))
	if err == nil {
		t.Fatal("expected error for empty batch")
	}
}

func TestDecode_WrongVersion(t *testing.T) {
	t.Helper()
	_, err := Decode([]byte(`{"jsonrpc":"1.0","method":"x","id":"1"}`))
	if err == nil {
		t.Fatal("expected error for wrong version")
	}
}

func TestDecode_BatchWithNotification(t *testing.T) {
	t.Helper()
	raw := []byte(`[
		{"jsonrpc":"2.0","method":"req","params":null,"id":"a"},
		{"jsonrpc":"2.0","method":"notify","params":null}
	]`)
	msgs, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if _, ok := msgs[0].(*Request); !ok {
		t.Fatalf("element 0: expected *Request, got %T", msgs[0])
	}
	if _, ok := msgs[1].(*Notification); !ok {
		t.Fatalf("element 1: expected *Notification, got %T", msgs[1])
	}
}

func TestDecodeRequest_Direct(t *testing.T) {
	t.Helper()
	req, err := DecodeRequest([]byte(`{"jsonrpc":"2.0","method":"test","id":"x"}`))
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if req.Method != "test" {
		t.Fatalf("method = %q", req.Method)
	}
}

func TestDecodeRequest_MissingMethod(t *testing.T) {
	t.Helper()
	_, err := DecodeRequest([]byte(`{"jsonrpc":"2.0","id":"x"}`))
	if err == nil {
		t.Fatal("expected error for missing method")
	}
}

func TestDecodeResponse_Direct(t *testing.T) {
	t.Helper()
	resp, err := DecodeResponse([]byte(`{"jsonrpc":"2.0","result":42,"id":1}`))
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if resp.Result == nil {
		t.Fatal("result is nil")
	}
}

func TestDecodeResponse_UnknownField(t *testing.T) {
	t.Helper()
	_, err := DecodeResponse([]byte(`{"jsonrpc":"2.0","result":42,"id":1,"extra":"bad"}`))
	if err == nil {
		t.Fatal("expected error for unknown field in response")
	}
}

func TestEncode_Request(t *testing.T) {
	t.Helper()
	req, _ := NewRequest(StringID("e1"), "test", nil)
	data, err := Encode(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Verify it round-trips.
	got, err := DecodeRequest(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Method != "test" {
		t.Fatalf("method = %q", got.Method)
	}
}

func TestEncodeBatch_Empty(t *testing.T) {
	t.Helper()
	_, err := EncodeBatch(nil)
	if err == nil {
		t.Fatal("expected error for empty batch encode")
	}
}

func TestEncodeBatch_Success(t *testing.T) {
	t.Helper()
	req1, _ := NewRequest(NumberID(1), "a", nil)
	req2, _ := NewRequest(NumberID(2), "b", nil)
	data, err := EncodeBatch([]interface{}{req1, req2})
	if err != nil {
		t.Fatalf("encode batch: %v", err)
	}
	// Should start with [
	if data[0] != '[' {
		t.Fatalf("batch should start with [, got %c", data[0])
	}
}
