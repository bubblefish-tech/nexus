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

package query

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	"github.com/bubblefish-tech/nexus/internal/cache"
	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/embedding"
	"github.com/bubblefish-tech/nexus/internal/firewall"
	"github.com/prometheus/client_golang/prometheus"
)

// PolicyDenial is returned by Stage 0 when a query request is blocked by
// policy. The caller (typically the HTTP handler) should translate Code into
// an appropriate HTTP 403 response.
//
// Reference: Tech Spec Section 3.4 — Stage 0.
type PolicyDenial struct {
	// Code is the machine-readable error code sent to the client.
	Code string
	// Reason is the human-readable explanation sent to the client.
	Reason string
}

// Error implements the error interface so PolicyDenial can be used as an error.
func (d *PolicyDenial) Error() string {
	return d.Code + ": " + d.Reason
}

// StageFastPath is the sentinel RetrievalStage value used when the
// exact-subject fast path handles the query. It is distinct from stages 0–5 so
// callers can map it to the string "fast_path" in _nexus metadata.
//
// Reference: Tech Spec Section 3.7.
const StageFastPath = -1

// CascadeResult holds the output of a completed cascade run. When Denial is
// non-nil the caller must return HTTP 403 — all other fields are zero-valued.
//
// Reference: Tech Spec Section 3.4.
type CascadeResult struct {
	// Records is the page of memories returned by the winning stage.
	Records []destination.TranslatedPayload
	// NextCursor is the opaque base64 cursor for the next page. Empty when
	// HasMore is false.
	NextCursor string
	// HasMore is true when additional pages are available.
	HasMore bool
	// Profile is the retrieval profile that was used.
	Profile string
	// RetrievalStage is the numeric stage number that produced results.
	// Zero when Denial is set. StageFastPath (-1) when the exact-subject fast
	// path was used.
	RetrievalStage int
	// Denial is non-nil when Stage 0 blocked the request. The caller must
	// return HTTP 403 with Denial.Code as the error body.
	Denial *PolicyDenial
	// SemanticUnavailable is true when the embedding provider was not
	// configured or was unreachable. Callers should set
	// _nexus.semantic_unavailable = true in the response metadata.
	//
	// Reference: Tech Spec Section 3.4 — Stage 4, Phase 5 Behavioral Contract 4.
	SemanticUnavailable bool
	// SemanticUnavailableReason is a human-readable explanation for why semantic
	// search was skipped. Set when SemanticUnavailable is true.
	SemanticUnavailableReason string

	// FirewallResult holds the outcome of the retrieval firewall PostFilter.
	// Non-nil when the firewall is enabled and executed. The handler uses this
	// to populate the interaction record and set _nexus.retrieval_firewall_filtered.
	// Reference: Tech Spec Addendum Section A3.5.
	FirewallResult *firewall.FilterResult

	// Debug holds optional per-stage diagnostic information. Populated only
	// when the cascade is run in debug mode. Reference: Tech Spec Section 7.3.
	Debug *DebugInfo

	// ClusterExpanded is true when cluster-aware profile expanded results
	// to include cluster members.
	// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.4.
	ClusterExpanded bool

	// Conflict is true when expanded cluster members have different content
	// hashes, indicating contradictory writes.
	// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.4.
	Conflict bool

	// ClusterCount is the number of distinct clusters in the result set.
	ClusterCount int
}

// DebugInfo holds per-stage diagnostic data for the _nexus.debug response.
// Reference: Tech Spec Section 7.3.
type DebugInfo struct {
	StagesHit          []string           `json:"stages_hit"`
	CandidatesPerStage map[string]int     `json:"candidates_per_stage"`
	PerStageLatencyMs  map[string]float64 `json:"per_stage_latency_ms"`
	CacheHit           bool               `json:"cache_hit"`
	CacheType          string             `json:"cache_type"`
	TemporalDecayConfig struct {
		Mode             string  `json:"mode"`
		HalfLifeDays     float64 `json:"half_life_days"`
		OverSampleFactor int     `json:"over_sample_factor"`
	} `json:"temporal_decay_config"`
	TotalLatencyMs float64 `json:"total_latency_ms"`
}

// StageName returns the human-readable stage name for use in _nexus.stage
// response metadata.
//
// Reference: Tech Spec Section 3.7, Section 7.2.
func StageName(stage int) string {
	switch stage {
	case StageFastPath:
		return "fast_path"
	case 1:
		return "exact_cache"
	case 2:
		return "semantic_cache"
	case 3:
		return "structured"
	case 4:
		return "semantic"
	case 5:
		return "hybrid_merge"
	default:
		return "unknown"
	}
}

