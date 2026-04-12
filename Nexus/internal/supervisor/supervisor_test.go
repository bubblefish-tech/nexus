// Copyright © 2026 BubbleFish Technologies, Inc.

package supervisor

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testConfig returns a Config with a short timeout for fast test execution.
func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Timeout:       500 * time.Millisecond,
		CheckInterval: 100 * time.Millisecond,
		LogsDir:       filepath.Join(t.TempDir(), "logs"),
	}
}

// ── Normal heartbeat ────────────────────────────────────────────────────────

// TestSupervisor_NormalHeartbeat starts a supervisor, registers a goroutine
// that beats every 100ms, waits 2 seconds, and verifies no fatal exit.
func TestSupervisor_NormalHeartbeat(t *testing.T) {
	var exitCalled atomic.Int32
	origExit := exitFunc
	exitFunc = func(code int) { exitCalled.Store(int32(code)) }
	defer func() { exitFunc = origExit }()

	cfg := testConfig(t)
	s := New(cfg, testLogger())
	s.Register("healthy")
	s.Start()

	// Beat every 100ms for 2 seconds.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			s.Beat("healthy")
			time.Sleep(100 * time.Millisecond)
		}
	}()

	<-done
	s.Shutdown()
	s.Stop()

	if exitCalled.Load() != 0 {
		t.Errorf("exit was called with code %d; want no exit", exitCalled.Load())
	}
}

// ── Stall detection ─────────────────────────────────────────────────────────

// TestSupervisor_StallDetection registers a goroutine, stops beating, and
// verifies the supervisor detects the stall and calls exit(3).
func TestSupervisor_StallDetection(t *testing.T) {
	var exitCalled atomic.Int32
	var exitMu sync.Mutex
	exitCh := make(chan struct{}, 1)

	origExit := exitFunc
	exitFunc = func(code int) {
		exitMu.Lock()
		defer exitMu.Unlock()
		exitCalled.Store(int32(code))
		select {
		case exitCh <- struct{}{}:
		default:
		}
	}
	defer func() { exitFunc = origExit }()

	cfg := testConfig(t)
	s := New(cfg, testLogger())
	s.Register("staller")
	// Beat once at registration time, then never again.
	s.Start()

	// Wait for the supervisor to detect the stall (timeout + check interval).
	select {
	case <-exitCh:
		// Expected.
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not detect stall within 3 seconds")
	}

	if got := exitCalled.Load(); got != 3 {
		t.Errorf("exit code: want 3, got %d", got)
	}

	// Clean up: stop the supervisor (it may still be running since exit is mocked).
	s.Shutdown()
	s.Stop()
}

// ── Stack dump ──────────────────────────────────────────────────────────────

// TestSupervisor_StackDump triggers stall detection and verifies the stack
// dump file is written and contains expected content.
func TestSupervisor_StackDump(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "logs")

	exitCh := make(chan struct{}, 1)
	origExit := exitFunc
	exitFunc = func(code int) {
		select {
		case exitCh <- struct{}{}:
		default:
		}
	}
	defer func() { exitFunc = origExit }()

	cfg := Config{
		Timeout:       500 * time.Millisecond,
		CheckInterval: 100 * time.Millisecond,
		LogsDir:       logsDir,
	}
	s := New(cfg, testLogger())
	s.Register("dump-target")
	s.Start()

	select {
	case <-exitCh:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not detect stall within 3 seconds")
	}

	s.Shutdown()
	s.Stop()

	// Verify stack dump file exists and has content.
	files, err := filepath.Glob(filepath.Join(logsDir, "deadlock-*.log"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no deadlock dump file found")
	}

	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "dump-target") {
		t.Error("stack dump does not contain stalled goroutine name")
	}
	if !strings.Contains(content, "goroutine") {
		t.Error("stack dump does not contain goroutine stacks")
	}
}

// ── Shutdown interaction ────────────────────────────────────────────────────

// TestSupervisor_ShutdownNoFalsePositive starts a supervisor, begins
// shutdown, stops beating, and verifies no fatal exit during shutdown.
func TestSupervisor_ShutdownNoFalsePositive(t *testing.T) {
	var exitCalled atomic.Int32
	origExit := exitFunc
	exitFunc = func(code int) { exitCalled.Store(int32(code)) }
	defer func() { exitFunc = origExit }()

	cfg := testConfig(t)
	s := New(cfg, testLogger())
	s.Register("drain-worker")
	s.Beat("drain-worker")
	s.Start()

	// Signal shutdown immediately — goroutine is "draining".
	s.Shutdown()

	// Wait past the timeout without beating.
	time.Sleep(cfg.Timeout + 2*cfg.CheckInterval)

	s.Stop()

	if exitCalled.Load() != 0 {
		t.Errorf("exit was called during shutdown with code %d; want no exit", exitCalled.Load())
	}
}

// ── Multiple goroutines ─────────────────────────────────────────────────────

// TestSupervisor_MultipleGoroutines monitors 3 goroutines, stalls one,
// and verifies the supervisor identifies the correct one.
func TestSupervisor_MultipleGoroutines(t *testing.T) {
	var exitCalled atomic.Int32
	exitCh := make(chan struct{}, 1)

	origExit := exitFunc
	exitFunc = func(code int) {
		exitCalled.Store(int32(code))
		select {
		case exitCh <- struct{}{}:
		default:
		}
	}
	defer func() { exitFunc = origExit }()

	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := Config{
		Timeout:       500 * time.Millisecond,
		CheckInterval: 100 * time.Millisecond,
		LogsDir:       filepath.Join(t.TempDir(), "logs"),
	}
	s := New(cfg, logger)
	s.Register("worker-a")
	s.Register("worker-b")
	s.Register("worker-c")
	s.Start()

	// Beat worker-a and worker-c, but NOT worker-b.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-exitCh:
				return
			case <-time.After(50 * time.Millisecond):
				s.Beat("worker-a")
				s.Beat("worker-c")
			}
		}
	}()

	<-done

	if exitCalled.Load() != 3 {
		t.Errorf("exit code: want 3, got %d", exitCalled.Load())
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "worker-b") {
		t.Errorf("log should identify worker-b as stalled; logs:\n%s", logs)
	}

	s.Shutdown()
	s.Stop()
}
