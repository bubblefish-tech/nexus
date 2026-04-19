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

package discover

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// dockerOutputReader returns the raw output of `docker ps --format {{.Image}}`.
type dockerOutputReader func(timeout time.Duration) ([]byte, error)

// defaultDockerOutputReader runs `docker ps --format {{.Image}}` and returns its stdout.
// Returns an error (and no result) if Docker is not installed or the daemon is not running.
func defaultDockerOutputReader(timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "ps", "--format", "{{.Image}}").Output()
}

// probeDocker checks whether a Docker container with one of def.DockerImages is running.
func probeDocker(def ToolDefinition, timeout time.Duration) (DiscoveredTool, bool) {
	return probeDockerWithReader(def, defaultDockerOutputReader, timeout)
}

// probeDockerWithReader is the testable core of probeDocker.
func probeDockerWithReader(def ToolDefinition, reader dockerOutputReader, timeout time.Duration) (DiscoveredTool, bool) {
	if len(def.DockerImages) == 0 {
		return DiscoveredTool{}, false
	}
	out, err := reader(timeout)
	if err != nil {
		return DiscoveredTool{}, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, img := range def.DockerImages {
			if strings.Contains(line, img) {
				return DiscoveredTool{
					Name:            def.Name,
					DetectionMethod: MethodDocker,
					ConnectionType:  def.ConnectionType,
					Endpoint:        def.DefaultEndpoint,
					Orchestratable:  def.Orchestratable,
					IngestCapable:   def.IngestCapable,
				}, true
			}
		}
	}
	return DiscoveredTool{}, false
}
