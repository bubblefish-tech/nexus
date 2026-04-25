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

//go:build darwin

package supervisor

import (
	"fmt"

	"howett.net/plist"
)

// LaunchdConfig holds values for the launchd plist.
type LaunchdConfig struct {
	Label            string
	Program          string
	ProgramArguments []string
	WorkingDirectory string
	StandardOutPath  string
	StandardErrorPath string
	EnvironmentVariables map[string]string
}

// DefaultLaunchdConfig returns defaults for the launchd plist.
func DefaultLaunchdConfig() LaunchdConfig {
	return LaunchdConfig{
		Label:   "com.bubblefish.nexus",
		Program: "/usr/local/bin/nexus",
		ProgramArguments: []string{
			"/usr/local/bin/nexus",
			"start",
			"--foreground",
		},
		WorkingDirectory:  "~/.nexus",
		StandardOutPath:   "~/.nexus/logs/nexus.stdout.log",
		StandardErrorPath: "~/.nexus/logs/nexus.stderr.log",
	}
}

// launchdPlist is the internal structure that maps to the plist XML format.
type launchdPlist struct {
	Label                string            `plist:"Label"`
	Program              string            `plist:"Program"`
	ProgramArguments     []string          `plist:"ProgramArguments"`
	KeepAlive            bool              `plist:"KeepAlive"`
	RunAtLoad            bool              `plist:"RunAtLoad"`
	WorkingDirectory     string            `plist:"WorkingDirectory"`
	StandardOutPath      string            `plist:"StandardOutPath"`
	StandardErrorPath    string            `plist:"StandardErrorPath"`
	EnvironmentVariables map[string]string `plist:"EnvironmentVariables,omitempty"`
	ProcessType          string            `plist:"ProcessType"`
	ThrottleInterval     int               `plist:"ThrottleInterval"`
}

// GenerateLaunchdPlist renders the launchd plist XML content.
func GenerateLaunchdPlist(cfg LaunchdConfig) (string, error) {
	p := launchdPlist{
		Label:                cfg.Label,
		Program:              cfg.Program,
		ProgramArguments:     cfg.ProgramArguments,
		KeepAlive:            true,
		RunAtLoad:            true,
		WorkingDirectory:     cfg.WorkingDirectory,
		StandardOutPath:      cfg.StandardOutPath,
		StandardErrorPath:    cfg.StandardErrorPath,
		EnvironmentVariables: cfg.EnvironmentVariables,
		ProcessType:          "Background",
		ThrottleInterval:     5,
	}

	data, err := plist.MarshalIndent(p, plist.XMLFormat, "\t")
	if err != nil {
		return "", fmt.Errorf("marshal launchd plist: %w", err)
	}
	return string(data), nil
}
