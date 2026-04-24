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
	"errors"
	"fmt"
	"net"
	"syscall"
)

// HTTPError wraps a non-2xx HTTP response with the status and path.
type HTTPError struct {
	Status int
	Path   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http %d at %s", e.Status, e.Path)
}

// ErrorKind classifies any error produced by the api package.
type ErrorKind int

const (
	ErrKindUnknown       ErrorKind = iota
	ErrKindConnection              // connection refused, DNS, TLS, timeout
	ErrKindNotFound                // 404
	ErrKindForbidden               // 401/403
	ErrKindClient                  // other 4xx
	ErrKindServer                  // 5xx
	ErrKindSerialization           // JSON decode failure
)

// Classify returns the ErrorKind for a given error. Never returns ErrKindUnknown
// for nil — use if err != nil first.
func Classify(err error) ErrorKind {
	if err == nil {
		return ErrKindUnknown
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		switch {
		case httpErr.Status == 404:
			return ErrKindNotFound
		case httpErr.Status == 401 || httpErr.Status == 403:
			return ErrKindForbidden
		case httpErr.Status >= 400 && httpErr.Status < 500:
			return ErrKindClient
		case httpErr.Status >= 500:
			return ErrKindServer
		}
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, syscall.ECONNREFUSED) {
		return ErrKindConnection
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return ErrKindConnection
	}
	return ErrKindSerialization
}
