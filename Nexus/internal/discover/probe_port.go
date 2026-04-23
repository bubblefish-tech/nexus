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
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// probePort checks whether a port-detectable tool is listening on its default port.
func probePort(def ToolDefinition, timeout time.Duration) (DiscoveredTool, bool) {
	base := fmt.Sprintf("http://localhost:%d", def.DefaultPort)
	return probePortAt(def, base, timeout)
}

// probePortAt hits baseURL+def.ProbeURL and checks for def.ExpectedResponse in the response body.
// Separated from probePort for testability.
func probePortAt(def ToolDefinition, baseURL string, timeout time.Duration) (DiscoveredTool, bool) {
	target := baseURL + def.ProbeURL
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(target) //nolint:noctx
	if err != nil {
		return DiscoveredTool{}, false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return DiscoveredTool{}, false
	}
	if !strings.Contains(string(body), def.ExpectedResponse) {
		return DiscoveredTool{}, false
	}

	endpoint := def.DefaultEndpoint
	if endpoint == "" {
		endpoint = baseURL
	}
	return DiscoveredTool{
		Name:            def.Name,
		DetectionMethod: MethodPort,
		ConnectionType:  def.ConnectionType,
		Endpoint:        endpoint,
		Orchestratable:  def.Orchestratable,
		IngestCapable:   def.IngestCapable,
	}, true
}
