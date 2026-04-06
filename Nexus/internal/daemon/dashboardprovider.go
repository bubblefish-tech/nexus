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
	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/lint"
	"github.com/BubbleFish-Nexus/internal/web"
)

// DashboardSecurityProvider adapts Daemon to the web.SecurityProvider interface.
// All methods are safe for concurrent use (they use the daemon's RWMutex and
// the securitylog's internal mutex).
//
// Reference: Tech Spec Section 13.2 — Security Tab.
type DashboardSecurityProvider struct {
	d *Daemon
}

// NewDashboardSecurityProvider creates a SecurityProvider backed by the given Daemon.
func NewDashboardSecurityProvider(d *Daemon) *DashboardSecurityProvider {
	return &DashboardSecurityProvider{d: d}
}

// SourcePolicies returns a read-only summary of all source policies.
func (p *DashboardSecurityProvider) SourcePolicies() []web.SourcePolicyInfo {
	cfg := p.d.getConfig()
	out := make([]web.SourcePolicyInfo, 0, len(cfg.Sources))
	for _, src := range cfg.Sources {
		out = append(out, web.SourcePolicyInfo{
			Name:                src.Name,
			CanRead:             src.CanRead,
			CanWrite:            src.CanWrite,
			AllowedDestinations: src.Policy.AllowedDestinations,
			MaxResults:          src.Policy.MaxResults,
			MaxResponseBytes:    src.Policy.MaxResponseBytes,
			RateLimit:           src.RateLimit.RequestsPerMinute,
		})
	}
	return out
}

// AuthFailures returns the last N auth failure events.
func (p *DashboardSecurityProvider) AuthFailures(limit int) []web.AuthFailureInfo {
	if p.d.securityLog == nil {
		return nil
	}
	events := p.d.securityLog.Recent(limit)
	var out []web.AuthFailureInfo
	for _, e := range events {
		if e.EventType != "auth_failure" {
			continue
		}
		tokenClass, _ := e.Details["token_class"].(string)
		statusCode := 401
		if tokenClass == "wrong_token_class" {
			statusCode = 403
		}
		out = append(out, web.AuthFailureInfo{
			Timestamp:  e.Timestamp.Format("2006-01-02T15:04:05Z"),
			Source:     e.Source,
			IP:         e.IP,
			Endpoint:   e.Endpoint,
			TokenClass: tokenClass,
			StatusCode: statusCode,
		})
	}
	return out
}

// LintFindings runs config lint and returns the findings.
func (p *DashboardSecurityProvider) LintFindings() []web.LintFinding {
	cfg := p.d.getConfig()
	configDir, err := config.ConfigDir()
	if err != nil {
		return nil
	}
	result := lint.Run(cfg, configDir)
	out := make([]web.LintFinding, 0, len(result.Findings))
	for _, f := range result.Findings {
		out = append(out, web.LintFinding{
			Severity: string(f.Severity),
			Check:    f.Check,
			Message:  f.Message,
		})
	}
	return out
}
