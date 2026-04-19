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
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bubblefish-tech/nexus/internal/audit"
	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/mcp"
	"github.com/bubblefish-tech/nexus/internal/quarantine"
	"github.com/bubblefish-tech/nexus/internal/query"
	"github.com/bubblefish-tech/nexus/internal/version"
	"github.com/bubblefish-tech/nexus/internal/wal"
)

// Daemon implements mcp.Pipeline. All three methods route through the same
// WAL → queue → destination pipeline as the HTTP handlers. MCP calls NEVER
// go through HTTP round-trips.
//
// Reference: Tech Spec Section 14.3 — "Internal pipeline — not HTTP round-trip."

// Write persists content through the WAL → queue → destination pipeline,
// identical to handleWrite but called directly without HTTP overhead.
//
// It respects the same ordering contract as handleWrite:
//  1. Source lookup
//  2. Policy gate
//  3. Build TranslatedPayload
//  4. WAL append
//  5. Queue enqueue
//
// Reference: Tech Spec Section 3.2, Section 14.3.
func (d *Daemon) Write(ctx context.Context, params mcp.WriteParams) (mcp.WriteResult, error) {
	cfg := d.getConfig()

	// Step 1 — Look up source by name.
	src := cfg.SourceByName(params.Source)
	if src == nil {
		return mcp.WriteResult{}, fmt.Errorf("mcp: source %q not found in config", params.Source)
	}

	// Step 2 — CanWrite guard.
	if !src.CanWrite {
		return mcp.WriteResult{}, fmt.Errorf("mcp: source %q does not have write permission", params.Source)
	}

	// Resolve destination: use params override, then source default.
	dest := params.Destination
	if dest == "" {
		dest = src.TargetDest
	}

	// Step 2b — Policy gate.
	if len(src.Policy.AllowedDestinations) > 0 && !containsString(src.Policy.AllowedDestinations, dest) {
		return mcp.WriteResult{}, fmt.Errorf("mcp: policy_denied: destination %q not permitted for source %q", dest, params.Source)
	}
	if len(src.Policy.AllowedOperations) > 0 && !containsString(src.Policy.AllowedOperations, "write") {
		return mcp.WriteResult{}, fmt.Errorf("mcp: policy_denied: write operation not permitted for source %q", params.Source)
	}

	// Resolve subject: use params override, then source namespace.
	subject := params.Subject
	if subject == "" {
		subject = src.Namespace
	}

	// Resolve actor fields.
	// Reference: Tech Spec Section 7.1 — Provenance Semantics.
	actorType := params.ActorType
	if actorType == "" {
		actorType = src.DefaultActorType
	}
	if !destination.ValidActorType(actorType) {
		return mcp.WriteResult{}, fmt.Errorf("mcp: invalid_actor_type: actor_type must be one of: user, agent, system")
	}
	actorID := params.ActorID

	// Step 3 — Build TranslatedPayload.
	payloadID := newID()
	requestID := newID()

	tp := destination.TranslatedPayload{
		PayloadID:        payloadID,
		RequestID:        requestID,
		Source:           src.Name,
		Subject:          subject,
		Namespace:        src.Namespace,
		Destination:      dest,
		Collection:       params.Collection,
		Content:          params.Content,
		Role:             "user", // MCP writes default to user role
		Timestamp:        time.Now().UTC(),
		IdempotencyKey:   params.IdempotencyKey,
		SchemaVersion:    1,
		TransformVersion: "1.0",
		ActorType:        actorType,
		ActorID:          actorID,
	}
	tp.Embedding = d.embedContent(ctx, payloadID, tp.Content)

	// DEF.2 — Tier-0 immune scan on MCP write path.
	if d.immuneScanner != nil {
		metaAny := make(map[string]any, len(tp.Metadata))
		for k, v := range tp.Metadata {
			metaAny[k] = v
		}
		scan := d.immuneScanner.ScanWrite(tp.Content, metaAny, tp.Embedding)
		switch scan.Action {
		case "quarantine", "reject":
			d.logger.Info("mcp: write quarantined by Tier-0 immune scanner",
				"component", "immune",
				"payload_id", payloadID,
				"rule", scan.Rule,
				"action", scan.Action,
			)
			if d.quarantineStore != nil {
				metaBytes, _ := json.Marshal(tp.Metadata)
				rec := quarantine.Record{
					ID:                quarantine.NewID(),
					OriginalPayloadID: payloadID,
					Content:           tp.Content,
					MetadataJSON:      string(metaBytes),
					SourceName:        src.Name,
					AgentID:           actorID,
					QuarantineReason:  scan.Details,
					RuleID:            scan.Rule,
					QuarantinedAtMs:   time.Now().UnixMilli(),
				}
				if err := d.quarantineStore.Insert(rec); err != nil {
					d.logger.Error("mcp: quarantine store insert failed",
						"component", "quarantine",
						"error", err,
					)
				}
			}
			d.emitControlEvent(
				audit.ControlEventMemoryQuarantined,
				actorID, payloadID, "memory",
				actorID, scan.Rule,
				"quarantined", scan.Details,
				map[string]string{"source": src.Name, "scan_action": scan.Action},
			)
			return mcp.WriteResult{PayloadID: payloadID, Status: "accepted"}, nil
		case "normalize":
			if scan.NormalizedContent != "" {
				tp.Content = scan.NormalizedContent
			}
		}
	}

	payloadBytes, err := json.Marshal(tp)
	if err != nil {
		return mcp.WriteResult{}, fmt.Errorf("mcp: marshal payload: %w", err)
	}

	// Step 4 — WAL append. If WAL fails, return error — do NOT enqueue.
	entry := wal.Entry{
		PayloadID:      payloadID,
		IdempotencyKey: params.IdempotencyKey,
		Source:         src.Name,
		Destination:    dest,
		Subject:        subject,
		ActorType:      actorType,
		ActorID:        actorID,
		Payload:        payloadBytes,
	}
	if err := d.wal.Append(entry); err != nil {
		d.logger.Error("mcp: WAL append failed",
			"component", "mcp",
			"source", src.Name,
			"payload_id", payloadID,
			"error", err,
		)
		d.metrics.ErrorsTotal.WithLabelValues("wal_append").Inc()
		return mcp.WriteResult{}, fmt.Errorf("mcp: WAL append: %w", err)
	}

	// Step 5 — Non-blocking enqueue.
	if err := d.queue.Enqueue(entry); err != nil {
		d.logger.Warn("mcp: queue full — load shedding",
			"component", "mcp",
			"source", src.Name,
			"payload_id", payloadID,
		)
		// WAL entry is durable. Return error so the caller can retry.
		return mcp.WriteResult{}, fmt.Errorf("mcp: queue full; data is durable in WAL and will be replayed on restart")
	}

	d.logger.Info("mcp: write accepted",
		"component", "mcp",
		"source", src.Name,
		"payload_id", payloadID,
		"subject", subject,
	)
	d.metrics.ThroughputPerSource.WithLabelValues(src.Name).Inc()

	// Emit interaction record for MCP write path.
	// Reference: Tech Spec Addendum Section A2.4.
	d.emitAuditRecord(audit.InteractionRecord{
		RecordID:       audit.NewRecordID(),
		RequestID:      requestID,
		Timestamp:      time.Now().UTC(),
		Source:         src.Name,
		ActorType:      actorType,
		ActorID:        actorID,
		OperationType:  "write",
		Endpoint:       "mcp:write",
		HTTPMethod:     "MCP",
		HTTPStatusCode: 200,
		PayloadID:      payloadID,
		Destination:    dest,
		Subject:        subject,
		PolicyDecision: "allowed",
	})

	return mcp.WriteResult{PayloadID: payloadID, Status: "accepted"}, nil
}

