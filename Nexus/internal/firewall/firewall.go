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

// Package firewall implements the Retrieval Firewall: policy-governed access
// control at the retrieval level using sensitivity labels, classification tiers,
// blocked-label enforcement, and namespace isolation.
//
// The firewall operates on metadata only — no content inspection. It is
// deterministic, fast, and auditable.
//
// Reference: Tech Spec Addendum Sections A3.1, A3.4, A3.5, A3.6, A3.7, A3.8.
package firewall

import (
	"log/slog"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/prometheus/client_golang/prometheus"
)

// FilterResult holds the outcome of a PostFilter call.
type FilterResult struct {
	// Records is the filtered set of memories visible to the source.
	Records []destination.TranslatedPayload
	// Filtered is true when at least one memory was removed.
	Filtered bool
	// FilteredLabels is the set of sensitivity labels that caused removals.
	FilteredLabels []string
	// TierFiltered is true when at least one memory was removed due to tier.
	TierFiltered bool
	// CountRemoved is the number of memories removed by the firewall.
	CountRemoved int
	// CountRemaining is the number of memories that passed the firewall.
	CountRemaining int
}

// PreQueryDenial is returned by PreQuery when the query itself is forbidden
// before any data is fetched. The caller should return HTTP 403.
type PreQueryDenial struct {
	Code   string
	Reason string
}

// Error implements the error interface.
func (d *PreQueryDenial) Error() string {
	return d.Code + ": " + d.Reason
}

// RetrievalFirewall enforces sensitivity label, classification tier, and
// namespace isolation policies on retrieval results. All state is held in
// struct fields — no package-level variables.
//
// INVARIANT: blocked_labels are ABSOLUTE. No admin bypass. No override.
// Reference: Tech Spec Addendum Section A3.5.
type RetrievalFirewall struct {
	enabled   bool
	tierOrder []string            // e.g. ["public", "internal", "confidential", "restricted"]
	tierIndex map[string]int      // tier name → ordinal index (lower = less sensitive)
	logger    *slog.Logger

	// Metrics — all optional (nil-safe).
	filteredTotal *prometheus.CounterVec // labels: source, label
	deniedTotal   *prometheus.CounterVec // labels: source
	latency       *prometheus.HistogramVec // labels: source
}

// New creates a RetrievalFirewall from the daemon-level configuration. When
// cfg.Enabled is false, PreQuery and PostFilter are no-ops with zero overhead.
func New(cfg config.DaemonRetrievalFirewallConfig, logger *slog.Logger) *RetrievalFirewall {
	if logger == nil {
		logger = slog.Default()
	}
	tierIndex := make(map[string]int, len(cfg.TierOrder))
	for i, t := range cfg.TierOrder {
		tierIndex[t] = i
	}
	return &RetrievalFirewall{
		enabled:   cfg.Enabled,
		tierOrder: cfg.TierOrder,
		tierIndex: tierIndex,
		logger:    logger,
	}
}

// WithMetrics attaches Prometheus metrics to the firewall.
func (fw *RetrievalFirewall) WithMetrics(
	filteredTotal *prometheus.CounterVec,
	deniedTotal *prometheus.CounterVec,
	latency *prometheus.HistogramVec,
) *RetrievalFirewall {
	fw.filteredTotal = filteredTotal
	fw.deniedTotal = deniedTotal
	fw.latency = latency
	return fw
}

// Enabled returns whether the retrieval firewall is active.
func (fw *RetrievalFirewall) Enabled() bool {
	return fw.enabled
}

// PreQuery performs Stage 0 access-level checks before any data is fetched.
// Returns nil when the query is allowed, or a *PreQueryDenial when blocked.
//
// Checks:
//   - If the source has a max_classification_tier that is not recognized in
//     tier_order, the query is denied (unknown tiers = maximally restricted).
//
// Reference: Tech Spec Addendum Section A3.5 — Pre-query.
func (fw *RetrievalFirewall) PreQuery(src *config.Source, namespace string) *PreQueryDenial {
	if !fw.enabled {
		return nil
	}

	pol := src.Policy.RetrievalFirewall

	// Namespace isolation: if visible_namespaces is set and cross_namespace_read
	// is false, the queried namespace must be in the visible list.
	// Reference: Tech Spec Addendum Section A3.6.
	if len(pol.VisibleNamespaces) > 0 && !pol.CrossNamespaceRead && namespace != "" {
		if !containsString(pol.VisibleNamespaces, namespace) {
			if fw.deniedTotal != nil {
				fw.deniedTotal.WithLabelValues(src.Name).Inc()
			}
			return &PreQueryDenial{
				Code:   "retrieval_firewall_denied",
				Reason: "namespace not in visible_namespaces for this source",
			}
		}
	}

	// Verify max_classification_tier is a recognized tier. Unknown tier =
	// maximally restricted → deny. This catches misconfiguration early.
	if pol.MaxClassificationTier != "" {
		if _, ok := fw.tierIndex[pol.MaxClassificationTier]; !ok {
			if fw.deniedTotal != nil {
				fw.deniedTotal.WithLabelValues(src.Name).Inc()
			}
			return &PreQueryDenial{
				Code:   "retrieval_firewall_denied",
				Reason: "tier_exceeds_maximum",
			}
		}
	}

	return nil
}

