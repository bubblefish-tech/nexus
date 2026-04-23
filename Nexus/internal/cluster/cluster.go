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

// Package cluster implements async cluster assignment for BubbleFish Nexus.
//
// Entries are assigned to clusters based on SimHash LSH bucket prefiltering
// followed by cosine similarity. Entries with similarity >= 0.92 to an existing
// cluster member join that cluster; otherwise a new single-member cluster is
// created.
//
// Clusters never span tiers. Cluster size is capped at 16; overflow is resolved
// by superseding the oldest member (deterministic by timestamp).
//
// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.3.
package cluster

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"sort"

	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/lsh"
)

const (
	// SimilarityThreshold is the minimum cosine similarity for an entry to
	// join an existing cluster.
	SimilarityThreshold = 0.92

	// MaxClusterSize is the maximum number of non-superseded members in a
	// cluster. When exceeded, the oldest member is superseded.
	MaxClusterSize = 16

	// BucketCandidateLimit is the maximum number of candidates fetched from
	// the same (tier, lsh_bucket) for similarity comparison.
	BucketCandidateLimit = 200
)

// Assigner assigns incoming entries to clusters. It is safe for concurrent use
// only if the underlying ClusterQuerier serialises writes (which SQLite does
// via MaxOpenConns=1).
//
// All state is held in struct fields; there are no package-level variables.
type Assigner struct {
	seeds  *lsh.TierSeeds
	dest   destination.ClusterQuerier
	logger *slog.Logger
}

// NewAssigner creates a cluster Assigner backed by the given tier seeds and
// destination. If logger is nil the default slog logger is used.
func NewAssigner(seeds *lsh.TierSeeds, dest destination.ClusterQuerier, logger *slog.Logger) *Assigner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Assigner{
		seeds:  seeds,
		dest:   dest,
		logger: logger,
	}
}

// Assign computes the LSH bucket for the entry's embedding and tier, finds
// candidate entries in the same bucket, and assigns the entry to an existing
// cluster or creates a new one.
//
// If the entry has no embedding, Assign is a no-op and returns ("", nil).
// The returned string is the assigned cluster ID (empty when skipped).
//
// The caller is responsible for persisting the LSHBucket on the entry before
// calling Assign — Assign reads tp.LSHBucket and tp.Tier but does not modify
// the entry in the destination (it calls UpdateCluster instead).
func (a *Assigner) Assign(tp destination.TranslatedPayload) (string, error) {
	if len(tp.Embedding) == 0 {
		return "", nil
	}

	// Fetch candidates from the same (tier, lsh_bucket).
	candidates, err := a.dest.QueryBucketCandidates(tp.Tier, tp.LSHBucket, BucketCandidateLimit)
	if err != nil {
		return "", fmt.Errorf("cluster: query bucket candidates: %w", err)
	}

	// Find the best matching existing cluster among candidates.
	bestClusterID, bestScore := a.findBestCluster(tp.Embedding, candidates)

	if bestClusterID != "" && bestScore >= SimilarityThreshold {
		// Join the existing cluster.
		if err := a.dest.UpdateCluster(tp.PayloadID, bestClusterID, "member"); err != nil {
			return "", fmt.Errorf("cluster: update cluster membership: %w", err)
		}

		// Enforce cluster size cap.
		if err := a.enforceClusterCap(bestClusterID); err != nil {
			return "", fmt.Errorf("cluster: enforce cap: %w", err)
		}

		a.logger.Debug("cluster: assigned to existing cluster",
			"component", "cluster",
			"payload_id", tp.PayloadID,
			"cluster_id", bestClusterID,
			"score", bestScore,
		)
		return bestClusterID, nil
	}

	// Create a new cluster.
	clusterID := generateClusterID()
	if err := a.dest.UpdateCluster(tp.PayloadID, clusterID, "primary"); err != nil {
		return "", fmt.Errorf("cluster: create new cluster: %w", err)
	}

	a.logger.Debug("cluster: created new cluster",
		"component", "cluster",
		"payload_id", tp.PayloadID,
		"cluster_id", clusterID,
	)
	return clusterID, nil
}

