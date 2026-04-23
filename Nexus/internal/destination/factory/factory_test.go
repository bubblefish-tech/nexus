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

package factory

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/config"
)

func TestOpenByType_SQLite(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Destination{
		Type:   "sqlite",
		DBPath: filepath.Join(dir, "test.db"),
	}
	dest, err := OpenByType(cfg, slog.Default(), dir)
	if err != nil {
		t.Fatalf("OpenByType sqlite: %v", err)
	}
	defer dest.Close()
}

func TestOpenByType_UnknownType(t *testing.T) {
	t.Helper()
	cfg := &config.Destination{Type: "unknowndb"}
	_, err := OpenByType(cfg, slog.Default(), t.TempDir())
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestOpenByType_EmptyType(t *testing.T) {
	t.Helper()
	cfg := &config.Destination{Type: ""}
	_, err := OpenByType(cfg, slog.Default(), t.TempDir())
	if err == nil {
		t.Fatal("expected error for empty type")
	}
}

func TestOpenByType_PostgresNoDBPath(t *testing.T) {
	t.Helper()
	cfg := &config.Destination{Type: "postgres", DSN: ""}
	_, err := OpenByType(cfg, slog.Default(), t.TempDir())
	if err == nil {
		t.Fatal("expected error for postgres without DSN")
	}
}