// CascadeRunner executes the 6-stage retrieval cascade. All state is held in
// struct fields; there are no package-level variables.
//
// Reference: Tech Spec Section 3.4.
type CascadeRunner struct {
	querier          destination.Querier
	logger           *slog.Logger
	exactCache       *cache.ExactCache
	semanticCache    *cache.SemanticCache  // Stage 2 — Phase 6
	embeddingClient  embedding.EmbeddingClient
	embeddingLatency prometheus.Observer   // optional; nil is safe
	retrieval        config.RetrievalConfig // Phase 6: over-sample factor + decay
	decayCounter     prometheus.Counter    // Phase 6: nexus_temporal_decay_applied_total
	destinations     map[string]*config.Destination // Phase R-7: per-dest/collection decay
	debug            bool // when true, populate DebugInfo in CascadeResult
	fw               *firewall.RetrievalFirewall  // Phase R-31: retrieval firewall
	clusterQuerier   destination.ClusterQuerier   // Phase 3: cluster expansion
	sketchPrefilter  *SketchPrefilterConfig       // Stage 3.5: BF-Sketch prefilter (nil = disabled)
	bm25Searcher     BM25Searcher                 // Stage 3.75: BM25 sparse retrieval (nil = disabled)
}

// New creates a CascadeRunner backed by the provided querier. If logger is nil
// the default slog logger is used.
func New(querier destination.Querier, logger *slog.Logger) *CascadeRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &CascadeRunner{
		querier: querier,
		logger:  logger,
	}
}

// WithExactCache attaches an ExactCache to the runner, enabling Stage 1
// retrieval. Returns the runner for method chaining.
//
// Reference: Tech Spec Section 3.4 — Stage 1.
func (cr *CascadeRunner) WithExactCache(c *cache.ExactCache) *CascadeRunner {
	cr.exactCache = c
	return cr
}

// WithSemanticCache attaches a SemanticCache to the runner, enabling Stage 2
// semantic cache retrieval. Returns the runner for method chaining.
//
// Reference: Tech Spec Section 3.4 — Stage 2.
func (cr *CascadeRunner) WithSemanticCache(c *cache.SemanticCache) *CascadeRunner {
	cr.semanticCache = c
	return cr
}

// WithEmbeddingClient attaches an EmbeddingClient to the runner, enabling
// Stage 2 (semantic cache) and Stage 4 (semantic retrieval). A nil client is
// valid and results in graceful degradation: Stages 2+4 are skipped and
// CascadeResult.SemanticUnavailable is set to true.
//
// embeddingLatency is an optional prometheus.Observer for recording embedding
// call duration. Pass nil to disable metric recording.
//
// Reference: Tech Spec Section 3.4 — Stage 4, Phase 5 Behavioral Contract 5.
func (cr *CascadeRunner) WithEmbeddingClient(c embedding.EmbeddingClient, embeddingLatency prometheus.Observer) *CascadeRunner {
	cr.embeddingClient = c
	cr.embeddingLatency = embeddingLatency
	return cr
}

// WithRetrievalConfig injects the global retrieval configuration used to
// determine the over-sample factor and default temporal decay parameters.
// Returns the runner for method chaining.
//
// Reference: Tech Spec Section 3.5, Section 3.6.
func (cr *CascadeRunner) WithRetrievalConfig(cfg config.RetrievalConfig) *CascadeRunner {
	cr.retrieval = cfg
	return cr
}

// WithDecayCounter attaches a Prometheus counter that is incremented each time
// temporal decay reranking is applied in Stage 5. Pass nil to disable.
//
// Reference: Tech Spec Section 11.3 — nexus_temporal_decay_applied_total.
func (cr *CascadeRunner) WithDecayCounter(c prometheus.Counter) *CascadeRunner {
	cr.decayCounter = c
	return cr
}

// WithDestinations attaches the destination configuration map to the runner,
// enabling per-destination and per-collection temporal decay resolution in
// Stage 5. Returns the runner for method chaining.
//
// Reference: Tech Spec Section 3.6.
func (cr *CascadeRunner) WithDestinations(dests map[string]*config.Destination) *CascadeRunner {
	cr.destinations = dests
	return cr
}

// WithDebug enables debug mode. When true, the CascadeResult.Debug field is
// populated with per-stage diagnostic information.
// Reference: Tech Spec Section 7.3.
func (cr *CascadeRunner) WithDebug(enabled bool) *CascadeRunner {
	cr.debug = enabled
	return cr
}

// WithClusterQuerier attaches a ClusterQuerier to the runner, enabling
// cluster-aware profile expansion. When set and the profile is "cluster-aware",
// results are expanded to include all cluster members and conflict detection is
// applied. Returns the runner for method chaining.
//
// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.4.
func (cr *CascadeRunner) WithClusterQuerier(cq destination.ClusterQuerier) *CascadeRunner {
	cr.clusterQuerier = cq
	return cr
}