// ComputeBucket computes the LSH bucket ID for a given embedding and tier.
// Returns the bucket ID or an error if the hyperplane vectors cannot be generated.
func (a *Assigner) ComputeBucket(embedding []float32, tier int) (int, error) {
	dim := len(embedding)
	hvecs, err := a.seeds.HyperplaneVectors(tier, dim)
	if err != nil {
		return 0, fmt.Errorf("cluster: hyperplane vectors: %w", err)
	}
	bucket, err := lsh.BucketID(embedding, hvecs)
	if err != nil {
		return 0, fmt.Errorf("cluster: bucket id: %w", err)
	}
	return int(bucket), nil
}

// findBestCluster finds the cluster with the highest average cosine similarity
// to the query embedding among candidates. Returns the best cluster ID and its
// similarity score. Returns ("", 0) when no candidates exist.
func (a *Assigner) findBestCluster(queryVec []float32, candidates []destination.TranslatedPayload) (string, float32) {
	// Group candidates by cluster_id, skipping entries without a cluster.
	type clusterAgg struct {
		totalSim float32
		count    int
	}
	clusters := make(map[string]*clusterAgg)

	for _, c := range candidates {
		if c.ClusterID == "" || c.ClusterRole == "superseded" {
			continue
		}
		if len(c.Embedding) == 0 {
			continue
		}

		sim := cosineSimilarity(queryVec, c.Embedding)
		agg, ok := clusters[c.ClusterID]
		if !ok {
			agg = &clusterAgg{}
			clusters[c.ClusterID] = agg
		}
		agg.totalSim += sim
		agg.count++
	}

	var bestID string
	var bestScore float32
	for id, agg := range clusters {
		avg := agg.totalSim / float32(agg.count)
		if avg > bestScore {
			bestScore = avg
			bestID = id
		}
	}
	return bestID, bestScore
}

// enforceClusterCap ensures a cluster does not exceed MaxClusterSize active
// (non-superseded) members. If it does, the oldest member by timestamp is
// marked as superseded. This is deterministic: given the same set of members,
// the same member is always superseded.
func (a *Assigner) enforceClusterCap(clusterID string) error {
	members, err := a.dest.QueryClusterMembers(destination.ClusterQueryParams{
		ClusterID: clusterID,
	})
	if err != nil {
		return fmt.Errorf("query members for cap: %w", err)
	}

	// Filter to active members only.
	active := make([]destination.TranslatedPayload, 0, len(members))
	for _, m := range members {
		if m.ClusterRole != "superseded" {
			active = append(active, m)
		}
	}

	if len(active) <= MaxClusterSize {
		return nil
	}

	// Sort by timestamp ascending (oldest first) for deterministic eviction.
	sort.Slice(active, func(i, j int) bool {
		return active[i].Timestamp.Before(active[j].Timestamp)
	})

	// Supersede the oldest members until we're at cap.
	excess := len(active) - MaxClusterSize
	for i := 0; i < excess; i++ {
		if err := a.dest.UpdateCluster(active[i].PayloadID, clusterID, "superseded"); err != nil {
			return fmt.Errorf("supersede oldest member: %w", err)
		}
		a.logger.Debug("cluster: superseded oldest member",
			"component", "cluster",
			"payload_id", active[i].PayloadID,
			"cluster_id", clusterID,
		)
	}
	return nil
}

// generateClusterID produces a random 16-character hex cluster ID.
func generateClusterID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("cluster: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// cosineSimilarity computes cosine similarity between two float32 vectors.
// Returns 0 for zero-length or zero-norm vectors.
func cosineSimilarity(a, b []float32) float32 {
	n := len(a)
	if n == 0 || len(b) < n {
		return 0
	}
	var dot, normA, normB float32
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
