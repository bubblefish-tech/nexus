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

// Package supervisor provides in-process supervision tree, sidecar supervision,
// and goroutine heartbeat monitoring.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sony/gobreaker/v2"
)

// ChildSpec defines a supervised child process/goroutine.
type ChildSpec struct {
	// Name uniquely identifies this child within the tree.
	Name string

	// Start is the function that runs the child. It should block until
	// the child finishes or the context is cancelled.
	Start func(ctx context.Context) error

	// RestartPolicy controls how the child is restarted on failure.
	RestartPolicy RestartPolicy

	// BreakerConfig configures the circuit breaker for this child.
	// Nil means use DefaultBreakerConfig().
	BreakerConfig *BreakerConfig
}

// RestartPolicy controls restart behavior for a supervised child.
type RestartPolicy int

const (
	// RestartAlways restarts the child whenever it exits (error or not).
	RestartAlways RestartPolicy = iota
	// RestartOnFailure restarts the child only when it exits with an error.
	RestartOnFailure
	// RestartNever does not restart the child.
	RestartNever
)

// String returns a human-readable representation.
func (p RestartPolicy) String() string {
	switch p {
	case RestartAlways:
		return "always"
	case RestartOnFailure:
		return "on_failure"
	case RestartNever:
		return "never"
	default:
		return fmt.Sprintf("unknown(%d)", int(p))
	}
}

// BreakerConfig configures a circuit breaker for a supervised child.
type BreakerConfig struct {
	// MaxFailures is the number of failures in the window before the breaker opens.
	// Default: 5.
	MaxFailures uint32

	// Window is the time window for counting failures.
	// Default: 60s.
	Window time.Duration

	// OpenTimeout is how long the breaker stays open before transitioning to half-open.
	// Default: 30s.
	OpenTimeout time.Duration
}

// DefaultBreakerConfig returns production defaults for the circuit breaker.
func DefaultBreakerConfig() BreakerConfig {
	return BreakerConfig{
		MaxFailures: 5,
		Window:      60 * time.Second,
		OpenTimeout: 30 * time.Second,
	}
}

// childState tracks the runtime state of a supervised child.
type childState struct {
	spec    ChildSpec
	breaker *gobreaker.CircuitBreaker[struct{}]
	cancel  context.CancelFunc
	done    chan struct{}
	err     error
	mu      sync.Mutex
}

// TreeConfig configures the supervision tree.
type TreeConfig struct {
	// MaxRestartIntensity is the maximum number of restarts allowed in
	// RestartWindow before the tree shuts down. Default: 10.
	MaxRestartIntensity int

	// RestartWindow is the time window for counting restarts.
	// Default: 60s.
	RestartWindow time.Duration

	// ShutdownTimeout is the maximum time to wait for children to stop
	// during shutdown. Default: 30s.
	ShutdownTimeout time.Duration
}

// DefaultTreeConfig returns production defaults.
func DefaultTreeConfig() TreeConfig {
	return TreeConfig{
		MaxRestartIntensity: 10,
		RestartWindow:       60 * time.Second,
		ShutdownTimeout:     30 * time.Second,
	}
}

// Tree is an Erlang-style supervision tree that manages child goroutines
// with circuit breakers. Children are started in order and stopped in
// reverse order.
type Tree struct {
	cfg      TreeConfig
	logger   *slog.Logger
	children []*childState

	mu       sync.Mutex
	restarts []time.Time

	stopOnce sync.Once
	stopCh   chan struct{}
	stopped  chan struct{}

	nowFunc func() time.Time // injectable clock for testing
}

