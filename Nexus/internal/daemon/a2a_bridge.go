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

package daemon

import (
	"context"
	"database/sql"
	"path/filepath"

	"github.com/BubbleFish-Nexus/internal/a2a/client"
	"github.com/BubbleFish-Nexus/internal/a2a/governance"
	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/a2a/server"
	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/mcp/bridge"
	_ "modernc.org/sqlite"
)

// setupA2ABridge constructs the A2A bridge and wires it into the MCP server.
// This is a no-op if [a2a] enabled is false or if the MCP server is nil.
// Any error during A2A setup disables A2A and logs a warning (fail-safe:
// A2A errors must never bring down the daemon).
func (d *Daemon) setupA2ABridge(cfg *config.Config) {
	if !cfg.A2A.Enabled {
		return
	}
	if d.mcpServer == nil {
		d.logger.Warn("daemon: A2A enabled but MCP server not running — A2A disabled",
			"component", "a2a",
		)
		return
	}

	configDir, err := config.ConfigDir()
	if err != nil {
		d.logger.Warn("daemon: A2A setup failed — cannot resolve config dir",
			"component", "a2a",
			"error", err,
		)
		return
	}

	// Open a shared SQLite database for governance grants.
	dbPath := filepath.Join(configDir, "nexus.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout%3d5000")
	if err != nil {
		d.logger.Warn("daemon: A2A setup failed — cannot open database",
			"component", "a2a",
			"error", err,
		)
		return
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL"} {
		if _, err := db.Exec(pragma); err != nil {
			d.logger.Warn("daemon: A2A setup failed — database pragma",
				"component", "a2a",
				"error", err,
			)
			db.Close()
			return
		}
	}

	// Migrate governance tables.
	if err := governance.MigrateGrants(db); err != nil {
		d.logger.Warn("daemon: A2A setup failed — governance migration",
			"component", "a2a",
			"error", err,
		)
		db.Close()
		return
	}

	grantStore := governance.NewGrantStore(db)
	govEngine := governance.NewEngine(grantStore)

	// Agent registry.
	regPath := filepath.Join(configDir, "a2a", "registry.db")
	regStore, err := registry.NewStore(regPath)
	if err != nil {
		d.logger.Warn("daemon: A2A setup failed — registry store",
			"component", "a2a",
			"error", err,
		)
		db.Close()
		return
	}

	// Client pool.
	factory := client.NewFactory(d.logger)
	pool := client.NewPool(factory, d.logger)

	// Audit sink adapter — use a fake for now; the bridge just needs a
	// non-nil AuditSink to log events.
	audit := server.NewFakeAuditSink()

	// Construct the bridge.
	br := bridge.NewBridge(pool, govEngine, regStore, audit, d.logger)

	// Wire into the MCP server.
	d.mcpServer.SetBridge(br)

	// Count registered agents for the log message.
	agents, _ := regStore.List(context.Background(), registry.ListFilter{})
	agentCount := 0
	if agents != nil {
		agentCount = len(agents)
	}

	d.logger.Info("daemon: A2A bridge enabled",
		"component", "a2a",
		"agents", agentCount,
	)
}
