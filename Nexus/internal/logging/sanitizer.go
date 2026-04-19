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

package logging

import (
	"context"
	"encoding/base64"
	"log/slog"
	"regexp"
	"strings"
)

var (
	// tokenRe matches bfn_ API token patterns anywhere in a string.
	tokenRe = regexp.MustCompile(`bfn_\S+`)

	// base64Re matches candidate base64 strings of 64 or more characters.
	base64Re = regexp.MustCompile(`[A-Za-z0-9+/]{64,}={0,2}`)
)

// SanitizingHandler is a slog.Handler wrapper that redacts sensitive patterns
// from all log records before forwarding them to the inner handler.
//
// Redaction rules:
//   - bfn_ token patterns     → [REDACTED:token]
//   - Base64 strings > 64 chars → [REDACTED:base64]
//   - Memory content attributes → [REDACTED:content]
type SanitizingHandler struct {
	inner slog.Handler
}

// NewSanitizingHandler returns a SanitizingHandler that wraps inner.
func NewSanitizingHandler(inner slog.Handler) *SanitizingHandler {
	return &SanitizingHandler{inner: inner}
}

func (h *SanitizingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *SanitizingHandler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, redactString(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, nr)
}

func (h *SanitizingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		sanitized[i] = redactAttr(a)
	}
	return &SanitizingHandler{inner: h.inner.WithAttrs(sanitized)}
}

func (h *SanitizingHandler) WithGroup(name string) slog.Handler {
	return &SanitizingHandler{inner: h.inner.WithGroup(name)}
}

// redactString applies token and base64 redaction to a plain string.
func redactString(s string) string {
	s = tokenRe.ReplaceAllString(s, "[REDACTED:token]")
	s = base64Re.ReplaceAllStringFunc(s, func(m string) string {
		if isLikelyBase64(m) {
			return "[REDACTED:base64]"
		}
		return m
	})
	return s
}

// redactAttr sanitizes a single slog.Attr, recursing into groups.
func redactAttr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	switch a.Value.Kind() {
	case slog.KindGroup:
		group := a.Value.Group()
		sanitized := make([]slog.Attr, len(group))
		for i, ga := range group {
			sanitized[i] = redactAttr(ga)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(sanitized...)}
	case slog.KindString:
		if isContentKey(a.Key) {
			return slog.String(a.Key, "[REDACTED:content]")
		}
		return slog.String(a.Key, redactString(a.Value.String()))
	default:
		return a
	}
}

// isContentKey reports whether the attribute key refers to memory content.
func isContentKey(key string) bool {
	lower := strings.ToLower(key)
	return lower == "content" ||
		strings.Contains(lower, "memory_content") ||
		strings.Contains(lower, "mem_content")
}

// isLikelyBase64 returns true if s decodes successfully as standard base64.
func isLikelyBase64(s string) bool {
	stripped := strings.TrimRight(s, "=")
	padded := stripped
	switch len(stripped) % 4 {
	case 2:
		padded = stripped + "=="
	case 3:
		padded = stripped + "="
	}
	_, err := base64.StdEncoding.DecodeString(padded)
	return err == nil
}