// WithFirewall attaches a RetrievalFirewall to the runner, enabling pre-query
// tier/namespace checks in Stage 0 and post-retrieval label/tier filtering
// after Stage 5. When fw is nil or not enabled, all firewall logic is skipped.
//
// Reference: Tech Spec Addendum Section A3.5.
func (cr *CascadeRunner) WithFirewall(fw *firewall.RetrievalFirewall) *CascadeRunner {
	cr.fw = fw
	return cr
}

// WithSketchPrefilter attaches a BF-Sketch prefilter configuration, enabling
// Stage 3.5 in the cascade. Returns the runner for method chaining.
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.7.
func (cr *CascadeRunner) WithSketchPrefilter(cfg *SketchPrefilterConfig) *CascadeRunner {
	cr.sketchPrefilter = cfg
	return cr
}

// WithBM25Searcher attaches a BM25 sparse keyword searcher, enabling
// Stage 3.75 in the cascade and RRF fusion in Stage 5.
func (cr *CascadeRunner) WithBM25Searcher(s BM25Searcher) *CascadeRunner {
	cr.bm25Searcher = s
	return cr
}

// Run executes the 6-stage retrieval cascade for the given source policy and
// canonical query. Stages execute strictly in order 0 → 5. Each stage may
// produce results and short-circuit, pass through to the next stage, or block
// the request entirely (Stage 0 only).
//
// Reference: Tech Spec Section 3.4.
func (cr *CascadeRunner) Run(ctx context.Context, src *config.Source, q CanonicalQuery) (CascadeResult, error) {
	cascadeStart := time.Now()

	// Debug tracking — only allocate when debug mode is on.
	var dbg *DebugInfo
	if cr.debug {
		dbg = &DebugInfo{
			StagesHit:          make([]string, 0, 6),
			CandidatesPerStage: make(map[string]int),
			PerStageLatencyMs:  make(map[string]float64),
		}
	}

	// ── Stage 0: Policy Gate — always runs ──────────────────────────────────
	// Returns HTTP 403 with a specific denial reason when blocked.
	// Reference: Tech Spec Section 3.4 — Stage 0.
	s0Start := time.Now()
	if denial := runStage0(src, q); denial != nil {
		cr.logger.Warn("query: cascade Stage 0 denied request",
			"component", "cascade",
			"source", src.Name,
			"destination", q.Destination,
			"code", denial.Code,
		)
		return CascadeResult{Denial: denial, Profile: q.Profile}, nil
	}

	// ── Stage 0+: Retrieval Firewall PreQuery ───────────────────────────────
	// Checks namespace isolation and tier validity BEFORE any data is fetched.
	// Reference: Tech Spec Addendum Section A3.5 — Pre-query.
	if cr.fw != nil && cr.fw.Enabled() {
		if fwDenial := cr.fw.PreQuery(src, q.Namespace); fwDenial != nil {
			cr.logger.Warn("query: retrieval firewall denied request",
				"component", "cascade",
				"source", src.Name,
				"code", fwDenial.Code,
				"reason", fwDenial.Reason,
			)
			return CascadeResult{
				Denial:  &PolicyDenial{Code: fwDenial.Code, Reason: fwDenial.Reason},
				Profile: q.Profile,
			}, nil
		}
	}

	if dbg != nil {
		dbg.StagesHit = append(dbg.StagesHit, "policy_gate")
		dbg.PerStageLatencyMs["policy_gate"] = float64(time.Since(s0Start).Microseconds()) / 1000.0
	}

	// ── Fast Path: Exact-Subject Short-Circuit ──────────────────────────────
	// Active when: query shape is subject + limit only (no Q, no actor_type,
	// no cursor). Bypasses the full cascade with a direct indexed query.
	// Returns empty on 0 results — does NOT fall through to the cascade.
	//
	// Reference: Tech Spec Section 3.7.
	if IsFastPath(q) {
		records, nextCursor, hasMore, err := runStage3(ctx, cr.querier, q)
		if err != nil {
			return CascadeResult{}, err
		}
		cr.logger.Debug("query: fast path executed",
			"component", "cascade",
			"source", src.Name,
			"destination", q.Destination,
			"subject", q.Subject,
			"results", len(records),
		)
		res := CascadeResult{
			Records:        records,
			NextCursor:     nextCursor,
			HasMore:        hasMore,
			Profile:        q.Profile,
			RetrievalStage: StageFastPath,
		}
		// Apply retrieval firewall PostFilter to fast path results.
		if cr.fw != nil && cr.fw.Enabled() && len(records) > 0 {
			fr := cr.fw.PostFilter(src, records)
			res.Records = fr.Records
			res.FirewallResult = &fr
		}
		return res, nil
	}

	// ── Stage 1: Exact Cache ────────────────────────────────────────────────
	// Active when: ProfileEnabled(1, profile) AND policy.read_from_cache = true.
	// Key: SHA256(scope_hash + dest + params + policy_hash).
	// Scope isolation: source identity is embedded in the key so source A
	// cannot retrieve source B's cached entries.
	// Watermark check: entries are stale when a write was delivered after they
	// were cached; stale entries produce a miss.
	// Reference: Tech Spec Section 3.4 — Stage 1, Section 3.5.
	var cacheKey [32]byte
	useCache := cr.exactCache != nil && src.Policy.Cache.ReadFromCache && ProfileEnabled(1, q.Profile)
	if useCache {
		ph := sourcePolicyHash(src.Policy.Cache)
		cacheKey = cache.BuildKey(src.Name, q.Destination, q.Profile,
			q.Namespace, q.Subject, q.Q, q.Limit, q.CursorOffset, ph)
		if entry, ok := cr.exactCache.Get(cacheKey, q.Destination); ok {
			cr.logger.Debug("query: Stage 1 cache hit",
				"component", "cascade",
				"source", src.Name,
				"destination", q.Destination,
			)
			recs := entry.Records
			result := CascadeResult{
				Records:        recs,
				NextCursor:     entry.NextCursor,
				HasMore:        entry.HasMore,
				Profile:        q.Profile,
				RetrievalStage: 1,
			}
			// Apply retrieval firewall PostFilter to cached results.
			if cr.fw != nil && cr.fw.Enabled() && len(recs) > 0 {
				fr := cr.fw.PostFilter(src, recs)
				result.Records = fr.Records
				result.FirewallResult = &fr
			}
			if dbg != nil {
				dbg.StagesHit = append(dbg.StagesHit, "exact_cache")
				dbg.CandidatesPerStage["exact_cache"] = len(entry.Records)
				dbg.CacheHit = true
				dbg.CacheType = "exact"
				dbg.TotalLatencyMs = float64(time.Since(cascadeStart).Microseconds()) / 1000.0
				result.Debug = dbg
			}
			return result, nil
		}
	}

	// ── Pre-Semantic: Eligibility check and embedding computation ────────────
	// Embedding is computed once and shared by Stage 2 (semantic cache lookup)
	// and Stage 4 (semantic vector search). If computation fails, both stages
	// degrade gracefully.
	//
	// INVARIANT: NEVER propagate embedding errors to the caller. Set
	// semanticSkipped = true and continue with Stage 3 results.
	//
	// Reference: Tech Spec Section 3.4 — Stages 2 + 4.
	var (
		queryVec        []float32
		queryVecOK      bool // true when queryVec is populated and usable
		semanticSkipped bool
		skipReason      string
		semanticSearcher destination.SemanticSearcher // non-nil when Stage 4 can run
	)

	if cr.embeddingClient == nil {
		semanticSkipped = true
		skipReason = "embedding not configured"
	} else if !ProfileEnabled(4, q.Profile) {
		semanticSkipped = true
		skipReason = "profile=" + q.Profile + " skips semantic retrieval"
	} else {
		// Check destination semantic search capability before computing embedding.
		ss, isSearcher := cr.querier.(destination.SemanticSearcher)
		if !isSearcher || !ss.CanSemanticSearch() {
			semanticSkipped = true
			skipReason = "destination does not support semantic search"
		} else {
			semanticSearcher = ss
			// Compute embedding (shared by Stage 2 and Stage 4).
			t0 := time.Now()
			var embedErr error
			queryVec, embedErr = cr.embeddingClient.Embed(ctx, q.Q)
			elapsed := time.Since(t0)
			if cr.embeddingLatency != nil {
				cr.embeddingLatency.Observe(elapsed.Seconds())
			}
			if embedErr != nil {
				semanticSkipped = true
				skipReason = "embedding provider unavailable"
				cr.logger.Warn("query: embedding failed; degrading gracefully",
					"component", "cascade",
					"source", src.Name,
					"error", embedErr,
				)
			} else if len(queryVec) == 0 {
				semanticSkipped = true
				skipReason = "empty embedding vector returned"
			} else {
				queryVecOK = true
			}
		}
	}

	// ── Stage 2: Semantic Cache ─────────────────────────────────────────────
	// Active when: ProfileEnabled(2, profile) AND embedding computed AND
	// semantic cache configured AND policy allows reads from cache.
	//
	// On a hit: short-circuit and return cached results (skipping Stages 3–5).
	// On a miss: continue to Stage 3.
	//
	// Reference: Tech Spec Section 3.4 — Stage 2, Section 3.5.
	if ProfileEnabled(2, q.Profile) && queryVecOK && cr.semanticCache != nil && src.Policy.Cache.ReadFromCache {
		scopeKey := cache.SemanticScopeKey(src.Name, q.Destination, q.Profile, q.Namespace)
		threshold := src.Policy.Cache.SemanticSimilarityThreshold
		if threshold <= 0 {
			threshold = 0.92 // default per Tech Spec Section 3.4 — Stage 2
		}
		if entry, ok := cr.semanticCache.Get(scopeKey, queryVec, q.Destination, threshold); ok {
			cr.logger.Debug("query: Stage 2 semantic cache hit",
				"component", "cascade",
				"source", src.Name,
				"destination", q.Destination,
			)
			recs := entry.Records
			result := CascadeResult{
				Records:        recs,
				NextCursor:     entry.NextCursor,
				HasMore:        entry.HasMore,
				Profile:        q.Profile,
				RetrievalStage: 2,
			}
			// Apply retrieval firewall PostFilter to cached results.
			if cr.fw != nil && cr.fw.Enabled() && len(recs) > 0 {
				fr := cr.fw.PostFilter(src, recs)
				result.Records = fr.Records
				result.FirewallResult = &fr
			}
			if dbg != nil {
				dbg.StagesHit = append(dbg.StagesHit, "semantic_cache")
				dbg.CandidatesPerStage["semantic_cache"] = len(entry.Records)
				dbg.CacheHit = true
				dbg.CacheType = "semantic"
				dbg.TotalLatencyMs = float64(time.Since(cascadeStart).Microseconds()) / 1000.0
				result.Debug = dbg
			}
			return result, nil
		}
	}

	// ── Stage 3: Structured Lookup ──────────────────────────────────────────
	// Active when: metadata filters present OR exact-subject fast path.
	// Uses parameterized WHERE clauses — no SQL string concatenation ever.
	//
	// Over-sampling: when Stage 5 may run (Stages 3 and 4 both active), fetch
	// more than max_results so temporal decay reranking has a larger candidate
	// pool. The result is trimmed to q.Limit by Stage 5.
	//
	// Reference: Tech Spec Section 3.4 — Stage 3.
	fetchLimit := q.Limit
	if queryVecOK {
		// Over-sample for hybrid merge (Stage 5).
		overSample := cr.retrieval.OverSampleFactor
		if overSample <= 0 {
			overSample = profileOverSample(q.Profile)
		}
		if overSample > fetchLimit {
			fetchLimit = overSample
		}
		if fetchLimit > destination.MaxQueryLimit {
			fetchLimit = destination.MaxQueryLimit
		}
	}

	overSampledQ := q
	overSampledQ.Limit = fetchLimit

	// ── Stage 3.1: Temporal Bin Filter ──────────────────────────────────────
	// When the query contains temporal language ("yesterday", "last week"),
	// pre-filter Stage 3 results to the matching bin via SQL WHERE clause.
	overSampledQ.TemporalBin = -1
	temporalBin := ExtractTemporalHint(q.Q)
	if temporalBin >= 0 {
		overSampledQ.TemporalBin = temporalBin
		cr.logger.Debug("query: Stage 3.1 temporal bin filter applied",
			"component", "cascade",
			"bin", temporalBin,
			"query", q.Q,
		)
	}

	records, nextCursor, hasMore, err := runStage3(ctx, cr.querier, overSampledQ)
	if err != nil {
		return CascadeResult{}, err
	}

	// ── Stage 3.5: Sketch Prefilter (BF-Sketch) ────────────────────────────
	// Active when: substrate enabled, query has embedding, candidate set
	// exceeds threshold. Reduces candidates via sketch inner-product ranking.
	// Rule 18: errors fall through to Stage 4 with original candidates.
	//
	// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.7.
	if cr.sketchPrefilter != nil && queryVecOK {
		records = stage35SketchPrefilter(
			cr.sketchPrefilter,
			queryVec,
			records,
			200, // prefilter threshold
			100, // top-K
		)
	}

	// ── Stage 3.75: BM25 Sparse Retrieval ──────────────────────────────────
	// Active when: BM25 searcher configured and profile is not "fast"/"wake".
	// Non-fatal: cascade continues with dense-only results on error.
	var bm25Results []BM25Result
	if cr.bm25Searcher != nil && q.Profile != "fast" && q.Profile != "wake" {
		var bm25Err error
		bm25Results, bm25Err = cr.bm25Searcher.BM25Search(ctx, q.Q, q.Namespace, fetchLimit)
		if bm25Err != nil {
			cr.logger.Warn("query: Stage 3.75 BM25 search failed, continuing without sparse results",
				"component", "cascade",
				"source", src.Name,
				"error", bm25Err,
			)
		} else {
			cr.logger.Debug("query: Stage 3.75 BM25 retrieval complete",
				"component", "cascade",
				"source", src.Name,
				"results", len(bm25Results),
			)
		}
	}

	// ── Stage 4: Semantic Retrieval ─────────────────────────────────────────
	// Active when: embedding computed + destination supports semantic search.
	// Uses the pre-computed queryVec from the Pre-Semantic block above.
	//
	// Graceful degradation: any error sets semanticSkipped = true and falls
	// through to Stage 3 results.
	// INVARIANT: NEVER crash or return an error to the caller on Stage 4 failure.
	//
	// Reference: Tech Spec Section 3.4 — Stage 4.
	var stage4Records []destination.ScoredRecord

	if queryVecOK && semanticSearcher != nil {
		searchParams := destination.QueryParams{
			Namespace:   q.Namespace,
			Destination: q.Destination,
			Limit:       fetchLimit, // over-sampled
			Profile:     q.Profile,
		}
		var searchErr error
		stage4Records, searchErr = semanticSearcher.SemanticSearch(ctx, queryVec, searchParams)
		if searchErr != nil {
			semanticSkipped = true
			skipReason = "semantic search error"
			cr.logger.Warn("query: Stage 4 semantic search failed; degrading gracefully",
				"component", "cascade",
				"source", src.Name,
				"error", searchErr,
			)
			// err was set inside; clear so it doesn't surface to caller.
		} else {
			cr.logger.Debug("query: Stage 4 semantic retrieval complete",
				"component", "cascade",
				"source", src.Name,
				"destination", q.Destination,
				"results", len(stage4Records),
			)
		}
	}

	// ── Stage 5: Hybrid Merge + Temporal Decay ──────────────────────────────
	// Active when: Stages 3 AND 4 both produced results.
	// Implements: dedup by payload_id, temporal decay rerank, trim to q.Limit.
	//
	// When only Stage 4 produced results, uses Stage 4 results trimmed to limit.
	// When only Stage 3 produced results (or semantic skipped), uses Stage 3.
	//
	// Reference: Tech Spec Section 3.4 — Stage 5, Section 3.6.
	var (
		finalRecords    []destination.TranslatedPayload
		finalNextCursor string
		finalHasMore    bool
		finalStage      int
	)

	if ProfileEnabled(5, q.Profile) && !semanticSkipped && len(stage4Records) > 0 && len(records) > 0 {
		// ── Stage 5: Both stages have results — full hybrid merge ────────────
		var destDecay config.DestinationDecayConfig
		if cr.destinations != nil {
			if dest := cr.destinations[q.Destination]; dest != nil {
				destDecay = dest.Decay
			}
		}
		decayCfg := ResolveDecay(cr.retrieval, destDecay, q.Collection, src.Policy.Decay, q.Profile)

		if len(bm25Results) > 0 {
			finalRecords = RRFMerge(stage4Records, bm25Results, 60)
			if q.Limit > 0 && len(finalRecords) > q.Limit {
				finalRecords = finalRecords[:q.Limit]
			}
		} else {
			finalRecords = HybridMerge(records, stage4Records, q.Limit, decayCfg.Enabled, decayCfg, time.Now())
		}
		finalNextCursor = "" // Stage 5 result is a full reranked page; no cursor
		finalHasMore = false
		finalStage = 5
		if decayCfg.Enabled && cr.decayCounter != nil {
			cr.decayCounter.Inc()
		}
		cr.logger.Debug("query: Stage 5 hybrid merge complete",
			"component", "cascade",
			"source", src.Name,
			"stage3_count", len(records),
			"stage4_count", len(stage4Records),
			"bm25_count", len(bm25Results),
			"merged_count", len(finalRecords),
			"decay_enabled", decayCfg.Enabled,
		)
	} else if !semanticSkipped && len(stage4Records) > 0 {
		// Stage 4 results only (Stage 3 returned nothing). Trim to q.Limit.
		end := len(stage4Records)
		if q.Limit > 0 && end > q.Limit {
			end = q.Limit
		}
		sem := make([]destination.TranslatedPayload, end)
		for i := 0; i < end; i++ {
			sem[i] = stage4Records[i].Payload
		}
		finalRecords = sem
		finalNextCursor = ""
		finalHasMore = false
		finalStage = 4
	} else {
		// Stage 3 results only (semantic skipped or Stage 4 returned nothing).
		// Trim to the original requested limit.
		trimmed := records
		if q.Limit > 0 && len(trimmed) > q.Limit {
			trimmed = trimmed[:q.Limit]
		}
		finalRecords = trimmed
		finalNextCursor = nextCursor
		finalHasMore = hasMore
		finalStage = 3
	}

	// ── Cluster Expansion (cluster-aware profile) ──────────────────────────
	// When profile = "cluster-aware" and a ClusterQuerier is available,
	// expand each result that has a cluster_id to include all cluster members.
	// Detect conflicts when members have different content hashes.
	//
	// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.4.
	var clusterExpanded bool
	var clusterConflict bool
	var clusterCount int

	if q.Profile == ProfileClusterAware && cr.clusterQuerier != nil && len(finalRecords) > 0 {
		expanded, conflict, nClusters := expandClusters(cr.clusterQuerier, finalRecords, src)
		if len(expanded) > 0 {
			finalRecords = expanded
			clusterExpanded = true
			clusterConflict = conflict
			clusterCount = nClusters
			cr.logger.Debug("query: cluster expansion complete",
				"component", "cascade",
				"source", src.Name,
				"original", len(finalRecords),
				"expanded", len(expanded),
				"conflict", conflict,
				"clusters", nClusters,
			)
		}
	}

	// ── Retrieval Firewall PostFilter ────────────────────────────────────────
	// Removes memories the source cannot see based on blocked_labels,
	// max_classification_tier, required_labels, and namespace isolation.
	// Executes after retrieval, before projection. NOT bypassable.
	//
	// Reference: Tech Spec Addendum Section A3.5 — Post-retrieval.
	var fwResult *firewall.FilterResult
	if cr.fw != nil && cr.fw.Enabled() && len(finalRecords) > 0 {
		fr := cr.fw.PostFilter(src, finalRecords)
		fwResult = &fr
		finalRecords = fr.Records
		if fr.Filtered {
			cr.logger.Info("query: retrieval firewall filtered results",
				"component", "cascade",
				"source", src.Name,
				"removed", fr.CountRemoved,
				"remaining", fr.CountRemaining,
			)
		}
	}

	// ── Exact Cache write-back ───────────────────────────────────────────────
	// Store final results in the exact cache for future identical requests when
	// the source policy permits caching writes.
	if useCache && cr.exactCache != nil && src.Policy.Cache.WriteToCache {
		cr.exactCache.Put(cacheKey, q.Destination, cache.CacheEntry{
			Records:    finalRecords,
			NextCursor: finalNextCursor,
			HasMore:    finalHasMore,
		})
	}

	// ── Semantic Cache write-back ────────────────────────────────────────────
	// Store final results keyed by the query embedding vector so that future
	// semantically similar queries hit Stage 2 directly.
	if queryVecOK && !semanticSkipped && cr.semanticCache != nil && src.Policy.Cache.WriteToCache {
		scopeKey := cache.SemanticScopeKey(src.Name, q.Destination, q.Profile, q.Namespace)
		cr.semanticCache.Put(scopeKey, queryVec, q.Destination, cache.SemanticCacheEntry{
			Records:    finalRecords,
			NextCursor: finalNextCursor,
			HasMore:    finalHasMore,
		})
	}

	result := CascadeResult{
		Records:         finalRecords,
		NextCursor:      finalNextCursor,
		HasMore:         finalHasMore,
		Profile:         q.Profile,
		RetrievalStage:  finalStage,
		FirewallResult:  fwResult,
		ClusterExpanded: clusterExpanded,
		Conflict:        clusterConflict,
		ClusterCount:    clusterCount,
	}

	// Populate debug info for the non-cache path.
	if dbg != nil {
		dbg.StagesHit = append(dbg.StagesHit, "structured_lookup")
		dbg.CandidatesPerStage["structured_lookup"] = len(records)
		if len(stage4Records) > 0 {
			dbg.StagesHit = append(dbg.StagesHit, "semantic_retrieval")
			dbg.CandidatesPerStage["semantic_retrieval"] = len(stage4Records)
		}
		if finalStage == 5 {
			dbg.StagesHit = append(dbg.StagesHit, "hybrid_merge")
			dbg.CandidatesPerStage["hybrid_merge"] = len(finalRecords)
			var destDecay config.DestinationDecayConfig
			if cr.destinations != nil {
				if dest := cr.destinations[q.Destination]; dest != nil {
					destDecay = dest.Decay
				}
			}
			decayCfg := ResolveDecay(cr.retrieval, destDecay, q.Collection, src.Policy.Decay, q.Profile)
			dbg.TemporalDecayConfig.Mode = decayCfg.Mode
			dbg.TemporalDecayConfig.HalfLifeDays = decayCfg.HalfLifeDays
			dbg.TemporalDecayConfig.OverSampleFactor = cr.retrieval.OverSampleFactor
		}
		dbg.TotalLatencyMs = float64(time.Since(cascadeStart).Microseconds()) / 1000.0
		result.Debug = dbg
	}

	if semanticSkipped {
		result.SemanticUnavailable = true
		result.SemanticUnavailableReason = skipReason
	}
	return result, nil
}

