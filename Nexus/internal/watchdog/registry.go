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

// Package watchdog implements per-subsystem watchdogs with configurable
// timeouts. Each subsystem registers with a central WatchdogRegistry and
// reports health via periodic Beat() calls. The registry checks all
// registered subsystems on a configurable interval and reports aggregate
// health status.
//
// Unlike the top-level supervisor (which kills the process on stall),
// the watchdog registry reports degraded subsystems without terminating.
// The supervisor can optionally consume watchdog status to make its
// own kill decisions.
//
// Default timeouts per subsystem:
//
//	heartbeat: 10s
//	embedding: 30s
//	wal:        5s
//	mcp:       15s
package watchdog

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// SubsystemStatus represents the health status of a single subsystem.
type SubsystemStatus int

const (
	// StatusHealthy indicates the subsystem is beating within its timeout.
	StatusHealthy SubsystemStatus = iota
	// StatusDegraded indicates the subsystem has missed its timeout deadline.
	StatusDegraded
	// StatusUnknown indicates the subsystem has not yet reported a heartbeat.
	StatusUnknown
)

// String returns a human-readable status label.
func (s SubsystemStatus) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusDegraded:
		return "degraded"
	case StatusUnknown:
		return "unknown"
	default:
		return fmt.Sprintf("SubsystemStatus(%d)", int(s))
	}
}

// SubsystemReport is the health report for a single registered subsystem.
type SubsystemReport struct {
	Name     string
	Status   SubsystemStatus
	LastBeat time.Time
	Timeout  time.Duration
	Age      time.Duration // time since last beat (zero if never beat)
}

// RegistryConfig configures the WatchdogRegistry.
type RegistryConfig struct {
	// CheckInterval is how often the registry checks all subsystems.
	// Defaults to 2 seconds.
	CheckInterval time.Duration

	// DefaultTimeout is the timeout used when a subsystem is registered
	// without specifying its own. Defaults to 10 seconds.
	DefaultTimeout time.Duration
}

// DefaultTimeouts maps well-known subsystem names to their default timeouts.
// These are used when a subsystem is registered without an explicit timeout
// and match the daemon.toml [watchdog] defaults.
var DefaultTimeouts = map[string]time.Duration{
	"heartbeat": 10 * time.Second,
	"embedding": 30 * time.Second,
	"wal":       5 * time.Second,
	"mcp":       15 * time.Second,
}

// subsystemEntry is the internal bookkeeping for a registered subsystem.
type subsystemEntry struct {
	name     string
	timeout  time.Duration
	lastBeat time.Time
	hasBeat  bool // true after first Beat() call
}

// WatchdogRegistry monitors per-subsystem health via heartbeats.
// All methods are safe for concurrent use.
type WatchdogRegistry struct {
	mu            sync.RWMutex
	subsystems    map[string]*subsystemEntry
	cfg           RegistryConfig
	logger        *slog.Logger
	stopCh        chan struct{}
	stopped       chan struct{}
	once          sync.Once
	shutdown      bool
	onDegraded    func(name string, age time.Duration) // optional callback on degradation
}

// New creates a new WatchdogRegistry. Call Start() to begin monitoring.
func New(cfg RegistryConfig, logger *slog.Logger) *WatchdogRegistry {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 2 * time.Second
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 10 * time.Second
	}
	return &WatchdogRegistry{
		subsystems: make(map[string]*subsystemEntry),
		cfg:        cfg,
		logger:     logger,
		stopCh:     make(chan struct{}),
		stopped:    make(chan struct{}),
	}
}

// Register adds a subsystem to the monitoring set with the given timeout.
// If timeout is zero, the default timeout for the subsystem name is used
// (from DefaultTimeouts), or RegistryConfig.DefaultTimeout as a fallback.
//
// Register is safe to call before or after Start(). Registering a name
// that already exists overwrites the timeout but preserves the last beat.
func (r *WatchdogRegistry) Register(name string, timeout time.Duration) {
	if timeout <= 0 {
		if dt, ok := DefaultTimeouts[name]; ok {
			timeout = dt
		} else {
			timeout = r.cfg.DefaultTimeout
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.subsystems[name]; ok {
		// Preserve last beat, update timeout.
		existing.timeout = timeout
		return
	}

	r.subsystems[name] = &subsystemEntry{
		name:    name,
		timeout: timeout,
	}
}

// Unregister removes a subsystem from the monitoring set.
// No-op if the subsystem is not registered.
func (r *WatchdogRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.subsystems, name)
}

// Beat updates the last heartbeat time for the named subsystem.
// If the subsystem is not registered, the call is silently ignored.
func (r *WatchdogRegistry) Beat(name string) {
	r.mu.Lock()
	entry, ok := r.subsystems[name]
	if ok {
		entry.lastBeat = time.Now()
		entry.hasBeat = true
	}
	r.mu.Unlock()
}

// Status returns the health report for a single subsystem.
// Returns nil if the subsystem is not registered.
func (r *WatchdogRegistry) Status(name string) *SubsystemReport {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.subsystems[name]
	if !ok {
		return nil
	}

	return r.buildReport(entry, time.Now())
}

// AllStatus returns health reports for all registered subsystems,
// sorted by name.
func (r *WatchdogRegistry) AllStatus() []SubsystemReport {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	reports := make([]SubsystemReport, 0, len(r.subsystems))
	for _, entry := range r.subsystems {
		reports = append(reports, *r.buildReport(entry, now))
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Name < reports[j].Name
	})
	return reports
}

