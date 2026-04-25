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
	"encoding/json"
	"errors"
	"io"
	"time"
)

// PipeMsg is a structured message sent over the supervisor↔daemon pipe.
type PipeMsg struct {
	// Type identifies the message kind.
	Type PipeMsgType `json:"type"`

	// Tier is set on TierChange messages.
	Tier DegradationTier `json:"tier,omitempty"`

	// Timestamp is when the message was created.
	Timestamp time.Time `json:"ts"`

	// Payload carries optional data (e.g., error details).
	Payload string `json:"payload,omitempty"`
}

// PipeMsgType identifies the kind of pipe message.
type PipeMsgType string

const (
	// PipeMsgHeartbeat is sent by the daemon to indicate liveness.
	PipeMsgHeartbeat PipeMsgType = "heartbeat"
	// PipeMsgTierChange is sent by the supervisor to indicate a tier transition.
	PipeMsgTierChange PipeMsgType = "tier_change"
	// PipeMsgShutdown is sent by the supervisor to request graceful shutdown.
	PipeMsgShutdown PipeMsgType = "shutdown"
	// PipeMsgReady is sent by the daemon when it has finished starting.
	PipeMsgReady PipeMsgType = "ready"
	// PipeMsgError is sent by the daemon to report a non-fatal error.
	PipeMsgError PipeMsgType = "error"
)

// ErrPipeClosed indicates the pipe has been closed.
var ErrPipeClosed = errors.New("supervisor: pipe closed")

// Pipe is the interface for the local communication channel between
// the supervisor process and the daemon process. Implementations are
// platform-specific (named pipes on Windows, Unix domain sockets elsewhere).
type Pipe interface {
	// Send writes a message to the pipe. Returns ErrPipeClosed if the
	// other end has disconnected.
	Send(msg PipeMsg) error

	// Recv reads the next message from the pipe. Blocks until a message
	// is available or the pipe is closed (returns ErrPipeClosed).
	Recv() (PipeMsg, error)

	// Close closes the pipe. Safe to call multiple times.
	Close() error
}

// pipePair creates a connected pair of Pipe implementations backed by
// io.ReadWriteCloser. Used for testing and in-process communication.
func pipePair() (Pipe, Pipe) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()

	a := &streamPipe{
		enc:    json.NewEncoder(w1),
		dec:    json.NewDecoder(r2),
		closer: &multiCloser{closers: []io.Closer{w1, r2}},
	}
	b := &streamPipe{
		enc:    json.NewEncoder(w2),
		dec:    json.NewDecoder(r1),
		closer: &multiCloser{closers: []io.Closer{w2, r1}},
	}
	return a, b
}

// streamPipe is a Pipe backed by a JSON encoder/decoder over streams.
type streamPipe struct {
	enc    *json.Encoder
	dec    *json.Decoder
	closer io.Closer
}

func (p *streamPipe) Send(msg PipeMsg) error {
	if err := p.enc.Encode(msg); err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			return ErrPipeClosed
		}
		return err
	}
	return nil
}

func (p *streamPipe) Recv() (PipeMsg, error) {
	var msg PipeMsg
	if err := p.dec.Decode(&msg); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
			return msg, ErrPipeClosed
		}
		return msg, err
	}
	return msg, nil
}

func (p *streamPipe) Close() error {
	return p.closer.Close()
}

// multiCloser closes multiple io.Closers.
type multiCloser struct {
	closers []io.Closer
}

func (mc *multiCloser) Close() error {
	var first error
	for _, c := range mc.closers {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
