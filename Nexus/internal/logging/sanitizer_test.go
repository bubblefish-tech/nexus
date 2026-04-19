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

package logging_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/logging"
)

// captureHandler records the last log record for inspection.
type captureHandler struct {
	buf *bytes.Buffer
	inner slog.Handler
}

func newCapture() (*captureHandler, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	inner := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return &captureHandler{buf: buf, inner: inner}, buf
}

func (c *captureHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return c.inner.Enabled(ctx, level)
}
func (c *captureHandler) Handle(ctx context.Context, r slog.Record) error {
	return c.inner.Handle(ctx, r)
}
func (c *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &captureHandler{buf: c.buf, inner: c.inner.WithAttrs(attrs)}
}
func (c *captureHandler) WithGroup(name string) slog.Handler {
	return &captureHandler{buf: c.buf, inner: c.inner.WithGroup(name)}
}

func logAndCapture(t *testing.T, fn func(*slog.Logger)) string {
	t.Helper()
	cap, buf := newCapture()
	logger := slog.New(logging.NewSanitizingHandler(cap))
	fn(logger)
	return buf.String()
}

func TestSanitizingHandler_TokenInMessage(t *testing.T) {
	t.Helper()
	out := logAndCapture(t, func(l *slog.Logger) {
		l.Info("auth failed for token bfn_abc123secretXYZ")
	})
	if strings.Contains(out, "bfn_") {
		t.Errorf("token not redacted: %s", out)
	}
	if !strings.Contains(out, "[REDACTED:token]") {
		t.Errorf("expected [REDACTED:token] in output: %s", out)
	}
}

func TestSanitizingHandler_TokenInAttr(t *testing.T) {
	t.Helper()
	out := logAndCapture(t, func(l *slog.Logger) {
		l.Info("auth event", "token", "bfn_secretvalue99")
	})
	if strings.Contains(out, "bfn_") {
		t.Errorf("token not redacted in attr: %s", out)
	}
	if !strings.Contains(out, "[REDACTED:token]") {
		t.Errorf("expected [REDACTED:token]: %s", out)
	}
}

func TestSanitizingHandler_Base64LongRedacted(t *testing.T) {
	t.Helper()
	// 48 bytes → 64-char base64 string (exactly 64 chars, should be redacted)
	raw := make([]byte, 48)
	for i := range raw {
		raw[i] = byte(i % 256)
	}
	b64 := base64.StdEncoding.EncodeToString(raw)
	if len(b64) < 64 {
		t.Fatalf("test setup: expected b64 len >= 64, got %d", len(b64))
	}

	out := logAndCapture(t, func(l *slog.Logger) {
		l.Info("blob received", "data", b64)
	})
	if strings.Contains(out, b64) {
		t.Errorf("base64 blob not redacted: %s", out)
	}
	if !strings.Contains(out, "[REDACTED:base64]") {
		t.Errorf("expected [REDACTED:base64]: %s", out)
	}
}

func TestSanitizingHandler_Base64ShortNotRedacted(t *testing.T) {
	t.Helper()
	// 30 bytes → 40-char base64 (under 64 chars, should NOT be redacted)
	raw := make([]byte, 30)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	b64 := base64.StdEncoding.EncodeToString(raw)
	if len(b64) >= 64 {
		t.Fatalf("test setup: expected b64 len < 64, got %d", len(b64))
	}

	out := logAndCapture(t, func(l *slog.Logger) {
		l.Info("short blob", "data", b64)
	})
	if !strings.Contains(out, b64) {
		t.Errorf("short base64 should not be redacted; output: %s", out)
	}
}

func TestSanitizingHandler_ContentKeyRedacted(t *testing.T) {
	t.Helper()
	out := logAndCapture(t, func(l *slog.Logger) {
		l.Info("memory stored", "content", "the user prefers dark mode")
	})
	if strings.Contains(out, "dark mode") {
		t.Errorf("content attr value not redacted: %s", out)
	}
	if !strings.Contains(out, "[REDACTED:content]") {
		t.Errorf("expected [REDACTED:content]: %s", out)
	}
}

func TestSanitizingHandler_MemoryContentKeyRedacted(t *testing.T) {
	t.Helper()
	out := logAndCapture(t, func(l *slog.Logger) {
		l.Info("write", "memory_content", "sensitive payload")
	})
	if strings.Contains(out, "sensitive payload") {
		t.Errorf("memory_content attr not redacted: %s", out)
	}
	if !strings.Contains(out, "[REDACTED:content]") {
		t.Errorf("expected [REDACTED:content]: %s", out)
	}
}

func TestSanitizingHandler_NonSensitivePassThrough(t *testing.T) {
	t.Helper()
	out := logAndCapture(t, func(l *slog.Logger) {
		l.Info("request processed", "component", "handler", "status", 200)
	})
	if !strings.Contains(out, "handler") {
		t.Errorf("non-sensitive value should pass through: %s", out)
	}
	if strings.Contains(out, "REDACTED") {
		t.Errorf("unexpected redaction in non-sensitive log: %s", out)
	}
}

func TestSanitizingHandler_Enabled(t *testing.T) {
	t.Helper()
	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := logging.NewSanitizingHandler(inner)
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("debug should not be enabled when inner is Warn")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("error should be enabled when inner is Warn")
	}
}

func TestSanitizingHandler_WithAttrs(t *testing.T) {
	t.Helper()
	buf := &bytes.Buffer{}
	inner := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := logging.NewSanitizingHandler(inner)
	// WithAttrs adds a token-bearing attr at construction time.
	child := h.WithAttrs([]slog.Attr{slog.String("api_key", "bfn_withattrstoken")})
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	_ = child.Handle(context.Background(), r)
	if strings.Contains(buf.String(), "bfn_") {
		t.Errorf("WithAttrs token not redacted: %s", buf.String())
	}
}

func TestSanitizingHandler_WithGroup(t *testing.T) {
	t.Helper()
	out := logAndCapture(t, func(l *slog.Logger) {
		l.WithGroup("auth").Info("event", "token", "bfn_grouptoken123")
	})
	if strings.Contains(out, "bfn_") {
		t.Errorf("WithGroup token not redacted: %s", out)
	}
}

func TestSanitizingHandler_GroupedAttr(t *testing.T) {
	t.Helper()
	out := logAndCapture(t, func(l *slog.Logger) {
		l.Info("nested",
			slog.Group("payload",
				slog.String("content", "secret memory content"),
				slog.String("source", "claude"),
			),
		)
	})
	if strings.Contains(out, "secret memory content") {
		t.Errorf("grouped content attr not redacted: %s", out)
	}
	if !strings.Contains(out, "[REDACTED:content]") {
		t.Errorf("expected [REDACTED:content] in grouped attr: %s", out)
	}
	if !strings.Contains(out, "claude") {
		t.Errorf("non-content grouped attr should pass through: %s", out)
	}
}

func TestSanitizingHandler_MultipleTokensInMessage(t *testing.T) {
	t.Helper()
	out := logAndCapture(t, func(l *slog.Logger) {
		l.Warn("tokens bfn_first and bfn_second both failed")
	})
	count := strings.Count(out, "[REDACTED:token]")
	if count != 2 {
		t.Errorf("expected 2 token redactions, got %d: %s", count, out)
	}
}
