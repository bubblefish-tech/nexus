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
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// ─── Interface compliance (compile-time) ────────────────────────────────

func TestInterfaceCompliance(t *testing.T) {
	t.Helper()
	var _ AuthProvider = (*defaultAuthProvider)(nil)
	var _ AuditSink = (*defaultAuditSink)(nil)
	var _ PermissionChecker = (*defaultPermissionChecker)(nil)
}

// ─── defaultAuthProvider ────────────────────────────────────────────────

func TestDefaultAuthProviderAcceptsCorrectToken(t *testing.T) {
	t.Helper()
	ap := newDefaultAuthProvider("secret-token")
	id, err := ap.Authenticate(context.Background(), []byte("secret-token"))
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if id == nil {
		t.Fatal("expected identity, got nil")
	}
	if id.ID != "admin" {
		t.Errorf("expected admin, got %s", id.ID)
	}
	if len(id.Roles) != 1 || id.Roles[0] != "admin" {
		t.Errorf("expected [admin] roles, got %v", id.Roles)
	}
	if id.DisplayName == "" {
		t.Error("expected non-empty display name")
	}
}

func TestDefaultAuthProviderRejectsWrongToken(t *testing.T) {
	t.Helper()
	ap := newDefaultAuthProvider("secret-token")
	if _, err := ap.Authenticate(context.Background(), []byte("wrong-token")); err == nil {
		t.Fatal("expected error on wrong token")
	}
}

func TestDefaultAuthProviderRejectsEmptyCredentials(t *testing.T) {
	t.Helper()
	ap := newDefaultAuthProvider("secret-token")
	if _, err := ap.Authenticate(context.Background(), []byte{}); err == nil {
		t.Fatal("expected error on empty credentials")
	}
}

func TestDefaultAuthProviderRejectsWhenUnconfigured(t *testing.T) {
	t.Helper()
	ap := newDefaultAuthProvider("")
	if _, err := ap.Authenticate(context.Background(), []byte("anything")); err == nil {
		t.Fatal("expected error when admin token not configured")
	}
}

func TestDefaultAuthProviderRejectsNearMatch(t *testing.T) {
	t.Helper()
	ap := newDefaultAuthProvider("secret-token")
	if _, err := ap.Authenticate(context.Background(), []byte("secret-tokxn")); err == nil {
		t.Fatal("expected error on near-match (constant-time compare should fail)")
	}
}

func TestDefaultAuthProviderRejectsLengthMismatch(t *testing.T) {
	t.Helper()
	ap := newDefaultAuthProvider("secret-token")
	if _, err := ap.Authenticate(context.Background(), []byte("secret-token-extra")); err == nil {
		t.Fatal("expected error on longer credentials")
	}
	if _, err := ap.Authenticate(context.Background(), []byte("secret-toke")); err == nil {
		t.Fatal("expected error on shorter credentials")
	}
}

// ─── defaultAuditSink ───────────────────────────────────────────────────

