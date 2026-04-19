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
	"strings"

	"github.com/shirou/gopsutil/v3/process"
)

// processNameLister returns the names of all running processes.
type processNameLister func() ([]string, error)

// defaultProcessNameLister lists process names via gopsutil.
func defaultProcessNameLister() ([]string, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(procs))
	for _, p := range procs {
		name, err := p.Name()
		if err != nil {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

// probeProcess checks whether any of def.ProcessNames is currently running.
func probeProcess(def ToolDefinition) (DiscoveredTool, bool) {
	return probeProcessWithLister(def, defaultProcessNameLister)
}

// probeProcessWithLister is the testable core of probeProcess.
func probeProcessWithLister(def ToolDefinition, lister processNameLister) (DiscoveredTool, bool) {
	if len(def.ProcessNames) == 0 {
		return DiscoveredTool{}, false
	}
	names, err := lister()
	if err != nil {
		return DiscoveredTool{}, false
	}
	for _, name := range names {
		// Strip .exe suffix for Windows process names.
		base := strings.TrimSuffix(name, ".exe")
		for _, want := range def.ProcessNames {
			if strings.EqualFold(base, want) || strings.EqualFold(name, want) {
				return DiscoveredTool{
					Name:            def.Name,
					DetectionMethod: MethodProcess,
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