// PostFilter removes memories that the source is not permitted to see based on
// blocked_labels, max_classification_tier, required_labels, and namespace
// isolation rules. It returns a FilterResult describing what was removed.
//
// INVARIANT: blocked_labels are ABSOLUTE. No admin bypass. No debug bypass.
// INVARIANT: At most 0.1ms per result — metadata only, no content inspection.
//
// Reference: Tech Spec Addendum Section A3.5 — Post-retrieval.
func (fw *RetrievalFirewall) PostFilter(
	src *config.Source,
	records []destination.TranslatedPayload,
) FilterResult {
	if !fw.enabled || len(records) == 0 {
		return FilterResult{
			Records:        records,
			CountRemaining: len(records),
		}
	}

	start := time.Now()
	pol := src.Policy.RetrievalFirewall

	// Use precomputed sets from config load (populated in config/loader.go).
	// Falls back to building sets on the fly if they are nil (e.g. in tests
	// that construct Source structs directly without going through Load).
	blockedSet := pol.BlockedLabelsSet
	if blockedSet == nil {
		blockedSet = makeStringSet(pol.BlockedLabels)
	}
	requiredSet := pol.RequiredLabelsSet
	if requiredSet == nil {
		requiredSet = makeStringSet(pol.RequiredLabels)
	}
	namespaceSet := pol.VisibleNamespacesSet
	if namespaceSet == nil {
		namespaceSet = makeStringSet(pol.VisibleNamespaces)
	}

	maxTierIdx := -1
	if pol.MaxClassificationTier != "" {
		if idx, ok := fw.tierIndex[pol.MaxClassificationTier]; ok {
			maxTierIdx = idx
		}
		// Unknown max tier = maximally restricted (idx stays -1, blocks everything).
	}

	kept := make([]destination.TranslatedPayload, 0, len(records))
	filteredLabelsSet := make(map[string]struct{})
	tierFiltered := false

	for i := range records {
		rec := &records[i]

		// 1. Namespace isolation: if visible_namespaces is set and
		// cross_namespace_read is false, only visible namespaces pass.
		// Reference: Tech Spec Addendum Section A3.6.
		if len(namespaceSet) > 0 && !pol.CrossNamespaceRead {
			if _, visible := namespaceSet[rec.Namespace]; !visible {
				continue
			}
		}

		// 2. blocked_labels: ANY match → remove. ABSOLUTE. No exceptions.
		// Reference: Tech Spec Addendum Section A3.5.
		blocked := false
		if len(blockedSet) > 0 {
			for _, label := range rec.SensitivityLabels {
				if _, isBlocked := blockedSet[label]; isBlocked {
					blocked = true
					filteredLabelsSet[label] = struct{}{}
				}
			}
		}
		if blocked {
			if fw.filteredTotal != nil {
				for label := range filteredLabelsSet {
					fw.filteredTotal.WithLabelValues(src.Name, label).Inc()
				}
			}
			continue
		}

		// 3. max_classification_tier: memory tier exceeds source max → remove.
		// Reference: Tech Spec Addendum Section A3.5.
		if maxTierIdx >= 0 {
			memTier := rec.ClassificationTier
			if memTier == "" {
				memTier = "public"
			}
			memIdx, ok := fw.tierIndex[memTier]
			if !ok {
				// Unknown tier on memory = maximally restricted → remove.
				tierFiltered = true
				continue
			}
			if memIdx > maxTierIdx {
				tierFiltered = true
				continue
			}
		}

		// 4. required_labels: if non-empty, memory must have ALL required labels.
		// Reference: Tech Spec Addendum Section A3.5.
		if len(requiredSet) > 0 {
			memLabels := makeStringSet(rec.SensitivityLabels)
			missing := false
			for req := range requiredSet {
				if _, has := memLabels[req]; !has {
					missing = true
					break
				}
			}
			if missing {
				continue
			}
		}

		kept = append(kept, records[i])
	}

	elapsed := time.Since(start)
	if fw.latency != nil {
		fw.latency.WithLabelValues(src.Name).Observe(elapsed.Seconds())
	}

	// Collect filtered labels for the interaction record.
	filteredLabels := make([]string, 0, len(filteredLabelsSet))
	for label := range filteredLabelsSet {
		filteredLabels = append(filteredLabels, label)
	}

	removed := len(records) - len(kept)
	return FilterResult{
		Records:        kept,
		Filtered:       removed > 0,
		FilteredLabels: filteredLabels,
		TierFiltered:   tierFiltered,
		CountRemoved:   removed,
		CountRemaining: len(kept),
	}
}

// makeStringSet converts a string slice to a set for O(1) lookups.
func makeStringSet(ss []string) map[string]struct{} {
	if len(ss) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}

// containsString reports whether s appears in slice.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// TierIndex returns the ordinal index for a tier name, or -1 if unknown.
// Exported for testing.
func (fw *RetrievalFirewall) TierIndex(tier string) int {
	if idx, ok := fw.tierIndex[tier]; ok {
		return idx
	}
	return -1
}