// Search executes the 6-stage retrieval cascade and returns matching records,
// identical to handleQuery but called directly without HTTP overhead.
//
// Reference: Tech Spec Section 3.4, Section 14.3.
func (d *Daemon) Search(ctx context.Context, params mcp.SearchParams) (mcp.SearchResult, error) {
	cfg := d.getConfig()

	// Look up source by name.
	src := cfg.SourceByName(params.Source)
	if src == nil {
		return mcp.SearchResult{}, fmt.Errorf("mcp: source %q not found in config", params.Source)
	}

	if !src.CanRead {
		return mcp.SearchResult{}, fmt.Errorf("mcp: source %q does not have read permission", params.Source)
	}

	// Resolve destination: use params override, then source default.
	destName := params.Destination
	if destName == "" {
		destName = src.TargetDest
	}

	// Resolve profile.
	profile := params.Profile
	if profile == "" {
		profile = src.DefaultProfile
	}
	if profile == "" {
		profile = cfg.Retrieval.DefaultProfile
	}

	// Normalize query params.
	cq, err := query.Normalize(destination.QueryParams{
		Destination: destName,
		Namespace:   src.Namespace,
		Subject:     params.Subject,
		Q:           params.Q,
		Limit:       params.Limit,
		Profile:     profile,
	})
	if err != nil {
		return mcp.SearchResult{}, fmt.Errorf("mcp: normalize query: %w", err)
	}

	// Execute the 6-stage retrieval cascade.
	runner := query.New(d.querier, d.logger).
		WithExactCache(d.exactCache).
		WithSemanticCache(d.semanticCache).
		WithEmbeddingClient(d.embeddingClient, d.metrics.EmbeddingLatency).
		WithRetrievalConfig(cfg.Retrieval).
		WithDecayCounter(d.metrics.TemporalDecayApplied).
		WithFirewall(d.retrievalFirewall)
	cascResult, err := runner.Run(ctx, src, cq)
	if err != nil {
		return mcp.SearchResult{}, fmt.Errorf("mcp: cascade: %w", err)
	}

	// Stage 0 denial → propagate as error.
	if cascResult.Denial != nil {
		d.emitAuditRecord(audit.InteractionRecord{
			RecordID:       audit.NewRecordID(),
			Timestamp:      time.Now().UTC(),
			Source:         src.Name,
			OperationType:  "query",
			Endpoint:       "mcp:search",
			HTTPMethod:     "MCP",
			HTTPStatusCode: 403,
			Subject:        params.Subject,
			PolicyDecision: "denied",
			PolicyReason:   cascResult.Denial.Reason,
		})
		return mcp.SearchResult{}, fmt.Errorf("mcp: policy_denied: %s", cascResult.Denial.Reason)
	}

	// Emit interaction record for MCP search path.
	mcpSearchRec := audit.InteractionRecord{
		RecordID:         audit.NewRecordID(),
		Timestamp:        time.Now().UTC(),
		Source:           src.Name,
		OperationType:    "query",
		Endpoint:         "mcp:search",
		HTTPMethod:       "MCP",
		HTTPStatusCode:   200,
		Subject:          params.Subject,
		RetrievalProfile: cascResult.Profile,
		StagesHit:        []string{query.StageName(cascResult.RetrievalStage)},
		ResultCount:      len(cascResult.Records),
		CacheHit:         cascResult.RetrievalStage <= 2 && cascResult.RetrievalStage >= 0,
		PolicyDecision:   "allowed",
	}
	if cascResult.FirewallResult != nil && cascResult.FirewallResult.Filtered {
		mcpSearchRec.PolicyDecision = "filtered"
		mcpSearchRec.SensitivityLabelsFiltered = cascResult.FirewallResult.FilteredLabels
		mcpSearchRec.TierFiltered = cascResult.FirewallResult.TierFiltered
	}
	d.emitAuditRecord(mcpSearchRec)

	return mcp.SearchResult{
		Records:             cascResult.Records,
		HasMore:             cascResult.HasMore,
		NextCursor:          cascResult.NextCursor,
		RetrievalStage:      cascResult.RetrievalStage,
		SemanticUnavailable: cascResult.SemanticUnavailable,
	}, nil
}

// Status returns the current daemon health and queue state.
//
// Reference: Tech Spec Section 14.3.
func (d *Daemon) Status(_ context.Context) (mcp.StatusResult, error) {
	queueDepth := 0
	if d.queue != nil {
		queueDepth = d.queue.Len()
	}

	cfg := d.getConfig()
	var sourceNames []string
	for _, s := range cfg.Sources {
		sourceNames = append(sourceNames, s.Name)
	}

	var uptimeSeconds int64
	if !d.startedAt.IsZero() {
		uptimeSeconds = int64(time.Since(d.startedAt).Seconds())
	}

	return mcp.StatusResult{
		Status:     "ok",
		Version:    version.Version,
		QueueDepth: queueDepth,
		Daemon: mcp.StatusDaemon{
			Version:       version.Version,
			UptimeSeconds: uptimeSeconds,
		},
		Tools:    mcp.DefaultStatusTools(),
		Profiles: mcp.DefaultStatusProfiles(),
		Sources:  sourceNames,
		Ingest:   mcp.StatusIngest{Enabled: !cfg.Ingest.KillSwitch && cfg.Ingest.Enabled},
	}, nil
}
