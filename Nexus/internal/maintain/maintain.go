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

package maintain

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bubblefish-tech/nexus/internal/discover"
	"github.com/bubblefish-tech/nexus/internal/maintain/fingerprint"
	"github.com/bubblefish-tech/nexus/internal/maintain/learned"
	"github.com/bubblefish-tech/nexus/internal/maintain/registry"
	"github.com/bubblefish-tech/nexus/internal/maintain/topology"
)

// Config holds Maintainer configuration.
type Config struct {
	// ConfigDir is the nexus config directory; passed to the discovery scanner.
	ConfigDir string

	// ReconcileInterval is how often the convergence loop runs. Default: 60s.
	ReconcileInterval time.Duration

	// ScanInterval is how often tool discovery runs. Default: 30s.
	ScanInterval time.Duration

	// LearnedStorePath is the on-disk path for the adaptive fix store.
	// Defaults to ~/.nexus/maintain/learned/fixes.json.
	LearnedStorePath string
}

// ToolStatusRow is a snapshot of one tool's state for CLI display.
type ToolStatusRow struct {
	Name     string
	Status   string // "running", "stopped", "unknown"
	Drift    int    // number of active drift entries
	Health   bool   // API reachable
	Protocol string // fingerprinted protocol, or ""
}

// MaintainStatus is the snapshot returned by Maintainer.Status().
type MaintainStatus struct {
	Platform     string
	Tools        []ToolStatusRow
	TopologySet  bool
	LearnedCount int
	LastScan     time.Time
}

// Maintainer coordinates all Worm Detection & Maintenance subsystems (W1–W10)
// into a single lifecycle object. The daemon starts one Maintainer; CLI commands
// create ephemeral ones for one-shot operations.
type Maintainer struct {
	cfg        Config
	twin       *EnvironmentTwin
	reg        *registry.Registry
	reconciler *Reconciler
	topRes     *topology.Resolver
	prober     *fingerprint.Prober
	learnStore *learned.Store
	scanner    *discover.Scanner
	logger     *slog.Logger

	mu       sync.Mutex
	cancel   context.CancelFunc
	stopped  chan struct{}
	lastScan time.Time
}

// New creates and wires all maintain sub-systems. The embedded connector
// registry is loaded immediately; the learned store is opened or created.
// Returns an error only for unrecoverable initialisation failures.
func New(cfg Config, logger *slog.Logger) (*Maintainer, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.ReconcileInterval == 0 {
		cfg.ReconcileInterval = 60 * time.Second
	}
	if cfg.ScanInterval == 0 {
		cfg.ScanInterval = 30 * time.Second
	}
	if cfg.LearnedStorePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("maintain: resolve home dir: %w", err)
		}
		cfg.LearnedStorePath = filepath.Join(home, ".nexus", "maintain", "learned", "fixes.json")
	}

	reg, err := registry.LoadEmbedded()
	if err != nil {
		return nil, fmt.Errorf("maintain: load registry: %w", err)
	}

	learnStore, err := learned.NewStore(cfg.LearnedStorePath)
	if err != nil {
		return nil, fmt.Errorf("maintain: open learned store: %w", err)
	}

	twin := NewTwin()
	rec := NewReconciler(twin, reg)

	m := &Maintainer{
		cfg:        cfg,
		twin:       twin,
		reg:        reg,
		reconciler: rec,
		topRes:     topology.NewResolver(),
		prober:     fingerprint.NewProber(),
		learnStore: learnStore,
		scanner:    discover.NewScanner(cfg.ConfigDir, logger),
		logger:     logger,
		stopped:    make(chan struct{}),
	}
	return m, nil
}

// Start launches the background scan and reconciliation loops.
// It returns immediately; loops run until ctx is cancelled.
// Calling Start more than once is a no-op after the first call.
func (m *Maintainer) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return nil // already started
	}
	loopCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock() // release before scan to avoid self-deadlock on m.lastScan update

	// Run an initial scan synchronously so Status() is useful immediately.
	if err := m.scan(loopCtx); err != nil {
		m.logger.WarnContext(loopCtx, "maintain: initial scan failed", "err", err)
	}

	go m.scanLoop(loopCtx)
	go m.reconcileLoop(loopCtx)

	return nil
}

// Stop cancels the background loops and waits for them to exit.
func (m *Maintainer) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Scan runs one discovery scan and refreshes the digital twin. Safe to call
// from CLI commands without starting the background loops.
func (m *Maintainer) Scan(ctx context.Context) error {
	return m.scan(ctx)
}

