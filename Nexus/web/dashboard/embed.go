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

// Package dashboard embeds the v4 dashboard HTML and static assets so they
// are baked into the binary at build time.
package dashboard

import "embed"

// HTML is the v4 dashboard HTML content.
//
//go:embed index.html
var HTML string

// A2APermissionsHTML is the A2A permissions management page.
//
//go:embed a2a_permissions.html
var A2APermissionsHTML string

// OpenClawHTML is the OpenClaw agent control page.
//
//go:embed openclaw.html
var OpenClawHTML string

// AgentsHTML is the control-plane agent registry dashboard page.
//
//go:embed agents.html
var AgentsHTML string

// GrantsHTML is the control-plane capability grants dashboard page.
//
//go:embed grants.html
var GrantsHTML string

// ApprovalsHTML is the control-plane approval requests dashboard page.
//
//go:embed approvals.html
var ApprovalsHTML string

// TasksHTML is the control-plane tasks dashboard page.
//
//go:embed tasks.html
var TasksHTML string

// ActionsHTML is the control-plane action log dashboard page.
//
//go:embed actions.html
var ActionsHTML string

// QuarantineHTML is the Tier-0 immune-scanner quarantine review dashboard page.
//
//go:embed quarantine.html
var QuarantineHTML string

// MemgraphHTML is the SHOW.2 memory-graph D3.js visualization dashboard page.
//
//go:embed memgraph.html
var MemgraphHTML string

// MemHealthHTML is the memory health dashboard page.
//
//go:embed memhealth.html
var MemHealthHTML string

// LogoPNG is the BubbleFish logo used in the sidebar and witness HUD.
//
//go:embed assets/logo_metal.png
var LogoPNG []byte

// Assets embeds the entire assets directory for future static files.
//
//go:embed assets/*
var Assets embed.FS
