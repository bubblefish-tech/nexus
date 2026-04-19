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

import "os"

// probeFilesystem checks whether any of def.DirectoryPaths exists on the filesystem.
// The first path that exists determines the match.
func probeFilesystem(def ToolDefinition) (DiscoveredTool, bool) {
	return probeFilesystemWithPaths(def, nil)
}

// probeFilesystemWithPaths is the testable core of probeFilesystem.
// pathsOverride replaces def.DirectoryPaths when non-nil.
func probeFilesystemWithPaths(def ToolDefinition, pathsOverride []string) (DiscoveredTool, bool) {
	paths := def.DirectoryPaths
	if pathsOverride != nil {
		paths = pathsOverride
	}
	for _, rawPath := range paths {
		expanded, err := ExpandPath(rawPath)
		if err != nil {
			continue
		}
		if _, err := os.Stat(expanded); err == nil {
			return DiscoveredTool{
				Name:            def.Name,
				DetectionMethod: MethodDirectory,
				ConnectionType:  def.ConnectionType,
				Endpoint:        def.DefaultEndpoint,
				Orchestratable:  def.Orchestratable,
				IngestCapable:   def.IngestCapable,
			}, true
		}
	}
	return DiscoveredTool{}, false
}