// Reconcile runs one convergence pass and returns the per-tool results.
// Records each outcome in the learned store.
func (m *Maintainer) Reconcile(ctx context.Context) []ReconcileResult {
	results := m.reconciler.Reconcile(ctx)
	for _, r := range results {
		if r.Skipped || r.IssueID == "" {
			continue
		}
		outcome := learned.OutcomeSuccess
		if r.Err != nil {
			outcome = learned.OutcomeFailure
		}
		m.learnStore.Record(r.Tool, r.IssueID, outcome)
	}
	return results
}

// FixTool runs one targeted convergence attempt for toolName only.
func (m *Maintainer) FixTool(ctx context.Context, toolName string) error {
	ts := m.twin.GetToolState(toolName)
	if ts == nil {
		return fmt.Errorf("maintain: tool %q not tracked (run scan first)", toolName)
	}
	conn, ok := m.reg.ConnectorFor(toolName)
	if !ok {
		return fmt.Errorf("maintain: no connector registered for %q", toolName)
	}
	needsLiveness := ts.Status == "stopped" || ts.Status == "unknown"
	needsConfig := len(ts.Drift) > 0
	if !needsLiveness && !needsConfig {
		return nil // already converged
	}
	issueID := selectIssue(ts, conn)
	if issueID == "" {
		return fmt.Errorf("maintain: no applicable issue found for %q", toolName)
	}
	// Prefer the issue with the best learned weight.
	candidates := make([]string, 0, len(conn.KnownIssues))
	for _, ki := range conn.KnownIssues {
		candidates = append(candidates, ki.ID)
	}
	if len(candidates) > 1 {
		issueID = m.learnStore.BestIssue(toolName, candidates)
	}
	rawSteps := m.reg.RecipeFor(toolName, issueID)
	if len(rawSteps) == 0 {
		return fmt.Errorf("maintain: empty recipe for %q issue %q", toolName, issueID)
	}
	steps := convertSteps(rawSteps, ts, conn)
	tx := NewTransaction(toolName, steps)
	err := tx.Execute(ctx)
	outcome := learned.OutcomeSuccess
	if err != nil {
		outcome = learned.OutcomeFailure
	}
	m.learnStore.Record(toolName, issueID, outcome)
	return err
}

// Status returns a point-in-time snapshot for CLI display.
func (m *Maintainer) Status() MaintainStatus {
	tools := m.twin.AllTools()
	rows := make([]ToolStatusRow, 0, len(tools))
	for _, ts := range tools {
		rows = append(rows, ToolStatusRow{
			Name:   ts.Name,
			Status: ts.Status,
			Drift:  len(ts.Drift),
			Health: ts.Health.Reachable,
		})
	}
	return MaintainStatus{
		Platform:     m.twin.Platform(),
		Tools:        rows,
		TopologySet:  m.twin.Topology() != nil,
		LearnedCount: m.learnStore.Len(),
		LastScan:     m.lastScan,
	}
}

// Twin returns the live EnvironmentTwin (for read-only inspection by the daemon).
func (m *Maintainer) Twin() *EnvironmentTwin { return m.twin }

// Registry returns the connector registry.
func (m *Maintainer) Registry() *registry.Registry { return m.reg }

// scan runs one discovery + twin refresh + topology resolve cycle.
func (m *Maintainer) scan(ctx context.Context) error {
	tools, err := m.scanner.FullScan(ctx)
	if err != nil {
		return fmt.Errorf("maintain: scan: %w", err)
	}
	m.twin.Refresh(ctx, tools)

	top, err := m.topRes.Resolve(ctx)
	if err == nil {
		m.twin.SetTopology(top)
	}

	m.mu.Lock()
	m.lastScan = time.Now().UTC()
	m.mu.Unlock()

	m.logger.InfoContext(ctx, "maintain: scan complete",
		"tools", len(tools), "platform", m.twin.Platform())
	return nil
}

func (m *Maintainer) scanLoop(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.scan(ctx); err != nil {
				m.logger.WarnContext(ctx, "maintain: scan error", "err", err)
			}
		}
	}
}

func (m *Maintainer) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.ReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			results := m.Reconcile(ctx)
			fixed, failed := 0, 0
			for _, r := range results {
				if r.Skipped {
					continue
				}
				if r.Err != nil {
					failed++
				} else {
					fixed++
				}
			}
			if fixed+failed > 0 {
				m.logger.InfoContext(ctx, "maintain: reconcile pass",
					"fixed", fixed, "failed", failed)
			}
		}
	}
}
