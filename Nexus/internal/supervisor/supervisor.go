// Copyright © 2026 BubbleFish Technologies, Inc.

// Package supervisor implements a goroutine heartbeat supervisor with
// deadlock self-kill. It monitors registered goroutines via periodic
// Beat() calls and terminates the process if any goroutine stalls beyond
// the configured timeout.
//
// On stall detection:
//  1. Logs level=fatal with the stalled goroutine name and last heartbeat age.
//  2. Dumps all goroutine stacks to logs/deadlock-<unix-timestamp>.log.
//  3. Calls os.Exit(3) — exit code 3 is reserved for supervisor-induced
//     termination so systemd/launchd/nssm can distinguish it.
//
// During graceful shutdown, monitoring stops to avoid false positives on
// goroutines that are legitimately draining.
package supervisor

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// exitFunc is the function called to terminate the process. Replaced in tests.
var exitFunc = os.Exit

// Supervisor monitors goroutine liveness via heartbeats. A single long-lived
// goroutine checks all registered goroutines every checkInterval seconds.
// If time.Since(lastBeat) exceeds timeout, the supervisor kills the process.
type Supervisor struct {
	mu           sync.Mutex
	heartbeats   map[string]time.Time
	timeout      time.Duration
	checkInterval time.Duration
	logger       *slog.Logger
	logsDir      string // directory for deadlock stack dumps
	stopCh       chan struct{}
	stopped      chan struct{}
	once         sync.Once
	shutdown     bool // set during graceful shutdown
}

// Config configures a Supervisor instance.
type Config struct {
	// Timeout is the maximum time between heartbeats before declaring a stall.
	// Production default: 30s. Test-only: 500ms.
	Timeout time.Duration

	// CheckInterval is how often the supervisor checks heartbeats.
	// Defaults to Timeout / 6 (5s for 30s timeout).
	CheckInterval time.Duration

	// LogsDir is the directory for deadlock stack dump files.
	// Defaults to "logs" relative to working directory.
	LogsDir string
}

// New creates a new Supervisor. Call Start() to begin monitoring.
func New(cfg Config, logger *slog.Logger) *Supervisor {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = cfg.Timeout / 6
	}
	if cfg.LogsDir == "" {
		cfg.LogsDir = "logs"
	}
	return &Supervisor{
		heartbeats:    make(map[string]time.Time),
		timeout:       cfg.Timeout,
		checkInterval: cfg.CheckInterval,
		logger:        logger,
		logsDir:       cfg.LogsDir,
		stopCh:        make(chan struct{}),
		stopped:       make(chan struct{}),
	}
}

// Register adds a goroutine to the monitoring set with an initial heartbeat
// at the current time. Must be called before Start() or concurrently with
// Beat() from the monitored goroutine.
func (s *Supervisor) Register(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeats[name] = time.Now()
}

// Beat updates the last heartbeat time for the named goroutine. This is a
// non-blocking call that takes a mutex briefly to update the map. Monitored
// goroutines should call this at least once per second during normal operation.
func (s *Supervisor) Beat(name string) {
	s.mu.Lock()
	s.heartbeats[name] = time.Now()
	s.mu.Unlock()
}

// Start begins the monitoring goroutine. Call Stop() during graceful shutdown.
func (s *Supervisor) Start() {
	go s.run()
}

// Shutdown marks the supervisor as shutting down. Stall detection is
// suppressed after this call to avoid false positives during graceful drain.
// Call this before stopping monitored goroutines.
func (s *Supervisor) Shutdown() {
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()
}

// Stop stops the monitoring goroutine. Safe to call multiple times.
func (s *Supervisor) Stop() {
	s.once.Do(func() {
		close(s.stopCh)
	})
	<-s.stopped
}

func (s *Supervisor) run() {
	defer close(s.stopped)

	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.check()
		}
	}
}

func (s *Supervisor) check() {
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return
	}

	now := time.Now()
	var stalled string
	var stalledAge time.Duration

	for name, last := range s.heartbeats {
		age := now.Sub(last)
		if age > s.timeout {
			stalled = name
			stalledAge = age
			break
		}
	}
	s.mu.Unlock()

	if stalled == "" {
		return
	}

	// Stall detected. Fatal sequence: log, dump, exit.
	s.logger.Error("supervisor: goroutine stall detected — initiating self-kill",
		"level", "fatal",
		"component", "supervisor",
		"stalled_goroutine", stalled,
		"last_heartbeat_age", stalledAge.String(),
	)

	s.dumpStacks(stalled)
	exitFunc(3)
}

// dumpStacks writes all goroutine stacks to logs/deadlock-<timestamp>.log.
func (s *Supervisor) dumpStacks(stalledName string) {
	_ = os.MkdirAll(s.logsDir, 0700)

	filename := fmt.Sprintf("deadlock-%d.log", time.Now().Unix())
	path := filepath.Join(s.logsDir, filename)

	// Capture all goroutine stacks.
	buf := make([]byte, 1<<20) // 1MB initial buffer
	n := runtime.Stack(buf, true)
	if n == len(buf) {
		// Buffer was full — try a larger one.
		buf = make([]byte, 4<<20) // 4MB
		n = runtime.Stack(buf, true)
	}

	header := fmt.Sprintf("Supervisor deadlock dump\nStalled goroutine: %s\nTimestamp: %s\n\n",
		stalledName, time.Now().UTC().Format(time.RFC3339Nano))

	content := header + string(buf[:n])

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		s.logger.Error("supervisor: failed to write stack dump",
			"component", "supervisor",
			"path", path,
			"error", err,
		)
		return
	}

	s.logger.Error("supervisor: goroutine stack dump written",
		"component", "supervisor",
		"path", path,
		"size_bytes", len(content),
	)
}
