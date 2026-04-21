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

package topology

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
)

// detectDocker probes the local Docker daemon for network information.
// Returns Available=false (not an error) when Docker is absent or not running.
func detectDocker(ctx context.Context) *DockerTopology {
	if _, err := exec.LookPath("docker"); err != nil {
		return &DockerTopology{Available: false}
	}

	// --format uses Go templates supported by all Docker versions since 1.13.
	out, err := exec.CommandContext(ctx, "docker", "network", "ls",
		"--format", "{{.Name}}\t{{.Driver}}").Output()
	if err != nil {
		// Docker installed but daemon not running, or permission denied.
		return &DockerTopology{Available: false}
	}

	var networks []DockerNetwork
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 2)
		if len(parts) == 2 {
			networks = append(networks, DockerNetwork{
				Name:   strings.TrimSpace(parts[0]),
				Driver: strings.TrimSpace(parts[1]),
			})
		}
	}

	return &DockerTopology{Available: true, Networks: networks}
}
