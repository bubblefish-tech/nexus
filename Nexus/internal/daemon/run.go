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

package daemon

import (
	"log/slog"

	"github.com/bubblefish-tech/nexus/internal/config"
)

// Option is a functional option for Run. Enterprise and TS editions use
// options to inject provider implementations (RBAC, boundary enforcement,
// classification marking) without modifying community code.
type Option func(*runConfig)

type runConfig struct {
	rbac       RBACEngine
	boundary   BoundaryEnforcer
	classifier ClassificationMarker
}

// WithRBAC injects an Enterprise RBAC engine.
func WithRBAC(e RBACEngine) Option {
	return func(c *runConfig) { c.rbac = e }
}

// WithBoundaryEnforcer injects a TS boundary enforcement provider.
func WithBoundaryEnforcer(b BoundaryEnforcer) Option {
	return func(c *runConfig) { c.boundary = b }
}

// WithClassificationMarker injects a TS classification marking provider.
func WithClassificationMarker(m ClassificationMarker) Option {
	return func(c *runConfig) { c.classifier = m }
}

// Run creates and starts a Daemon with the provided configuration, applying
// any functional options. It blocks until the daemon exits and returns any
// startup or runtime error. Signal handling and graceful shutdown remain in
// the cmd layer — Run is the entry point for programmatic embedding.
func Run(cfg *config.Config, logger *slog.Logger, opts ...Option) error {
	rc := &runConfig{}
	for _, o := range opts {
		o(rc)
	}

	d := New(cfg, logger)

	// Wire optional provider interfaces when supplied.
	// Community edition passes no options; all fields remain nil.
	_ = rc // currently unused; wired in Phase 4 when Daemon grows provider fields

	return d.Start()
}
