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
	"strings"
	"testing"
)

func TestAllErrorCodesUnique(t *testing.T) {
	codes := AllErrorCodes()
	seen := make(map[int]bool, len(codes))
	for _, c := range codes {
		if seen[c] {
			t.Errorf("duplicate error code %d", c)
		}
		seen[c] = true
	}
}

func TestAllErrorCodesHaveNames(t *testing.T) {
	for _, c := range AllErrorCodes() {
		name := ErrorCodeName(c)
		if name == "" {
			t.Errorf("error code %d has no name", c)
		}
	}
}

func TestErrorCodeNameUnknown(t *testing.T) {
	if name := ErrorCodeName(99999); name != "" {
		t.Errorf("unknown code should return empty, got %q", name)
	}
}

func TestNewError(t *testing.T) {
	e := NewError(CodeInvalidRequest, "bad request")
	if e.Code != CodeInvalidRequest {
		t.Errorf("expected code %d, got %d", CodeInvalidRequest, e.Code)
	}
	if e.Message != "bad request" {
		t.Errorf("expected message %q, got %q", "bad request", e.Message)
	}
	if e.Data != nil {
		t.Errorf("expected nil data, got %v", e.Data)
	}
}

func TestNewErrorf(t *testing.T) {
	e := NewErrorf(CodeSkillNotFound, "skill %q not found", "test_skill")
	if !strings.Contains(e.Message, "test_skill") {
		t.Errorf("message should contain skill name: %s", e.Message)
	}
}

func TestNewErrorWithData(t *testing.T) {
	data := ErrorData{TraceID: "aud_test123"}
	e := NewErrorWithData(CodePermissionDenied, "denied", data)
	if e.Data == nil {
		t.Fatal("data should not be nil")
	}
	ed, ok := e.Data.(ErrorData)
	if !ok {
		t.Fatal("data should be ErrorData")
	}
	if ed.TraceID != "aud_test123" {
		t.Errorf("expected traceId %q, got %q", "aud_test123", ed.TraceID)
	}
}

func TestErrorInterface(t *testing.T) {
	e := NewError(CodeInternalError, "something broke")
	msg := e.Error()
	if !strings.Contains(msg, "INTERNAL_ERROR") {
		t.Errorf("error string should contain code name: %s", msg)
	}
	if !strings.Contains(msg, "-32603") {
		t.Errorf("error string should contain code number: %s", msg)
	}
	if !strings.Contains(msg, "something broke") {
		t.Errorf("error string should contain message: %s", msg)
	}
}

func TestErrorCodesInExpectedRange(t *testing.T) {
	for _, c := range AllErrorCodes() {
		if c > -32000 && c < -32099 {
			// NA2A custom codes should be in -32000 to -32099
		} else if c >= -32700 && c <= -32600 {
			// Standard JSON-RPC codes
		} else if c >= -32012 && c <= -32000 {
			// NA2A codes
		} else {
			t.Errorf("unexpected error code range: %d", c)
		}
	}
}

func TestStandardJSONRPCCodes(t *testing.T) {
	// Verify the standard JSON-RPC codes are present
	tests := []struct {
		code int
		name string
	}{
		{-32600, "INVALID_REQUEST"},
		{-32601, "METHOD_NOT_FOUND"},
		{-32602, "INVALID_PARAMS"},
		{-32603, "INTERNAL_ERROR"},
	}
	for _, tt := range tests {
		name := ErrorCodeName(tt.code)
		if name != tt.name {
			t.Errorf("code %d: expected %q, got %q", tt.code, tt.name, name)
		}
	}
}

func TestNA2ASpecificCodes(t *testing.T) {
	// Verify all NA2A-specific codes from §12
	tests := []struct {
		code int
		name string
	}{
		{-32000, "UNAUTHENTICATED"},
		{-32001, "PERMISSION_DENIED"},
		{-32002, "APPROVAL_REQUIRED"},
		{-32003, "RATE_LIMITED"},
		{-32004, "SKILL_NOT_FOUND"},
		{-32005, "INVALID_INPUT"},
		{-32006, "TASK_NOT_FOUND"},
		{-32007, "TASK_NOT_CANCELABLE"},
		{-32008, "TRANSPORT_ERROR"},
		{-32009, "CAPABILITY_NOT_DECLARED"},
		{-32010, "EXTENSION_REQUIRED"},
		{-32011, "INCOMPATIBLE_VERSION"},
		{-32012, "AGENT_OFFLINE"},
	}
	for _, tt := range tests {
		name := ErrorCodeName(tt.code)
		if name != tt.name {
			t.Errorf("code %d: expected %q, got %q", tt.code, tt.name, name)
		}
	}
}