// sourcePolicyHash derives a short digest of the policy fields that affect
// result shape. A change in any of these fields produces a different digest,
// causing cache misses for stale policy-shaped results.
//
// The hash covers fields from PolicyCacheConfig that influence what the cache
// serves: ReadFromCache, WriteToCache, MaxTTLSeconds, and the semantic
// similarity threshold used in Stage 2.
func sourcePolicyHash(p config.PolicyCacheConfig) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "rfc=%v\x00wtc=%v\x00ttl=%d\x00sst=%.6f",
		p.ReadFromCache, p.WriteToCache, p.MaxTTLSeconds, p.SemanticSimilarityThreshold)
	return fmt.Sprintf("%x", h.Sum(nil))[:16] // first 8 bytes (16 hex chars) is sufficient
}

// runStage0 enforces the policy gate. It returns a *PolicyDenial when the
// request must be blocked, or nil to allow it to proceed to Stage 1.
//
// Checks (applied in this order):
//  1. src.CanRead must be true.
//  2. If AllowedDestinations is non-empty, q.Destination must be in the list.
//  3. If AllowedOperations is non-empty, "read" must be in the list.
//  4. If AllowedRetrievalModes is non-empty, q.Profile must be in the list.
//
// Reference: Tech Spec Section 3.4 — Stage 0.
func runStage0(src *config.Source, q CanonicalQuery) *PolicyDenial {
	if !src.CanRead {
		return &PolicyDenial{
			Code:   "source_not_permitted_to_read",
			Reason: "this source does not have read permission",
		}
	}
	if len(src.Policy.AllowedDestinations) > 0 && !containsString(src.Policy.AllowedDestinations, q.Destination) {
		return &PolicyDenial{
			Code:   "destination_not_allowed",
			Reason: "destination not permitted for this source",
		}
	}
	if len(src.Policy.AllowedOperations) > 0 && !containsString(src.Policy.AllowedOperations, "read") {
		return &PolicyDenial{
			Code:   "operation_not_allowed",
			Reason: "read operation not permitted for this source",
		}
	}
	if len(src.Policy.AllowedRetrievalModes) > 0 && !containsString(src.Policy.AllowedRetrievalModes, q.Profile) {
		return &PolicyDenial{
			Code:   "retrieval_mode_not_allowed",
			Reason: "retrieval profile not permitted for this source",
		}
	}
	// AllowedProfiles is the Phase R-6 profile whitelist. When non-empty, the
	// requested profile must appear in the list or the request is denied.
	// Reference: Tech Spec Section 3.5 — allowed_profiles.
	if len(src.Policy.AllowedProfiles) > 0 && !containsString(src.Policy.AllowedProfiles, q.Profile) {
		return &PolicyDenial{
			Code:   "policy_denied",
			Reason: "retrieval profile not in allowed_profiles for this source",
		}
	}
	return nil
}

