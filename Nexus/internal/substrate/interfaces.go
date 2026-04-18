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
	"time"
)

// Identity represents an authenticated principal. Returned by AuthProvider
// after successful authentication. Used by PermissionChecker to determine
// what operations the principal may perform.
type Identity struct {
	// ID is a stable identifier for the principal (username, service
	// account name, API key ID, etc).
	ID string

	// DisplayName is a human-readable name. Optional.
	DisplayName string

	// Roles lists the roles assigned to this principal. The default
	// implementation uses a single role "admin" for the admin token.
	// Implementations may populate this from LDAP groups, OIDC claims,
	// or other sources.
	Roles []string

	// Attributes is a free-form map for implementation-specific data
	// (e.g. LDAP DN, OIDC sub, custom claims). Default implementation
	// leaves this empty.
	Attributes map[string]string
}

// AuthProvider authenticates incoming requests. The default implementation
// accepts a single admin token configured via the substrate config.
// Implementations must be safe for concurrent use.
type AuthProvider interface {
	// Authenticate verifies the given credentials and returns an Identity
	// if valid. Returns an error if credentials are invalid, expired, or
	// cannot be verified. The format of credentials is
	// implementation-defined; the default accepts a bearer token.
	Authenticate(ctx context.Context, credentials []byte) (*Identity, error)
}

// AuditEvent represents a substrate event that should be recorded in the
// audit log. The default AuditSink writes these to the existing audit log
// infrastructure. Alternative implementations may forward events to
// external systems.
type AuditEvent struct {
	// Timestamp is when the event occurred.
	Timestamp time.Time

	// Type is a short event type identifier like "sketch_written",
	// "memory_shredded", "ratchet_advanced".
	Type string

	// MemoryID is the affected memory, if applicable.
	MemoryID string

	// Identity is the principal that triggered the event, if known.
	Identity *Identity

	// Payload is event-specific data as a JSON-serializable map.
	Payload map[string]interface{}
}

// AuditSink receives substrate audit events for delivery. The default
// implementation writes events to the substrate logger. Implementations
// must be safe for concurrent use. Implementations should not block the
// caller; prefer async delivery with buffering if the backing store may
// be slow.
type AuditSink interface {
	// Emit delivers an audit event. Returns an error if the event
	// cannot be recorded. The caller is responsible for deciding
	// whether to fail the operation on audit errors (substrate
	// currently logs and continues).
	Emit(ctx context.Context, event AuditEvent) error
}

// PermissionChecker enforces access control on substrate operations.
// The default implementation permits operations only for identities
// bearing the "admin" role. Implementations must be safe for concurrent
// use.
type PermissionChecker interface {
	// Check verifies that the given identity may perform the given
	// operation on the given resource. Returns nil if permitted,
	// an error if denied. The operation is a short verb like
	// "sketch.read", "sketch.write", "memory.shred", "ratchet.rotate",
	// "deletion.prove". The resource is an identifier or "*" for
	// global operations.
	Check(ctx context.Context, identity *Identity, operation string, resource string) error
}

// Option configures a Substrate instance. Used as variadic argument
// to New() for dependency injection of alternative interface
// implementations.
type Option func(*Substrate)

// WithAuthProvider overrides the default AuthProvider. Callers may
// inject LDAP, OIDC, SAML, or other authentication providers.
// A nil argument is ignored.
func WithAuthProvider(ap AuthProvider) Option {
	return func(s *Substrate) {
		if ap != nil {
			s.authProvider = ap
		}
	}
}

// WithAuditSink overrides the default AuditSink. Callers may inject
// Splunk, Datadog, Elasticsearch, CloudWatch, or other audit delivery
// implementations. A nil argument is ignored.
func WithAuditSink(sink AuditSink) Option {
	return func(s *Substrate) {
		if sink != nil {
			s.auditSink = sink
		}
	}
}

// WithPermissionChecker overrides the default PermissionChecker.
// Callers may inject multi-tenant RBAC or other access control
// implementations. A nil argument is ignored.
func WithPermissionChecker(pc PermissionChecker) Option {
	return func(s *Substrate) {
		if pc != nil {
			s.permissionChecker = pc
		}
	}
}
