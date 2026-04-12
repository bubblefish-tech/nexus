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

package query_test

import (
	"context"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/query"
)

// ---------------------------------------------------------------------------
// mockClusterQuerier — test double for destination.ClusterQuerier
// ---------------------------------------------------------------------------

type mockClusterQuerier struct {
	members map[string][]destination.TranslatedPayload
}

func newMockClusterQuerier() *mockClusterQuerier {
	return &mockClusterQuerier{
		members: make(map[string][]destination.TranslatedPayload),
	}
}

func (m *mockClusterQuerier) QueryClusterMembers(params destination.ClusterQueryParams) ([]destination.TranslatedPayload, error) {
	return m.members[params.ClusterID], nil
}

func (m *mockClusterQuerier) QueryBucketCandidates(tier, bucket, candidateLimit int) ([]destination.TranslatedPayload, error) {
	return nil, nil
}

func (m *mockClusterQuerier) UpdateCluster(payloadID, clusterID, clusterRole string) error {
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestClusterAware_ExpandsClusterMembers(t *testing.T) {
	// Set up a structured query that returns one record with a cluster_id.
	mq := &mockQuerier{
		result: destination.QueryResult{
			Records: []destination.TranslatedPayload{
				{
					PayloadID:   "r-1",
					Content:     "primary content",
					ClusterID:   "cluster-abc",
					ClusterRole: "primary",
					Tier:        1,
					Timestamp:   time.Now(),
				},
			},
		},
	}

	// Set up cluster querier that returns additional members.
	cq := newMockClusterQuerier()
	cq.members["cluster-abc"] = []destination.TranslatedPayload{
		{
			PayloadID:   "r-1",
			Content:     "primary content",
			ClusterID:   "cluster-abc",
			ClusterRole: "primary",
			Tier:        1,
			Timestamp:   time.Now().Add(-time.Minute),
		},
		{
			PayloadID:   "r-2",
			Content:     "member content",
			ClusterID:   "cluster-abc",
			ClusterRole: "member",
			Tier:        1,
			Timestamp:   time.Now(),
		},
	}

	src := &config.Source{
		Name:    "test-src",
		CanRead: true,
		Tier:    3,
	}

	runner := query.New(mq, nil).WithClusterQuerier(cq)

	q := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "testns",
		Profile:     query.ProfileClusterAware,
		Q:           "content",
		Limit:       20,
	}

	result, err := runner.Run(context.Background(), src, q)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !result.ClusterExpanded {
		t.Error("expected ClusterExpanded = true")
	}
	if result.ClusterCount != 1 {
		t.Errorf("expected ClusterCount = 1, got %d", result.ClusterCount)
	}
	// Should have both records (primary + member).
	if len(result.Records) != 2 {
		t.Errorf("expected 2 records after expansion, got %d", len(result.Records))
	}
	// No conflict — same-ish content but checking hash.
	// Both have different content so there IS a conflict.
	if !result.Conflict {
		t.Error("expected Conflict = true (different content hashes)")
	}
}

func TestClusterAware_ConflictDetection(t *testing.T) {
	mq := &mockQuerier{
		result: destination.QueryResult{
			Records: []destination.TranslatedPayload{
				{
					PayloadID:   "c-1",
					Content:     "version A",
					ClusterID:   "cluster-conflict",
					ClusterRole: "primary",
					Tier:        1,
					Timestamp:   time.Now(),
				},
			},
		},
	}

	cq := newMockClusterQuerier()
	cq.members["cluster-conflict"] = []destination.TranslatedPayload{
		{
			PayloadID:   "c-1",
			Content:     "version A",
			ClusterID:   "cluster-conflict",
			ClusterRole: "primary",
			Tier:        1,
		},
		{
			PayloadID:   "c-2",
			Content:     "version B", // Different content = conflict
			ClusterID:   "cluster-conflict",
			ClusterRole: "member",
			Tier:        1,
		},
	}

	src := &config.Source{
		Name:    "test-src",
		CanRead: true,
		Tier:    3,
	}

	runner := query.New(mq, nil).WithClusterQuerier(cq)
	q := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "testns",
		Profile:     query.ProfileClusterAware,
		Q:           "version",
		Limit:       20,
	}

	result, err := runner.Run(context.Background(), src, q)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !result.Conflict {
		t.Error("expected Conflict = true for members with different content")
	}
	if len(result.Records) != 2 {
		t.Errorf("expected 2 records, got %d", len(result.Records))
	}
}