// expandClusters takes the initial result set and, for each record that has a
// cluster_id, fetches all cluster members from the destination. It deduplicates
// by payload_id and detects conflicts (different content hashes within a cluster).
//
// Returns the expanded records, whether any conflict was found, and the number
// of distinct clusters represented.
//
// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.4.
func expandClusters(cq destination.ClusterQuerier, records []destination.TranslatedPayload, src *config.Source) ([]destination.TranslatedPayload, bool, int) {
	seen := make(map[string]bool, len(records)*2)
	expanded := make([]destination.TranslatedPayload, 0, len(records)*2)
	clusterIDs := make(map[string]bool)
	conflict := false

	// First pass: add all original records and note their cluster IDs.
	clusterIDsToExpand := make(map[string]bool)
	for _, r := range records {
		if !seen[r.PayloadID] {
			seen[r.PayloadID] = true
			expanded = append(expanded, r)
		}
		if r.ClusterID != "" {
			clusterIDsToExpand[r.ClusterID] = true
			clusterIDs[r.ClusterID] = true
		}
	}

	// Second pass: fetch and add cluster members.
	for cid := range clusterIDsToExpand {
		members, err := cq.QueryClusterMembers(destination.ClusterQueryParams{
			ClusterID:  cid,
			TierFilter: true,
			SourceTier: src.Tier,
		})
		if err != nil {
			// Degrade gracefully — skip this cluster's expansion.
			continue
		}

		// Conflict detection: check if members have different content hashes.
		contentHashes := make(map[[32]byte]bool)
		for _, m := range members {
			if m.ClusterRole == "superseded" {
				continue
			}
			h := sha256.Sum256([]byte(m.Content))
			contentHashes[h] = true

			if !seen[m.PayloadID] {
				seen[m.PayloadID] = true
				expanded = append(expanded, m)
			}
		}
		if len(contentHashes) > 1 {
			conflict = true
		}
	}

	return expanded, conflict, len(clusterIDs)
}

// containsString reports whether s appears in slice. Linear scan — policy
// lists are short (typically < 10 items) so no map overhead is warranted.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
