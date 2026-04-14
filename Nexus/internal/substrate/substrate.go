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

// Package substrate implements the BF-Sketch substrate for BubbleFish Nexus.
// BF-Sketch is a unified composition of binary quantization (RaBitQ),
// per-memory AES-256-GCM encryption with HKDF-derived keys, a cuckoo filter
// deletion oracle, and a forward-secure HMAC-SHA-256 hash ratchet.
//
// The substrate is controlled by the [substrate] section in daemon.toml and
// defaults to disabled in v0.1.3. When disabled, all methods are safe no-ops.
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan.
package substrate

import (
	"errors"
	"log/slog"
)

// Substrate is the top-level coordinator for the BF-Sketch substrate.
// A nil Substrate is safe to use; all methods return ErrDisabled.
//
// When disabled, no substrate code paths are invoked and the daemon
// behaves identically to a pre-substrate build.
type Substrate struct {
	cfg    Config
	logger *slog.Logger
	// Sub-components are lazily populated in BS.2–BS.7.
	// When disabled, all fields remain nil.
}

// New creates a Substrate from the given config. When disabled, returns
// a stub that is safe to use (all methods are no-ops returning ErrDisabled).
//
// When enabled, the full initialization path runs (BS.2+). For BS.1, enabling
// returns an error so the daemon fails closed if someone enables the flag
// without the full implementation in place.
func New(cfg Config, logger *slog.Logger) (*Substrate, error) {
	if !cfg.Enabled {
		return &Substrate{cfg: cfg, logger: logger}, nil
	}
	// Enabled-path setup deferred to BS.2+. Fail closed until then.
	return nil, errors.New("substrate: enabled path not yet implemented (BS.1 scaffold only)")
}

// Enabled reports whether the substrate is active.
func (s *Substrate) Enabled() bool {
	if s == nil {
		return false
	}
	return s.cfg.Enabled
}

// Shutdown releases resources and persists state.
// Safe to call on nil.
func (s *Substrate) Shutdown() error {
	if s == nil {
		return nil
	}
	return nil
}
