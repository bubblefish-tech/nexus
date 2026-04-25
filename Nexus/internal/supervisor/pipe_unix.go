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

//go:build !windows

package supervisor

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
)

// PlatformPipePath returns the default pipe path for the current platform.
// On Unix, this is a Unix domain socket in the config directory.
func PlatformPipePath(configDir string) string {
	return filepath.Join(configDir, "supervisor.sock")
}

// ListenPipe creates a server-side Pipe that listens on a Unix domain socket.
// The caller is responsible for closing the returned Pipe when done.
func ListenPipe(path string) (Pipe, error) {
	// Remove stale socket file if present.
	_ = os.Remove(path)

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}

	// Set socket file permissions to 0600.
	if err := os.Chmod(path, 0600); err != nil {
		ln.Close()
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

// DialPipe connects to an existing Unix domain socket.
func DialPipe(path string) (Pipe, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	return &streamPipe{
		enc:    json.NewEncoder(conn),
		dec:    json.NewDecoder(conn),
		closer: conn,
	}, nil
}