func TestDefaultAuditSinkLogsEvent(t *testing.T) {
	t.Helper()
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := newDefaultAuditSink(logger)
	err := sink.Emit(context.Background(), AuditEvent{
		Timestamp: time.Unix(1_700_000_000, 0),
		Type:      "sketch_written",
		MemoryID:  "mem-1",
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "substrate audit event") {
		t.Errorf("expected log line present, got: %s", out)
	}
	if !strings.Contains(out, "sketch_written") {
		t.Errorf("expected event_type in log, got: %s", out)
	}
	if !strings.Contains(out, "mem-1") {
		t.Errorf("expected memory_id in log, got: %s", out)
	}
}

func TestDefaultAuditSinkTolerateNilLogger(t *testing.T) {
	t.Helper()
	sink := newDefaultAuditSink(nil)
	if err := sink.Emit(context.Background(), AuditEvent{Type: "x"}); err != nil {
		t.Errorf("nil-logger emit should not error: %v", err)
	}
}

func TestDefaultAuditSinkFillsZeroTimestamp(t *testing.T) {
	t.Helper()
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := newDefaultAuditSink(logger)
	if err := sink.Emit(context.Background(), AuditEvent{Type: "ratchet_advanced"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "timestamp=") {
		t.Errorf("expected timestamp key in log line, got: %s", buf.String())
	}
}

func TestDefaultAuditSinkOmitsEmptyFields(t *testing.T) {
	t.Helper()
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := newDefaultAuditSink(logger)
	if err := sink.Emit(context.Background(), AuditEvent{Type: "bare"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "memory_id=") {
		t.Errorf("expected memory_id to be omitted when empty, got: %s", out)
	}
	if strings.Contains(out, "identity_id=") {
		t.Errorf("expected identity_id to be omitted when nil, got: %s", out)
	}
}

func TestDefaultAuditSinkIncludesIdentityID(t *testing.T) {
	t.Helper()
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := newDefaultAuditSink(logger)
	err := sink.Emit(context.Background(), AuditEvent{
		Type:     "memory_shredded",
		Identity: &Identity{ID: "op-42"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "op-42") {
		t.Errorf("expected identity id in log, got: %s", buf.String())
	}
}

// captureAuditSink is a test-double AuditSink that records all events.
type captureAuditSink struct {
	events []AuditEvent
}

func (c *captureAuditSink) Emit(_ context.Context, event AuditEvent) error {
	c.events = append(c.events, event)
	return nil
}

func TestCustomAuditSinkReceivesEvents(t *testing.T) {
	t.Helper()
	capture := &captureAuditSink{}
	var _ AuditSink = capture
	if err := capture.Emit(context.Background(), AuditEvent{Type: "x", MemoryID: "m"}); err != nil {
		t.Fatal(err)
	}
	if len(capture.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(capture.events))
	}
	if capture.events[0].MemoryID != "m" {
		t.Errorf("unexpected memory id: %q", capture.events[0].MemoryID)
	}
}

// ─── defaultPermissionChecker ───────────────────────────────────────────

func TestDefaultPermissionCheckerPermitsAdmin(t *testing.T) {
	t.Helper()
	pc := newDefaultPermissionChecker()
	id := &Identity{ID: "admin", Roles: []string{"admin"}}
	if err := pc.Check(context.Background(), id, "sketch.write", "mem-1"); err != nil {
		t.Errorf("expected permit, got %v", err)
	}
}

func TestDefaultPermissionCheckerDeniesNonAdmin(t *testing.T) {
	t.Helper()
	pc := newDefaultPermissionChecker()
	id := &Identity{ID: "user", Roles: []string{"reader"}}
	if err := pc.Check(context.Background(), id, "sketch.write", "mem-1"); err == nil {
		t.Error("expected deny for non-admin")
	}
}

func TestDefaultPermissionCheckerDeniesNilIdentity(t *testing.T) {
	t.Helper()
	pc := newDefaultPermissionChecker()
	if err := pc.Check(context.Background(), nil, "sketch.write", "mem-1"); err == nil {
		t.Error("expected deny for nil identity")
	}
}

func TestDefaultPermissionCheckerDeniesIdentityWithNoRoles(t *testing.T) {
	t.Helper()
	pc := newDefaultPermissionChecker()
	id := &Identity{ID: "lonely"}
	if err := pc.Check(context.Background(), id, "sketch.read", "*"); err == nil {
		t.Error("expected deny for identity with no roles")
	}
}

func TestDefaultPermissionCheckerPermitsIdentityWithAdminAmongMany(t *testing.T) {
	t.Helper()
	pc := newDefaultPermissionChecker()
	id := &Identity{ID: "multi", Roles: []string{"reader", "admin", "auditor"}}
	if err := pc.Check(context.Background(), id, "ratchet.rotate", "*"); err != nil {
		t.Errorf("expected permit (admin among many roles), got %v", err)
	}
}

// ─── Option functions ──────────────────────────────────────────────────

func TestWithAuthProviderOverrides(t *testing.T) {
	t.Helper()
	s := &Substrate{}
	custom := newDefaultAuthProvider("other")
	WithAuthProvider(custom)(s)
	if s.authProvider != custom {
		t.Error("WithAuthProvider did not override")
	}
}

func TestWithAuthProviderIgnoresNil(t *testing.T) {
	t.Helper()
	s := &Substrate{}
	original := newDefaultAuthProvider("keep")
	s.authProvider = original
	WithAuthProvider(nil)(s)
	if s.authProvider != original {
		t.Error("WithAuthProvider should ignore nil")
	}
}

func TestWithAuditSinkOverrides(t *testing.T) {
	t.Helper()
	s := &Substrate{}
	custom := &captureAuditSink{}
	WithAuditSink(custom)(s)
	if s.auditSink != custom {
		t.Error("WithAuditSink did not override")
	}
}

func TestWithAuditSinkIgnoresNil(t *testing.T) {
	t.Helper()
	s := &Substrate{}
	original := &captureAuditSink{}
	s.auditSink = original
	WithAuditSink(nil)(s)
	if s.auditSink != original {
		t.Error("WithAuditSink should ignore nil")
	}
}

func TestWithPermissionCheckerOverrides(t *testing.T) {
	t.Helper()
	s := &Substrate{}
	custom := newDefaultPermissionChecker()
	WithPermissionChecker(custom)(s)
	if s.permissionChecker != custom {
		t.Error("WithPermissionChecker did not override")
	}
}

func TestWithPermissionCheckerIgnoresNil(t *testing.T) {
	t.Helper()
	s := &Substrate{}
	original := newDefaultPermissionChecker()
	s.permissionChecker = original
	WithPermissionChecker(nil)(s)
	if s.permissionChecker != original {
		t.Error("WithPermissionChecker should ignore nil")
	}
}

// ─── applyDefaultInterfaces ─────────────────────────────────────────────

func TestApplyDefaultInterfacesWiresAllThree(t *testing.T) {
	t.Helper()
	s := &Substrate{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	applyDefaultInterfaces(s, "token", logger)
	if s.authProvider == nil {
		t.Error("authProvider not wired")
	}
	if s.auditSink == nil {
		t.Error("auditSink not wired")
	}
	if s.permissionChecker == nil {
		t.Error("permissionChecker not wired")
	}
}

func TestApplyDefaultInterfacesUsesAdminToken(t *testing.T) {
	t.Helper()
	s := &Substrate{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	applyDefaultInterfaces(s, "the-token", logger)
	if _, err := s.authProvider.Authenticate(context.Background(), []byte("the-token")); err != nil {
		t.Errorf("default provider should accept the configured token: %v", err)
	}
	if _, err := s.authProvider.Authenticate(context.Background(), []byte("wrong")); err == nil {
		t.Error("default provider should reject wrong token")
	}
}
