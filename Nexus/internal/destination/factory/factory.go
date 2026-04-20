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

// Package factory provides OpenByType, a single entry point for opening any
// supported memory destination adapter by config type name.
package factory

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/destination/cockroachdb"
	"github.com/bubblefish-tech/nexus/internal/destination/firestore"
	"github.com/bubblefish-tech/nexus/internal/destination/mongodb"
	"github.com/bubblefish-tech/nexus/internal/destination/mysql"
	"github.com/bubblefish-tech/nexus/internal/destination/tidb"
	"github.com/bubblefish-tech/nexus/internal/destination/turso"
)

// OpenByType opens the destination adapter identified by cfg.Type and returns
// it as a Destination. All connection strings in cfg are expected to have been
// resolved (env:/file: references expanded) by the config layer before this
// call.
//
// Supported types: sqlite, postgres, supabase, mysql, cockroachdb, mongodb,
// firestore, tidb, turso.
func OpenByType(cfg *config.Destination, logger *slog.Logger, configDir string) (destination.Destination, error) {
	switch strings.ToLower(cfg.Type) {
	case "sqlite":
		path := cfg.DBPath
		if path == "" {
			path = filepath.Join(configDir, "memories.db")
		}
		if strings.HasPrefix(path, "~") {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("destination: sqlite: expand path: %w", err)
			}
			path = filepath.Join(home, path[1:])
		}
		return destination.OpenSQLite(filepath.Clean(path), logger)

	case "postgres", "postgresql":
		if cfg.DSN == "" {
			return nil, fmt.Errorf("destination: postgres %q: dsn is required", cfg.Name)
		}
		return destination.OpenPostgres(cfg.DSN, 0, logger)

	case "supabase":
		if cfg.URL == "" {
			return nil, fmt.Errorf("destination: supabase %q: url is required", cfg.Name)
		}
		resolvedKey, err := config.ResolveEnv(cfg.APIKey, logger)
		if err != nil {
			return nil, fmt.Errorf("destination: supabase %q: resolve api_key: %w", cfg.Name, err)
		}
		return destination.OpenSupabase(cfg.URL, resolvedKey, logger)

	case "mysql", "mariadb":
		if cfg.DSN == "" {
			return nil, fmt.Errorf("destination: mysql %q: dsn is required", cfg.Name)
		}
		return mysql.Open(cfg.DSN, logger)

	case "cockroachdb", "crdb":
		if cfg.DSN == "" {
			return nil, fmt.Errorf("destination: cockroachdb %q: dsn is required", cfg.Name)
		}
		return cockroachdb.Open(cfg.DSN, logger)

	case "mongodb", "mongo":
		uri := cfg.ConnectionString
		if uri == "" {
			uri = cfg.DSN
		}
		if uri == "" {
			return nil, fmt.Errorf("destination: mongodb %q: connection_string (or dsn) is required", cfg.Name)
		}
		return mongodb.Open(uri, logger)

	case "firestore":
		projectID := cfg.ConnectionString
		if projectID == "" {
			return nil, fmt.Errorf("destination: firestore %q: connection_string (project ID) is required", cfg.Name)
		}
		if cfg.APIKey != "" {
			credFile, err := config.ResolveEnv(cfg.APIKey, logger)
			if err != nil {
				return nil, fmt.Errorf("destination: firestore %q: resolve api_key (credentials): %w", cfg.Name, err)
			}
			return firestore.OpenWithCredentials(projectID, credFile, logger)
		}
		return firestore.Open(projectID, logger)

	case "tidb":
		if cfg.DSN == "" {
			return nil, fmt.Errorf("destination: tidb %q: dsn is required", cfg.Name)
		}
		return tidb.Open(cfg.DSN, logger)

	case "turso", "libsql":
		url := cfg.ConnectionString
		if url == "" {
			url = cfg.URL
		}
		if url == "" {
			return nil, fmt.Errorf("destination: turso %q: connection_string (or url) is required", cfg.Name)
		}
		return turso.Open(url, logger)

	default:
		return nil, fmt.Errorf("destination: unknown type %q for destination %q", cfg.Type, cfg.Name)
	}
}