// NewTree creates a new supervision tree with the given children.
// Children are started in the order they appear in specs.
func NewTree(cfg TreeConfig, specs []ChildSpec, logger *slog.Logger) *Tree {
	if cfg.MaxRestartIntensity <= 0 {
		cfg.MaxRestartIntensity = 10
	}
	if cfg.RestartWindow <= 0 {
		cfg.RestartWindow = 60 * time.Second
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}

	children := make([]*childState, len(specs))
	for i, spec := range specs {
		bcfg := DefaultBreakerConfig()
		if spec.BreakerConfig != nil {
			bcfg = *spec.BreakerConfig
		}

		cb := gobreaker.NewCircuitBreaker[struct{}](gobreaker.Settings{
			Name:     spec.Name,
			Interval: bcfg.Window,
			Timeout:  bcfg.OpenTimeout,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return counts.ConsecutiveFailures >= bcfg.MaxFailures
			},
		})

		children[i] = &childState{
			spec:    spec,
			breaker: cb,
		}
	}

	return &Tree{
		cfg:      cfg,
		logger:   logger,
		children: children,
		stopCh:   make(chan struct{}),
		stopped:  make(chan struct{}),
		nowFunc:  time.Now,
	}
}

// Run starts all children and blocks until Stop() is called or the tree
// exceeds its restart intensity. Returns nil on clean shutdown.
func (t *Tree) Run(ctx context.Context) error {
	defer close(t.stopped)

	treeCtx, treeCancel := context.WithCancel(ctx)
	defer treeCancel()

	// Start all children.
	for _, cs := range t.children {
		t.startChild(treeCtx, cs)
	}

	// Monitor for failures and handle restarts.
	for {
		select {
		case <-t.stopCh:
			t.shutdownAll()
			return nil

		case <-treeCtx.Done():
			t.shutdownAll()
			return treeCtx.Err()

		default:
		}

		// Check for any child that has finished.
		finished, err := t.waitForAnyChild(treeCtx)
		if finished == nil {
			// Context was cancelled or stop was called.
			t.shutdownAll()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}

		t.logger.Info("supervision tree: child exited",
			"component", "tree",
			"child", finished.spec.Name,
			"error", err,
		)

		shouldRestart := false
		switch finished.spec.RestartPolicy {
		case RestartAlways:
			shouldRestart = true
		case RestartOnFailure:
			shouldRestart = err != nil
		case RestartNever:
			shouldRestart = false
		}

		if !shouldRestart {
			continue
		}

		// Check restart intensity.
		if t.exceedsRestartIntensity() {
			t.logger.Error("supervision tree: restart intensity exceeded — shutting down",
				"component", "tree",
				"max_restarts", t.cfg.MaxRestartIntensity,
				"window", t.cfg.RestartWindow,
			)
			t.shutdownAll()
			return errors.New("supervision tree: restart intensity exceeded")
		}

		// Check circuit breaker.
		_, cbErr := finished.breaker.Execute(func() (struct{}, error) {
			return struct{}{}, errors.New("child failed")
		})

		if errors.Is(cbErr, gobreaker.ErrOpenState) || errors.Is(cbErr, gobreaker.ErrTooManyRequests) {
			t.logger.Warn("supervision tree: circuit breaker open — deferring restart",
				"component", "tree",
				"child", finished.spec.Name,
				"breaker_state", finished.breaker.State().String(),
			)
			// Wait for the breaker to transition to half-open, then retry.
			go t.deferredRestart(treeCtx, finished)
			continue
		}

		// Restart the child.
		t.startChild(treeCtx, finished)
	}
}

// waitForAnyChild blocks until any child finishes or the context is done.
// Returns the finished child and its error, or nil if context was cancelled.
func (t *Tree) waitForAnyChild(ctx context.Context) (*childState, error) {
	// Build a list of done channels.
	cases := make([]<-chan struct{}, 0, len(t.children)+2)
	caseMap := make([]int, 0, len(t.children)+2)

	for i, cs := range t.children {
		cs.mu.Lock()
		if cs.done != nil {
			cases = append(cases, cs.done)
			caseMap = append(caseMap, i)
		}
		cs.mu.Unlock()
	}

	if len(cases) == 0 {
		// No active children — wait for stop signal.
		select {
		case <-t.stopCh:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Use a polling approach to avoid reflect.Select overhead.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			for j, ch := range cases {
				select {
				case <-ch:
					cs := t.children[caseMap[j]]
					cs.mu.Lock()
					err := cs.err
					cs.mu.Unlock()
					return cs, err
				default:
				}
			}
		}
	}
}

