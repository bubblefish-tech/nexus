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

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"syscall"
	"testing"
)

func TestClassify_HTTPErrors(t *testing.T) {
	t.Helper()
	tests := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{"400", &HTTPError{Status: 400, Path: "/api/x"}, ErrKindClient},
		{"401", &HTTPError{Status: 401, Path: "/api/x"}, ErrKindForbidden},
		{"403", &HTTPError{Status: 403, Path: "/api/x"}, ErrKindForbidden},
		{"404", &HTTPError{Status: 404, Path: "/api/x"}, ErrKindNotFound},
		{"429", &HTTPError{Status: 429, Path: "/api/x"}, ErrKindClient},
		{"500", &HTTPError{Status: 500, Path: "/api/x"}, ErrKindServer},
		{"503", &HTTPError{Status: 503, Path: "/api/x"}, ErrKindServer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.err)
			if got != tt.want {
				t.Errorf("Classify(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestClassify_ContextErrors(t *testing.T) {
	t.Helper()
	tests := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{"deadline exceeded", context.DeadlineExceeded, ErrKindConnection},
		{"canceled", context.Canceled, ErrKindConnection},
		{"econnrefused", syscall.ECONNREFUSED, ErrKindConnection},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.err)
			if got != tt.want {
				t.Errorf("Classify(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestClassify_NetError(t *testing.T) {
	t.Helper()
	// Wrap a net.Error to confirm ErrKindConnection.
	err := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	got := Classify(err)
	if got != ErrKindConnection {
		t.Errorf("Classify(net.OpError) = %d, want ErrKindConnection", got)
	}
}

func TestClassify_SerializationError(t *testing.T) {
	t.Helper()
	// A JSON syntax error is not an HTTP or network error — falls through to Serialization.
	err := &json.SyntaxError{Offset: 5}
	got := Classify(err)
	if got != ErrKindSerialization {
		t.Errorf("Classify(json.SyntaxError) = %d, want ErrKindSerialization", got)
	}
}

func TestClassify_Nil(t *testing.T) {
	t.Helper()
	got := Classify(nil)
	if got != ErrKindUnknown {
		t.Errorf("Classify(nil) = %d, want ErrKindUnknown", got)
	}
}

func TestClassify_WrappedHTTPError(t *testing.T) {
	t.Helper()
	wrapped := fmt.Errorf("outer: %w", &HTTPError{Status: 403, Path: "/api/status"})
	got := Classify(wrapped)
	if got != ErrKindForbidden {
		t.Errorf("Classify(wrapped 403) = %d, want ErrKindForbidden", got)
	}
}

func TestHTTPError_Message(t *testing.T) {
	t.Helper()
	e := &HTTPError{Status: 404, Path: "/api/status"}
	want := "http 404 at /api/status"
	if e.Error() != want {
		t.Errorf("HTTPError.Error() = %q, want %q", e.Error(), want)
	}
}
