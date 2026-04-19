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
	"sync"
	"time"
)

const generalMaxConcurrent = 50

// probeGeneralPorts scans common AI port ranges for OpenAI-compatible /v1/models endpoints.
func probeGeneralPorts(timeout time.Duration) []DiscoveredTool {
	return scanPorts(generalPortList(), func(port int) string {
		return fmt.Sprintf("http://localhost:%d", port)
	}, timeout)
}

// scanPorts is the testable core of probeGeneralPorts.
// baseURLOf maps a port number to the base URL to probe (allows test servers at arbitrary URLs).
func scanPorts(ports []int, baseURLOf func(int) string, timeout time.Duration) []DiscoveredTool {
	sem := make(chan struct{}, generalMaxConcurrent)
	var mu sync.Mutex
	var results []DiscoveredTool
	var wg sync.WaitGroup

	client := &http.Client{Timeout: timeout}

	for _, port := range ports {
		wg.Add(1)
		sem <- struct{}{}
		go func(p int) {
			defer wg.Done()
			defer func() { <-sem }()

			baseURL := baseURLOf(p)
			target := baseURL + "/v1/models"
			resp, err := client.Get(target) //nolint:noctx
			if err != nil {
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
			if err != nil {
				return
			}
			s := string(body)
			if !strings.Contains(s, "data") && !strings.Contains(s, "object") {
				return
			}
			dt := DiscoveredTool{
				Name:            fmt.Sprintf("OpenAI API (port %d)", p),
				DetectionMethod: MethodPort,
				ConnectionType:  ConnOpenAICompat,
				Endpoint:        baseURL,
				Orchestratable:  true,
				IngestCapable:   false,
			}
			mu.Lock()
			results = append(results, dt)
			mu.Unlock()
		}(port)
	}
	wg.Wait()
	return results
}

// generalPortList returns all port numbers in the general scan range.
func generalPortList() []int {
	var ports []int
	addRange := func(from, to int) {
		for p := from; p <= to; p++ {
			ports = append(ports, p)
		}
	}
	addRange(1234, 1240)
	addRange(3000, 3100)
	ports = append(ports, 4891)
	addRange(5000, 5010)
	ports = append(ports, 7474)
	addRange(7860, 7870)
	addRange(8000, 8100)
	ports = append(ports, 8443, 9090, 11434)
	return ports
}
