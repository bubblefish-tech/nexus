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
	"os"
	"path/filepath"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/client"
	"github.com/BubbleFish-Nexus/internal/a2a/governance"
	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/a2a/server"
	"github.com/BubbleFish-Nexus/internal/a2a/transport"
	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/mcp/bridge"
	"github.com/BurntSushi/toml"
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

	// Load agents from TOML files in <configDir>/a2a/agents/*.toml.
	d.loadA2AAgents(configDir, regStore)

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

// agentTOML is the TOML structure for agent registration files.
type agentTOML struct {
	Agent struct {
		Name    string   `toml:"name"`
		AgentID string   `toml:"agent_id"`
		Methods []string `toml:"methods"` // supported JSON-RPC methods (e.g., ["tasks/send", "agent/card"])
	} `toml:"agent"`
	Transport struct {
		Kind string `toml:"kind"`
		HTTP struct {
			URL            string `toml:"url"`
			Auth           string `toml:"auth"`
			BearerTokenEnv string `toml:"bearer_token_env"`
		} `toml:"http"`
	} `toml:"transport"`
}

// loadA2AAgents reads agent TOML files from <configDir>/a2a/agents/ and
// upserts them into the registry. Errors for individual files are logged
// and skipped — one bad TOML must not prevent other agents from loading.
func (d *Daemon) loadA2AAgents(configDir string, regStore *registry.Store) {
	agentsDir := filepath.Join(configDir, "a2a", "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		d.logger.Warn("daemon: cannot read a2a/agents directory",
			"component", "a2a",
			"error", err,
		)
		return
	}

	ctx := context.Background()
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}

		path := filepath.Join(agentsDir, entry.Name())
		var raw agentTOML
		if _, err := toml.DecodeFile(path, &raw); err != nil {
			d.logger.Warn("daemon: skip bad agent TOML",
				"component", "a2a",
				"file", entry.Name(),
				"error", err,
			)
			continue
		}

		if raw.Agent.Name == "" {
			d.logger.Warn("daemon: skip agent TOML with empty name",
				"component", "a2a",
				"file", entry.Name(),
			)
			continue
		}

		agentID := raw.Agent.AgentID
		if agentID == "" {
			agentID = "agt_" + a2a.NewTaskID()[4:] // generate if not set
		}

		// Build transport config.
		tc := transport.TransportConfig{
			Kind:           raw.Transport.Kind,
			URL:            raw.Transport.HTTP.URL,
			AuthType:       raw.Transport.HTTP.Auth,
			BearerTokenEnv: raw.Transport.HTTP.BearerTokenEnv,
		}

		// Check if already registered (idempotent on restart).
		existing, _ := regStore.GetByName(ctx, raw.Agent.Name)
		if existing != nil {
			d.logger.Debug("daemon: agent already registered, skipping",
				"component", "a2a",
				"name", raw.Agent.Name,
			)
			continue
		}

		agent := registry.RegisteredAgent{
			AgentID:     agentID,
			Name:        raw.Agent.Name,
			DisplayName: raw.Agent.Name,
			AgentCard: a2a.AgentCard{
				Name:    raw.Agent.Name,
				Methods: raw.Agent.Methods,
			},
			TransportConfig: tc,
			Status:          registry.StatusActive,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}

		if err := regStore.Register(ctx, agent); err != nil {
			d.logger.Warn("daemon: failed to register agent from TOML",
				"component", "a2a",
				"name", raw.Agent.Name,
				"error", err,
			)
			continue
		}

		d.logger.Info("daemon: registered A2A agent from TOML",
			"component", "a2a",
			"name", raw.Agent.Name,
			"agent_id", agentID,
			"url", tc.URL,
		)
	}
}
