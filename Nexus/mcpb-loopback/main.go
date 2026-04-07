// Copyright © 2026 BubbleFish Technologies, Inc.
//
// Loopback test: a minimal stdio "MCP server" that does NOT actually speak
// MCP. Its purpose is to discover whether a Go process spawned by Claude
// Desktop's MSIX sandbox can:
//   1. Write to an arbitrary user filesystem path
//   2. See its environment variables (BUBBLEFISH_HOME etc.)
//   3. Make a TCP connection to 127.0.0.1:7474 (the daemon's MCP listener)
//
// All findings are written to a hardcoded log file. The process then stays
// alive on stdin so Claude Desktop reports it as a healthy MCP server (no
// "exited early" warnings) until the user uninstalls or restarts.

package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"time"
)

const logPath = `D:\Test\BubbleFish\v010-dogfood\loopback-test.log`

func main() {
	// Wipe log on each run so we only see the most recent invocation.
	if err := os.WriteFile(logPath, nil, 0644); err != nil {
		// If we can't even create the file, fall back to stderr (which we
		// expect to be a dead channel, but let's try anyway).
		fmt.Fprintf(os.Stderr, "loopback-test: cannot create log %s: %v\n", logPath, err)
		// Don't exit yet — we still want to stay alive on stdin so Claude
		// Desktop doesn't show "exited early".
	}

	logf("=== LOOPBACK TEST START ===")
	logf("go runtime: %s", runtime.Version())
	logf("os/arch: %s/%s", runtime.GOOS, runtime.GOARCH)
	logf("pid: %d", os.Getpid())
	logf("ppid: %d", os.Getppid())

	if exe, err := os.Executable(); err == nil {
		logf("exe: %s", exe)
	} else {
		logf("exe: <error: %v>", err)
	}

	if cwd, err := os.Getwd(); err == nil {
		logf("cwd: %s", cwd)
	} else {
		logf("cwd: <error: %v>", err)
	}

	logf("argv: %v", os.Args)

	if home, err := os.UserHomeDir(); err == nil {
		logf("os.UserHomeDir(): %s", home)
	} else {
		logf("os.UserHomeDir(): <error: %v>", err)
	}

	logf("--- env ---")
	env := os.Environ()
	sort.Strings(env)
	for _, e := range env {
		logf("  %s", e)
	}
	logf("--- end env ---")

	// TCP connect test to the daemon's MCP listener.
	logf("tcp test: dialing 127.0.0.1:7474 (3s timeout) ...")
	tcpStart := time.Now()
	conn, err := net.DialTimeout("tcp", "127.0.0.1:7474", 3*time.Second)
	tcpElapsed := time.Since(tcpStart)
	if err != nil {
		logf("tcp result: FAILED after %v: %v", tcpElapsed, err)
	} else {
		logf("tcp result: CONNECTED in %v", tcpElapsed)
		_ = conn.Close()
	}

	// Control: TCP connect to a port that has nothing on it. If loopback
	// works, this should fail FAST (<100ms) with ECONNREFUSED. If loopback
	// is blocked at the AppContainer firewall, this will hang for 3 seconds
	// like a real timeout. The difference distinguishes "loopback is broken"
	// from "the specific port is broken".
	logf("control test: dialing 127.0.0.1:1 (3s timeout, expect fast refusal) ...")
	ctlStart := time.Now()
	ctl, err := net.DialTimeout("tcp", "127.0.0.1:1", 3*time.Second)
	ctlElapsed := time.Since(ctlStart)
	if err != nil {
		logf("control result: failed after %v: %v", ctlElapsed, err)
	} else {
		logf("control result: CONNECTED (UNEXPECTED!) in %v", ctlElapsed)
		_ = ctl.Close()
	}

	// HTTP GET test — same target as the tcp test but goes through Go's
	// http.Client which adds DNS resolution, header writing, etc. Useful to
	// distinguish raw TCP from HTTP-layer issues.
	logf("http test: GET http://127.0.0.1:7474/health (3s timeout) ...")
	httpStart := time.Now()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:7474/health")
	httpElapsed := time.Since(httpStart)
	if err != nil {
		logf("http result: FAILED after %v: %v", httpElapsed, err)
	} else {
		logf("http result: %s in %v", resp.Status, httpElapsed)
		_ = resp.Body.Close()
	}

	logf("=== LOOPBACK TEST DONE ===")
	logf("staying alive on stdin (so Claude Desktop reports server as healthy)")

	// Stay alive so Claude Desktop's "exited early" detector doesn't fire.
	// We just block on stdin and read everything we get, ignoring it. When
	// Claude Desktop closes the pipe (uninstall, shutdown), Scanner returns
	// false and we exit cleanly.
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		// Drop everything. We're not actually speaking MCP. If Claude Desktop
		// sends an initialize request and waits for a response, it'll time
		// out client-side, but the server process stays alive — which is what
		// matters for "did the spawn succeed and run to completion".
	}

	logf("stdin closed, exiting cleanly")
}

func logf(format string, args ...interface{}) {
	line := fmt.Sprintf("[%s] ", time.Now().Format(time.RFC3339Nano)) +
		fmt.Sprintf(format, args...) + "\n"
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logf: open: %v | %s", err, line)
		return
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(line); err != nil {
		fmt.Fprintf(os.Stderr, "logf: write: %v | %s", err, line)
	}
}
