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

//go:build linux

package supervisor

import (
	"fmt"
	"text/template"
)

// systemdUnitTemplate is the systemd service unit for nexus.
const systemdUnitTemplate = `[Unit]
Description=BubbleFish Nexus Memory Daemon
Documentation=https://github.com/bubblefish-tech/nexus
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
ExecStart={{.ExecStart}} start --foreground
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=5s
WatchdogSec=30s
TimeoutStartSec=30s
TimeoutStopSec=30s
LimitNOFILE=65536
Environment=GOGC=100
WorkingDirectory={{.WorkingDirectory}}
User={{.User}}
Group={{.Group}}

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths={{.DataDirectory}}
PrivateTmp=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictSUIDSGID=true

[Install]
WantedBy=multi-user.target
`

// SystemdUnitConfig holds values for the systemd unit template.
type SystemdUnitConfig struct {
	ExecStart        string
	WorkingDirectory string
	DataDirectory    string
	User             string
	Group            string
}

// DefaultSystemdUnitConfig returns defaults for the systemd unit.
func DefaultSystemdUnitConfig() SystemdUnitConfig {
	return SystemdUnitConfig{
		ExecStart:        "/usr/local/bin/nexus",
		WorkingDirectory: "/var/lib/nexus",
		DataDirectory:    "/var/lib/nexus",
		User:             "nexus",
		Group:            "nexus",
	}
}

// GenerateSystemdUnit renders the systemd unit file content.
func GenerateSystemdUnit(cfg SystemdUnitConfig) (string, error) {
	tmpl, err := template.New("systemd").Parse(systemdUnitTemplate)
	if err != nil {
		return "", fmt.Errorf("parse systemd template: %w", err)
	}

	var buf stringBuffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("execute systemd template: %w", err)
	}
	return buf.String(), nil
}