func TestClusterAware_NoConflictSameContent(t *testing.T) {
	mq := &mockQuerier{
		result: destination.QueryResult{
			Records: []destination.TranslatedPayload{
				{
					PayloadID:   "s-1",
					Content:     "same content",
					ClusterID:   "cluster-same",
					ClusterRole: "primary",
					Tier:        1,
					Timestamp:   time.Now(),
				},
			},
		},
	}

	cq := newMockClusterQuerier()
	cq.members["cluster-same"] = []destination.TranslatedPayload{
		{
			PayloadID:   "s-1",
			Content:     "same content",
			ClusterID:   "cluster-same",
			ClusterRole: "primary",
			Tier:        1,
		},
		{
			PayloadID:   "s-2",
			Content:     "same content", // Same content = no conflict
			ClusterID:   "cluster-same",
			ClusterRole: "member",
			Tier:        1,
		},
	}

	src := &config.Source{
		Name:    "test-src",
		CanRead: true,
		Tier:    3,
	}

	runner := query.New(mq, nil).WithClusterQuerier(cq)
	q := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "testns",
		Profile:     query.ProfileClusterAware,
		Q:           "same",
		Limit:       20,
	}

	result, err := runner.Run(context.Background(), src, q)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Conflict {
		t.Error("expected Conflict = false for identical content")
	}
	if !result.ClusterExpanded {
		t.Error("expected ClusterExpanded = true")
	}
}

func TestBalancedProfile_NoClusterExpansion(t *testing.T) {
	mq := &mockQuerier{
		result: destination.QueryResult{
			Records: []destination.TranslatedPayload{
				{
					PayloadID:   "b-1",
					Content:     "some content",
					ClusterID:   "cluster-xxx",
					ClusterRole: "primary",
					Tier:        1,
					Timestamp:   time.Now(),
				},
			},
		},
	}

	cq := newMockClusterQuerier()
	cq.members["cluster-xxx"] = []destination.TranslatedPayload{
		{PayloadID: "b-1", Content: "some content", ClusterID: "cluster-xxx", ClusterRole: "primary", Tier: 1},
		{PayloadID: "b-2", Content: "other content", ClusterID: "cluster-xxx", ClusterRole: "member", Tier: 1},
	}

	src := &config.Source{
		Name:    "test-src",
		CanRead: true,
		Tier:    3,
	}

	runner := query.New(mq, nil).WithClusterQuerier(cq)
	q := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "testns",
		Profile:     query.ProfileBalanced, // NOT cluster-aware
		Q:           "content",
		Limit:       20,
	}

	result, err := runner.Run(context.Background(), src, q)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.ClusterExpanded {
		t.Error("balanced profile should NOT expand clusters")
	}
	if len(result.Records) != 1 {
		t.Errorf("expected 1 record (no expansion), got %d", len(result.Records))
	}
}

func TestClusterAware_SupersededExcludedFromConflict(t *testing.T) {
	mq := &mockQuerier{
		result: destination.QueryResult{
			Records: []destination.TranslatedPayload{
				{
					PayloadID:   "sup-1",
					Content:     "current",
					ClusterID:   "cluster-sup",
					ClusterRole: "primary",
					Tier:        1,
					Timestamp:   time.Now(),
				},
			},
		},
	}

	cq := newMockClusterQuerier()
	cq.members["cluster-sup"] = []destination.TranslatedPayload{
		{PayloadID: "sup-1", Content: "current", ClusterID: "cluster-sup", ClusterRole: "primary", Tier: 1},
		{PayloadID: "sup-2", Content: "old different", ClusterID: "cluster-sup", ClusterRole: "superseded", Tier: 1},
	}

	src := &config.Source{
		Name:    "test-src",
		CanRead: true,
		Tier:    3,
	}

	runner := query.New(mq, nil).WithClusterQuerier(cq)
	q := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "testns",
		Profile:     query.ProfileClusterAware,
		Q:           "content",
		Limit:       20,
	}

	result, err := runner.Run(context.Background(), src, q)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Superseded members are excluded from conflict detection, so only one
	// active content hash exists. No conflict.
	if result.Conflict {
		t.Error("expected Conflict = false (superseded member should not count)")
	}
}

func TestClusterAware_ValidProfile(t *testing.T) {
	if !query.ValidProfile(query.ProfileClusterAware) {
		t.Error("cluster-aware should be a valid profile")
	}
}
