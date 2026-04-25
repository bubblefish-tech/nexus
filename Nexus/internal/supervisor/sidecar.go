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

package supervisor

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// SidecarConfig configures the sidecar supervisor.
type SidecarConfig struct {
	// TierConfig configures the degradation state machine.
	TierConfig TierMachineConfig

	// HeartbeatInterval is how often the daemon must send a heartbeat.
	// Default: 5s.
	HeartbeatInterval time.Duration

	// HeartbeatTimeout is how long without a heartbeat before the supervisor
	// considers the daemon stalled. Default: 15s.
	HeartbeatTimeout time.Duration

	// MaxRestartDelay is the maximum backoff between restart attempts.
	// Default: 60s.
	MaxRestartDelay time.Duration
}

// DefaultSidecarConfig returns production defaults.
func DefaultSidecarConfig() SidecarConfig {
	return SidecarConfig{
		TierConfig:        DefaultTierMachineConfig(),
		HeartbeatInterval: 5 * time.Second,
		HeartbeatTimeout:  15 * time.Second,
		MaxRestartDelay:   60 * time.Second,
	}
}

// DaemonSpawner is the function signature for starting a daemon process.
// It receives the current degradation tier and a pipe for communication.
// It should block until the daemon exits and return any error.
type DaemonSpawner func(ctx context.Context, tier DegradationTier, pipe Pipe) error

// Sidecar is the two-process supervisor that monitors a daemon via a local pipe.
// It implements tiered degradation: T0 instant restart, T1 reduced features,
// T2 read-only, T3 emergency shutdown.
type Sidecar struct {
	cfg     SidecarConfig
	logger  *slog.Logger
	machine *TierMachine
	spawner DaemonSpawner

	mu            sync.Mutex
	lastHeartbeat time.Time
	daemonReady   bool
	restartDelay  time.Duration

	stopOnce sync.Once
	stopCh   chan struct{}
	stopped  chan struct{}
}

// NewSidecar creates a new sidecar supervisor.
func NewSidecar(cfg SidecarConfig, spawner DaemonSpawner, logger *slog.Logger) *Sidecar {
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 5 * time.Second
	}
	if cfg.HeartbeatTimeout <= 0 {
		cfg.HeartbeatTimeout = 15 * time.Second
	}
	if cfg.MaxRestartDelay <= 0 {
		cfg.MaxRestartDelay = 60 * time.Second
	}

	return &Sidecar{
		cfg:          cfg,
		logger:       logger,
		machine:      NewTierMachine(cfg.TierConfig),
		spawner:      spawner,
		restartDelay: time.Second,
		stopCh:       make(chan struct{}),
		stopped:      make(chan struct{}),
	}
}

// Run starts the supervisor loop. It blocks until Stop() is called or
// the machine reaches TierEmergencyShutdown. Returns nil on clean shutdown.
func (s *Sidecar) Run(ctx context.Context) error {
	defer close(s.stopped)

	for {
		tier := s.machine.Current()
		if tier == TierEmergencyShutdown {
			s.logger.Error("sidecar: emergency shutdown — giving up",
				"component", "sidecar",
				"transitions", len(s.machine.Transitions()),
			)
			return errors.New("emergency shutdown: too many failures")
		}

		select {
		case <-s.stopCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		s.logger.Info("sidecar: starting daemon",
			"component", "sidecar",
			"tier", tier.String(),
			"restart_delay", s.restartDelay,
		)

		err := s.runOneCycle(ctx, tier)
		if err == nil {
			// Clean daemon exit.
			s.machine.RecordSuccess()
			s.restartDelay = time.Second
			s.logger.Info("sidecar: daemon exited cleanly",
				"component", "sidecar",
			)

			select {
			case <-s.stopCh:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			continue
		}

		newTier := s.machine.RecordFailure(err.Error())
		s.logger.Warn("sidecar: daemon failed — escalating",
			"component", "sidecar",
			"error", err,
			"new_tier", newTier.String(),
		)

		if newTier == TierEmergencyShutdown {
			continue // will exit at top of loop
		}

		// Backoff before restart.
		select {
		case <-s.stopCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.restartDelay):
		}

		// Increase backoff up to max.
		s.restartDelay *= 2
		if s.restartDelay > s.cfg.MaxRestartDelay {
			s.restartDelay = s.cfg.MaxRestartDelay
		}
	}
}

// runOneCycle runs a single daemon lifecycle with heartbeat monitoring.
func (s *Sidecar) runOneCycle(ctx context.Context, tier DegradationTier) error {
	pipeA, pipeB := pipePair()
	defer pipeA.Close()

	daemonCtx, daemonCancel := context.WithCancel(ctx)
	defer daemonCancel()

	// Reset heartbeat state.
	s.mu.Lock()
	s.lastHeartbeat = time.Now()
	s.daemonReady = false
	s.mu.Unlock()

	// Start daemon in a goroutine.
	daemonErr := make(chan error, 1)
	go func() {
		daemonErr <- s.spawner(daemonCtx, tier, pipeB)
		pipeB.Close()
	}()

	// Monitor heartbeats from the daemon.
	recvErr := make(chan error, 1)
	go func() {
		for {
			msg, err := pipeA.Recv()
			if err != nil {
				if errors.Is(err, ErrPipeClosed) {
					recvErr <- nil
				} else {
					recvErr <- err
				}
				return
			}
			s.handleDaemonMsg(msg)
		}
	}()

	// Heartbeat watchdog ticker.
	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			// Supervisor is stopping — cancel daemon context to trigger exit.
			daemonCancel()
			// Send shutdown hint after cancel (non-blocking: daemon may already be gone).
			go func() {
				_ = pipeA.Send(PipeMsg{
					Type:      PipeMsgShutdown,
					Timestamp: time.Now(),
				})
			}()
			return <-daemonErr

		case err := <-daemonErr:
			return err

		case <-recvErr:
			// Pipe closed or error — daemon probably crashed.
			daemonCancel()
			return <-daemonErr

		case <-ticker.C:
			s.mu.Lock()
			age := time.Since(s.lastHeartbeat)
			ready := s.daemonReady
			s.mu.Unlock()

			if ready && age > s.cfg.HeartbeatTimeout {
				s.logger.Warn("sidecar: heartbeat timeout — killing daemon",
					"component", "sidecar",
					"last_heartbeat_age", age,
				)
				daemonCancel()
				return <-daemonErr
			}
		}
	}
}

// handleDaemonMsg processes a message received from the daemon.
func (s *Sidecar) handleDaemonMsg(msg PipeMsg) {
	switch msg.Type {
	case PipeMsgHeartbeat:
		s.mu.Lock()
		s.lastHeartbeat = time.Now()
		s.mu.Unlock()

	case PipeMsgReady:
		s.mu.Lock()
		s.lastHeartbeat = time.Now()
		s.daemonReady = true
		s.mu.Unlock()
		s.logger.Info("sidecar: daemon reports ready",
			"component", "sidecar",
		)

	case PipeMsgError:
		s.logger.Warn("sidecar: daemon reported error",
			"component", "sidecar",
			"payload", msg.Payload,
		)
	}
}

// Stop signals the supervisor to shut down. Safe to call multiple times.
func (s *Sidecar) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	<-s.stopped
}

// CurrentTier returns the current degradation tier.
func (s *Sidecar) CurrentTier() DegradationTier {
	return s.machine.Current()
}

// Machine returns the underlying tier machine for inspection.
func (s *Sidecar) Machine() *TierMachine {
	return s.machine
}