// IsHealthy returns true if all registered subsystems are healthy.
func (r *WatchdogRegistry) IsHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	for _, entry := range r.subsystems {
		if r.entryStatus(entry, now) != StatusHealthy {
			return false
		}
	}
	return true
}

// DegradedSubsystems returns the names of all degraded subsystems.
func (r *WatchdogRegistry) DegradedSubsystems() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	var degraded []string
	for _, entry := range r.subsystems {
		if r.entryStatus(entry, now) != StatusHealthy {
			degraded = append(degraded, entry.name)
		}
	}
	sort.Strings(degraded)
	return degraded
}

// OnDegraded sets an optional callback invoked when a subsystem transitions
// to degraded status during a check cycle. The callback receives the
// subsystem name and the age of the last heartbeat.
func (r *WatchdogRegistry) OnDegraded(fn func(name string, age time.Duration)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onDegraded = fn
}

// Shutdown marks the registry as shutting down. After this call,
// periodic checks are suppressed to avoid false positives during
// graceful drain.
func (r *WatchdogRegistry) Shutdown() {
	r.mu.Lock()
	r.shutdown = true
	r.mu.Unlock()
}

// Start begins the periodic monitoring goroutine.
func (r *WatchdogRegistry) Start() {
	go r.run()
}

// Stop stops the monitoring goroutine. Safe to call multiple times.
func (r *WatchdogRegistry) Stop() {
	r.once.Do(func() {
		close(r.stopCh)
	})
	<-r.stopped
}

// Count returns the number of registered subsystems.
func (r *WatchdogRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.subsystems)
}

func (r *WatchdogRegistry) run() {
	defer close(r.stopped)

	ticker := time.NewTicker(r.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.check()
		}
	}
}

func (r *WatchdogRegistry) check() {
	r.mu.RLock()
	if r.shutdown {
		r.mu.RUnlock()
		return
	}

	now := time.Now()
	type degradedInfo struct {
		name string
		age  time.Duration
	}
	var degradedList []degradedInfo

	for _, entry := range r.subsystems {
		status := r.entryStatus(entry, now)
		if status == StatusDegraded {
			age := now.Sub(entry.lastBeat)
			degradedList = append(degradedList, degradedInfo{name: entry.name, age: age})
		}
	}

	var onDegraded func(string, time.Duration)
	if len(degradedList) > 0 {
		onDegraded = r.onDegraded
	}
	r.mu.RUnlock()

	// Log and notify outside the lock.
	for _, d := range degradedList {
		r.logger.Warn("watchdog: subsystem degraded",
			"component", "watchdog",
			"subsystem", d.name,
			"last_heartbeat_age", d.age.String(),
		)
		if onDegraded != nil {
			onDegraded(d.name, d.age)
		}
	}
}

// buildReport creates a SubsystemReport from an entry. Caller must hold r.mu.
func (r *WatchdogRegistry) buildReport(entry *subsystemEntry, now time.Time) *SubsystemReport {
	status := r.entryStatus(entry, now)
	var age time.Duration
	if entry.hasBeat {
		age = now.Sub(entry.lastBeat)
	}
	return &SubsystemReport{
		Name:     entry.name,
		Status:   status,
		LastBeat: entry.lastBeat,
		Timeout:  entry.timeout,
		Age:      age,
	}
}

// entryStatus computes the status for an entry. Caller must hold r.mu.
func (r *WatchdogRegistry) entryStatus(entry *subsystemEntry, now time.Time) SubsystemStatus {
	if !entry.hasBeat {
		return StatusUnknown
	}
	if now.Sub(entry.lastBeat) > entry.timeout {
		return StatusDegraded
	}
	return StatusHealthy
}