// startChild starts a child goroutine and tracks its lifecycle.
func (t *Tree) startChild(ctx context.Context, cs *childState) {
	cs.mu.Lock()
	childCtx, cancel := context.WithCancel(ctx)
	cs.cancel = cancel
	cs.done = make(chan struct{})
	cs.err = nil
	cs.mu.Unlock()

	done := cs.done
	go func() {
		defer close(done)
		err := cs.spec.Start(childCtx)
		cs.mu.Lock()
		cs.err = err
		cs.mu.Unlock()
	}()

	t.mu.Lock()
	t.restarts = append(t.restarts, t.nowFunc())
	t.mu.Unlock()

	t.logger.Info("supervision tree: child started",
		"component", "tree",
		"child", cs.spec.Name,
		"policy", cs.spec.RestartPolicy.String(),
	)
}

// deferredRestart waits for the circuit breaker to allow retries, then
// restarts the child.
func (t *Tree) deferredRestart(ctx context.Context, cs *childState) {
	bcfg := DefaultBreakerConfig()
	if cs.spec.BreakerConfig != nil {
		bcfg = *cs.spec.BreakerConfig
	}

	// Wait for the breaker's open timeout before attempting restart.
	select {
	case <-time.After(bcfg.OpenTimeout):
	case <-ctx.Done():
		return
	case <-t.stopCh:
		return
	}

	t.logger.Info("supervision tree: circuit breaker half-open — restarting child",
		"component", "tree",
		"child", cs.spec.Name,
	)

	t.startChild(ctx, cs)
}

// exceedsRestartIntensity checks whether the restart rate exceeds the
// configured intensity.
func (t *Tree) exceedsRestartIntensity() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.nowFunc()
	cutoff := now.Add(-t.cfg.RestartWindow)

	var recent []time.Time
	for _, r := range t.restarts {
		if r.After(cutoff) {
			recent = append(recent, r)
		}
	}
	t.restarts = recent

	return len(t.restarts) > t.cfg.MaxRestartIntensity
}

// shutdownAll stops all children in reverse order.
func (t *Tree) shutdownAll() {
	for i := len(t.children) - 1; i >= 0; i-- {
		cs := t.children[i]
		cs.mu.Lock()
		if cs.cancel != nil {
			cs.cancel()
		}
		done := cs.done
		cs.mu.Unlock()

		if done != nil {
			select {
			case <-done:
			case <-time.After(t.cfg.ShutdownTimeout):
				t.logger.Warn("supervision tree: child shutdown timed out",
					"component", "tree",
					"child", cs.spec.Name,
				)
			}
		}
	}
}

// Stop signals the tree to shut down. Safe to call multiple times.
func (t *Tree) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
	})
	<-t.stopped
}

// ChildStatus holds the current status of a supervised child.
type ChildStatus struct {
	Name          string
	RestartPolicy RestartPolicy
	BreakerState  string
	Running       bool
	LastError     error
}

// Status returns the current status of all children.
func (t *Tree) Status() []ChildStatus {
	statuses := make([]ChildStatus, len(t.children))
	for i, cs := range t.children {
		cs.mu.Lock()
		running := cs.done != nil
		if running {
			// Check if the done channel is closed.
			select {
			case <-cs.done:
				running = false
			default:
			}
		}
		lastErr := cs.err
		cs.mu.Unlock()

		statuses[i] = ChildStatus{
			Name:          cs.spec.Name,
			RestartPolicy: cs.spec.RestartPolicy,
			BreakerState:  cs.breaker.State().String(),
			Running:       running,
			LastError:     lastErr,
		}
	}
	return statuses
}
