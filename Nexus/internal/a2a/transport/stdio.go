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

package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
)

// StdioTransport implements Transport over newline-delimited JSON on stdin/stdout.
type StdioTransport struct{}

// Dial spawns a child process and communicates via its stdin/stdout.
func (t *StdioTransport) Dial(ctx context.Context, config TransportConfig) (Conn, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, config.Command, config.Args...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("transport: stdio stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("transport: stdio stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("transport: stdio start: %w", err)
	}

	conn := &stdioConn{
		reader:  bufio.NewReader(stdout),
		writer:  stdin,
		cmd:     cmd,
		pending: make(map[string]chan *jsonrpc.Response),
		done:    make(chan struct{}),
	}

	go conn.readLoop()
	return conn, nil
}

// Listen creates a stdio server that reads from os.Stdin and writes to os.Stdout.
func (t *StdioTransport) Listen(ctx context.Context, config TransportConfig) (Listener, error) {
	return &stdioListener{
		ctx:  ctx,
		done: make(chan struct{}),
	}, nil
}

// stdioConn is a client connection over a child process's stdin/stdout.
type stdioConn struct {
	reader  *bufio.Reader
	writer  io.WriteCloser
	cmd     *exec.Cmd
	pending map[string]chan *jsonrpc.Response
	mu      sync.Mutex
	done    chan struct{}
	closed  bool
}

func (c *stdioConn) readLoop() {
	defer func() {
		c.mu.Lock()
		for _, ch := range c.pending {
			close(ch)
		}
		c.pending = nil
		c.mu.Unlock()
	}()

	scanner := bufio.NewScanner(c.reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp jsonrpc.Response
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // skip malformed frames
		}

		idStr := resp.ID.String()
		c.mu.Lock()
		ch, ok := c.pending[idStr]
		if ok {
			delete(c.pending, idStr)
		}
		c.mu.Unlock()

		if ok {
			select {
			case ch <- &resp:
			default:
			}
		}
	}
}

// Send sends a JSON-RPC request and waits for the response.
func (c *stdioConn) Send(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("transport: connection closed")
	}

	idStr := req.ID.String()
	ch := make(chan *jsonrpc.Response, 1)
	if c.pending != nil {
		c.pending[idStr] = ch
	}
	c.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("transport: marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.writer.Write(data); err != nil {
		return nil, fmt.Errorf("transport: write request: %w", err)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("transport: connection closed while waiting for response")
		}
		return resp, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, idStr)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Stream is not natively supported over stdio; returns an error.
func (c *stdioConn) Stream(ctx context.Context, req *jsonrpc.Request) (<-chan Event, error) {
	return nil, fmt.Errorf("transport: stdio does not support streaming")
}

// Close kills the child process and closes pipes.
func (c *stdioConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	c.writer.Close()
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait() //nolint:errcheck
	}
	return nil
}

// stdioListener implements Listener for the server side of stdio transport.
type stdioListener struct {
	ctx      context.Context
	done     chan struct{}
	accepted bool
	mu       sync.Mutex
}

// Accept returns a single server-side stdio connection (stdin/stdout).
// Only one connection can be accepted per stdio listener.
func (l *stdioListener) Accept(ctx context.Context) (Conn, error) {
	l.mu.Lock()
	// Check if already closed.
	select {
	case <-l.done:
		l.mu.Unlock()
		return nil, fmt.Errorf("transport: listener closed")
	default:
	}

	if l.accepted {
		l.mu.Unlock()
		// Block until closed; only one stdio connection per process.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-l.done:
			return nil, fmt.Errorf("transport: listener closed")
		}
	}
	l.accepted = true
	l.mu.Unlock()

	return &stdioServerConn{
		reader: bufio.NewReader(os.Stdin),
		writer: os.Stdout,
		done:   make(chan struct{}),
	}, nil
}

// Close shuts down the stdio listener.
func (l *stdioListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}

// Addr returns a descriptor for the stdio listener.
func (l *stdioListener) Addr() string {
	return "stdio"
}

// stdioServerConn is a server-side stdio connection (reads stdin, writes stdout).
type stdioServerConn struct {
	reader *bufio.Reader
	writer io.Writer
	done   chan struct{}
	mu     sync.Mutex
}

// Send reads a request from stdin and returns nil (server-side pattern: use
// readRequest to get the incoming request, then write response with writeResponse).
func (c *stdioServerConn) Send(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	// Server-side: write a response frame (used by the handler loop).
	return nil, fmt.Errorf("transport: stdio server conn: use ReadRequest/WriteResponse")
}

// ReadRequest reads a JSON-RPC request from stdin.
func (c *stdioServerConn) ReadRequest(ctx context.Context) (*jsonrpc.Request, error) {
	type result struct {
		req *jsonrpc.Request
		err error
	}
	ch := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(c.reader)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var req jsonrpc.Request
			if err := json.Unmarshal(line, &req); err != nil {
				continue
			}
			ch <- result{req: &req}
			return
		}
		ch <- result{err: io.EOF}
	}()

	select {
	case r := <-ch:
		return r.req, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// WriteResponse writes a JSON-RPC response to stdout.
func (c *stdioServerConn) WriteResponse(resp *jsonrpc.Response) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("transport: marshal response: %w", err)
	}
	data = append(data, '\n')
	_, err = c.writer.Write(data)
	return err
}

// Stream is not supported on server-side stdio connections.
func (c *stdioServerConn) Stream(ctx context.Context, req *jsonrpc.Request) (<-chan Event, error) {
	return nil, fmt.Errorf("transport: stdio server does not support streaming")
}

// Close closes the server-side stdio connection.
func (c *stdioServerConn) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return nil
}
