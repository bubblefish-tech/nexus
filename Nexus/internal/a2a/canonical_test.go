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

func TestCanonicalizeKeyOrder(t *testing.T) {
	input := `{"z":1,"a":2,"m":3}`
	got, err := Canonicalize([]byte(input))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	want := `{"a":2,"m":3,"z":1}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// TestCanonicalizeUTF16KeyOrder exercises RFC 8785 §3.2.3 — JSON object keys
// must sort by UTF-16 code units, NOT by UTF-8 byte order. The two orderings
// diverge for any code point above U+FFFF (encoded as a high/low surrogate
// pair in UTF-16, with the first unit in 0xD800..0xDBFF, but encoded as a
// four-byte sequence starting at 0xF0 in UTF-8 — past 0xEE where U+E000+
// single-unit characters sit in UTF-8).
//
// Concrete case used here:
//
//	"𝐀" (U+1D400, MATHEMATICAL BOLD CAPITAL A)
//	    UTF-16: 0xD835 0xDC00
//	    UTF-8 : 0xF0 0x9D 0x90 0x80
//	"" (U+E000, first code point of the Private Use Area)
//	    UTF-16: 0xE000
//	    UTF-8 : 0xEE 0x80 0x80
//
// UTF-16 order: 𝐀 < "" (because 0xD835 < 0xE000).
// UTF-8 byte order: "" < 𝐀 (because 0xEE < 0xF0).
//
// A spec-conformant implementation MUST emit "𝐀" before "" — which is
// what RFC 8785 §3.2.3 mandates and what consumers of the canonical form
// (Ed25519 signers, hash producers) will expect.
func TestCanonicalizeUTF16KeyOrder(t *testing.T) {
	input := "{\"\\uE000\":\"pua\",\"\\uD835\\uDC00\":\"math-A\"}"
	got, err := Canonicalize([]byte(input))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	want := "{\"\xF0\x9D\x90\x80\":\"math-A\",\"\xEE\x80\x80\":\"pua\"}"
	if string(got) != want {
		t.Errorf("\n got: %q\nwant: %q", string(got), want)
	}
}

func TestCanonicalizeNestedObjects(t *testing.T) {
	input := `{"b":{"z":1,"a":2},"a":{"y":3,"x":4}}`
	got, err := Canonicalize([]byte(input))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	want := `{"a":{"x":4,"y":3},"b":{"a":2,"z":1}}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCanonicalizeArrayOrder(t *testing.T) {
	// Arrays preserve order
	input := `[3,1,2]`
	got, err := Canonicalize([]byte(input))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	want := `[3,1,2]`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCanonicalizeWhitespace(t *testing.T) {
	input := `{  "a" : 1 , "b" : [ 2 , 3 ] }`
	got, err := Canonicalize([]byte(input))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	want := `{"a":1,"b":[2,3]}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCanonicalizeLiterals(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"null", `null`, `null`},
		{"true", `true`, `true`},
		{"false", `false`, `false`},
		{"empty string", `""`, `""`},
		{"string", `"hello"`, `"hello"`},
		{"empty object", `{}`, `{}`},
		{"empty array", `[]`, `[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Canonicalize([]byte(tt.input))
			if err != nil {
				t.Fatalf("Canonicalize: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCanonicalizeNumbers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"zero", `0`, `0`},
		{"integer", `42`, `42`},
		{"negative integer", `-17`, `-17`},
		{"decimal", `1.5`, `1.5`},
		{"negative decimal", `-0.25`, `-0.25`},
		{"large integer", `1000000`, `1000000`},
		{"scientific to plain", `1e2`, `100`},
		{"negative zero", `-0`, `0`},
		{"small decimal", `0.001`, `0.001`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Canonicalize([]byte(tt.input))
			if err != nil {
				t.Fatalf("Canonicalize: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCanonicalizeStringEscaping(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"backslash", `"\\"`, `"\\"`},
		{"quote", `"\""`, `"\""`},
		{"newline", `"\n"`, `"\n"`},
		{"tab", `"\t"`, `"\t"`},
		{"backspace", `"\b"`, `"\b"`},
		{"formfeed", `"\f"`, `"\f"`},
		{"carriage return", `"\r"`, `"\r"`},
		{"control char", `"\u0001"`, `"\u0001"`},
		{"unicode literal", `"café"`, `"café"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Canonicalize([]byte(tt.input))
			if err != nil {
				t.Fatalf("Canonicalize: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCanonicalizeIdempotent(t *testing.T) {
	input := `{"b":2,"a":1}`
	first, err := Canonicalize([]byte(input))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := Canonicalize(first)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("not idempotent: %s vs %s", first, second)
	}
}

func TestCanonicalizeInvalidJSON(t *testing.T) {
	_, err := Canonicalize([]byte(`not json at all`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCanonicalizeAgentCard(t *testing.T) {
	card := AgentCard{
		Name:            "test-agent",
		URL:             "https://example.com",
		ProtocolVersion: "1.0",
		Endpoints: []Endpoint{
			{URL: "https://example.com/a2a", Transport: TransportHTTP},
		},
		Capabilities: AgentCapabilities{Streaming: true},
	}
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	canonical, err := Canonicalize(data)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}

	// Verify determinism: canonicalize again
	canonical2, err := Canonicalize(canonical)
	if err != nil {
		t.Fatalf("canonicalize2: %v", err)
	}
	if string(canonical) != string(canonical2) {
		t.Error("agent card canonicalization is not deterministic")
	}

	// Verify key ordering in output
	want := `{"capabilities":{"streaming":true},"endpoints":[{"transport":"http","url":"https://example.com/a2a"}],"name":"test-agent","protocolVersion":"1.0","url":"https://example.com"}`
	if string(canonical) != want {
		t.Errorf("canonical agent card:\ngot:  %s\nwant: %s", canonical, want)
	}
}

func TestCanonicalizeComplexNested(t *testing.T) {
	input := `{"c":[{"z":true,"a":false}],"a":{"nested":{"b":2,"a":1}},"b":null}`
	got, err := Canonicalize([]byte(input))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	want := `{"a":{"nested":{"a":1,"b":2}},"b":null,"c":[{"a":false,"z":true}]}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCanonicalizeMixedArray(t *testing.T) {
	input := `[1,"two",true,null,{"b":2,"a":1}]`
	got, err := Canonicalize([]byte(input))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	want := `[1,"two",true,null,{"a":1,"b":2}]`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
