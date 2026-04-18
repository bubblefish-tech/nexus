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

package substrate

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// defaultAuthProvider accepts a single admin token configured at
// substrate initialization. All authenticated requests receive an
// Identity with ID="admin" and Roles=["admin"]. If no admin token is
// configured (empty string), authentication always fails.
type defaultAuthProvider struct {
	adminToken []byte
}

func newDefaultAuthProvider(adminToken string) *defaultAuthProvider {
	return &defaultAuthProvider{
		adminToken: []byte(adminToken),
	}
}

// Authenticate verifies credentials via constant-time comparison against
// the configured admin token.
func (p *defaultAuthProvider) Authenticate(ctx context.Context, credentials []byte) (*Identity, error) {
	if len(p.adminToken) == 0 {
		return nil, errors.New("substrate: no admin token configured")
	}
	if subtle.ConstantTimeCompare(credentials, p.adminToken) != 1 {
		return nil, errors.New("substrate: invalid credentials")
	}
	return &Identity{
		ID:          "admin",
		DisplayName: "Substrate Admin",
		Roles:       []string{"admin"},
		Attributes:  map[string]string{},
	}, nil
}

// defaultAuditSink logs events to the substrate logger. It does not
// integrate with the existing hash-chained SubstrateAuditLog; call sites
// that emit audit events continue to use that log directly. This sink
// exists as a seam for alternative implementations to intercept and
// forward events; a subsequent consolidation phase will route internal
// emissions through the AuditSink interface.
type defaultAuditSink struct {
	logger *slog.Logger
}

func newDefaultAuditSink(logger *slog.Logger) *defaultAuditSink {
	return &defaultAuditSink{logger: logger}
}

// Emit records an event to the logger. Returns nil even if logger is nil
// so that audit events never block the caller on a missing sink.
func (s *defaultAuditSink) Emit(ctx context.Context, event AuditEvent) error {
	if s.logger == nil {
		return nil
	}
	ts := event.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	attrs := []any{
		"component", "substrate",
		"event_type", event.Type,
		"timestamp", ts.Format(time.RFC3339Nano),
	}
	if event.MemoryID != "" {
		attrs = append(attrs, "memory_id", event.MemoryID)
	}
	if event.Identity != nil {
		attrs = append(attrs, "identity_id", event.Identity.ID)
	}
	s.logger.Info("substrate audit event", attrs...)
	return nil
}

// defaultPermissionChecker permits all operations for any identity with
// the "admin" role. All other identities are denied. This matches the
// current single-admin access model of the substrate.
type defaultPermissionChecker struct{}

func newDefaultPermissionChecker() *defaultPermissionChecker {
	return &defaultPermissionChecker{}
}

// Check returns nil if the identity holds the "admin" role, otherwise
// an error describing the denial.
func (p *defaultPermissionChecker) Check(ctx context.Context, identity *Identity, operation string, resource string) error {
	if identity == nil {
		return errors.New("substrate: no identity provided")
	}
	for _, role := range identity.Roles {
		if role == "admin" {
			return nil
		}
	}
	return fmt.Errorf("substrate: identity %q not permitted for operation %q", identity.ID, operation)
}
