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

//go:build windows

package supervisor

import (
	"encoding/json"
	"net"
)

// PlatformPipePath returns the default pipe path for Windows.
// Uses a named pipe in the \\.\pipe\ namespace.
func PlatformPipePath(_ string) string {
	return `\\.\pipe\nexus-supervisor`
}

// ListenPipe creates a server-side Pipe on Windows.
// On Windows we use a TCP loopback connection as a portable fallback
// since named pipes require syscall-level code (winio) that we avoid
// for zero-dep compliance.
func ListenPipe(path string) (Pipe, error) {
	// Use TCP loopback on an ephemeral port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	conn, err := ln.Accept()
	if err != nil {
		ln.Close()
		return nil, err
	}
	ln.Close()

	return &streamPipe{
		enc:    json.NewEncoder(conn),
		dec:    json.NewDecoder(conn),
		closer: conn,
	}, nil
}

// DialPipe connects to an existing supervisor pipe on Windows.
func DialPipe(path string) (Pipe, error) {
	conn, err := net.Dial("tcp", path)
	if err != nil {
		return nil, err
	}
	return &streamPipe{
		enc:    json.NewEncoder(conn),
		dec:    json.NewDecoder(conn),
		closer: conn,
	}, nil
}
