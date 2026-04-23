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
	"time"

	"github.com/bubblefish-tech/nexus/internal/audit"
	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/lint"
	"github.com/bubblefish-tech/nexus/internal/web"
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

// ---------------------------------------------------------------------------
// DashboardAuditProvider — implements web.AuditProvider
// ---------------------------------------------------------------------------

// DashboardAuditProvider adapts Daemon to the web.AuditProvider interface.
// All methods are safe for concurrent use (they use the AuditReader which
// creates a new file handle per query).
//
// Reference: Tech Spec Addendum Section A2.7.
type DashboardAuditProvider struct {
	d *Daemon
}

// NewDashboardAuditProvider creates an AuditProvider backed by the given Daemon.
func NewDashboardAuditProvider(d *Daemon) *DashboardAuditProvider {
	return &DashboardAuditProvider{d: d}
}

// RecentInteractions returns the most recent interaction records.
func (p *DashboardAuditProvider) RecentInteractions(limit int) []web.AuditRecordInfo {
	if p.d.auditReader == nil {
		return nil
	}
	result, err := p.d.auditReader.Query(audit.AuditFilter{Limit: limit})
	if err != nil {
		return nil
	}
	return convertRecords(result.Records)
}

// InteractionsByActor returns interaction records for a specific actor.
func (p *DashboardAuditProvider) InteractionsByActor(actorID string, limit int) []web.AuditRecordInfo {
	if p.d.auditReader == nil {
		return nil
	}
	result, err := p.d.auditReader.Query(audit.AuditFilter{
		ActorID: actorID,
		Limit:   limit,
	})
	if err != nil {
		return nil
	}
	return convertRecords(result.Records)
}

// PolicyDenials returns interaction records with denied or filtered decisions.
func (p *DashboardAuditProvider) PolicyDenials(limit int) []web.AuditRecordInfo {
	if p.d.auditReader == nil {
		return nil
	}
	// Query denied first, then filtered, and merge.
	denied, err := p.d.auditReader.Query(audit.AuditFilter{
		PolicyDecision: "denied",
		Limit:          limit,
	})
	if err != nil {
		return nil
	}
	filtered, err := p.d.auditReader.Query(audit.AuditFilter{
		PolicyDecision: "filtered",
		Limit:          limit,
	})
	if err != nil {
		return convertRecords(denied.Records)
	}
	combined := make([]audit.InteractionRecord, 0, len(denied.Records)+len(filtered.Records))
	combined = append(combined, denied.Records...)
	combined = append(combined, filtered.Records...)
	if len(combined) > limit {
		combined = combined[:limit]
	}
	return convertRecords(combined)
}

// AuditStats returns summary statistics for the interaction log.
func (p *DashboardAuditProvider) AuditStats() web.AuditStatsInfo {
	empty := web.AuditStatsInfo{
		InteractionsPerHr: map[string]int{},
		TopSources:        map[string]int{},
		TopActors:         map[string]int{},
		ByOperation:       map[string]int{},
		ByDecision:        map[string]int{},
	}
	if p.d.auditReader == nil {
		return empty
	}
	result, err := p.d.auditReader.Query(audit.AuditFilter{Limit: 1000})
	if err != nil {
		return empty
	}

	stats := web.AuditStatsInfo{
		TotalRecords:      result.TotalMatching,
		InteractionsPerHr: make(map[string]int),
		TopSources:        make(map[string]int),
		TopActors:         make(map[string]int),
		ByOperation:       make(map[string]int),
		ByDecision:        make(map[string]int),
	}

	oneHourAgo := time.Now().UTC().Add(-1 * time.Hour)
	var denied, filteredCount int

	for _, rec := range result.Records {
		stats.ByOperation[rec.OperationType]++
		stats.ByDecision[rec.PolicyDecision]++
		stats.TopSources[rec.Source]++
		if rec.ActorID != "" {
			stats.TopActors[rec.ActorID]++
		}
		if rec.PolicyDecision == "denied" {
			denied++
		}
		if rec.PolicyDecision == "filtered" {
			filteredCount++
		}
		if rec.Timestamp.After(oneHourAgo) {
			stats.InteractionsPerHr[rec.OperationType]++
		}
	}

	if stats.TotalRecords > 0 {
		stats.DenialRate = float64(denied) / float64(stats.TotalRecords)
		stats.FilterRate = float64(filteredCount) / float64(stats.TotalRecords)
	}

	return stats
}

// convertRecords converts audit.InteractionRecord to web.AuditRecordInfo.
func convertRecords(records []audit.InteractionRecord) []web.AuditRecordInfo {
	out := make([]web.AuditRecordInfo, 0, len(records))
	for _, rec := range records {
		out = append(out, web.AuditRecordInfo{
			RecordID:       rec.RecordID,
			Timestamp:      rec.Timestamp.Format(time.RFC3339),
			Source:         rec.Source,
			ActorType:      rec.ActorType,
			ActorID:        rec.ActorID,
			OperationType:  rec.OperationType,
			Endpoint:       rec.Endpoint,
			HTTPStatusCode: rec.HTTPStatusCode,
			PolicyDecision: rec.PolicyDecision,
			PolicyReason:   rec.PolicyReason,
			LatencyMs:      rec.LatencyMs,
			Destination:    rec.Destination,
			Subject:        rec.Subject,
			ResultCount:    rec.ResultCount,
		})
	}
	return out
}
