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

package cluster_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/cluster"
)

// ── ErrFeatureLocked tests ──────────────────────────────────────────────────

func TestErrFeatureLocked_Message(t *testing.T) {
	msg := cluster.ErrFeatureLocked.Error()
	if !strings.Contains(msg, "Nexus Enterprise") {
		t.Errorf("error should mention Nexus Enterprise, got: %s", msg)
	}
	if !strings.Contains(msg, "bubblefish.sh/enterprise") {
		t.Errorf("error should contain bubblefish.sh/enterprise, got: %s", msg)
	}
}

// ── NodeState tests ─────────────────────────────────────────────────────────

func TestNodeState_String(t *testing.T) {
	tests := []struct {
		state cluster.NodeState
		want  string
	}{
		{cluster.NodeStateUnknown, "unknown"},
		{cluster.NodeStateFollower, "follower"},
		{cluster.NodeStateCandidate, "candidate"},
		{cluster.NodeStateLeader, "leader"},
		{cluster.NodeState(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("NodeState(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}

// ── StandaloneCluster tests ─────────────────────────────────────────────────

func TestStandaloneCluster_JoinReturnsFeatureLocked(t *testing.T) {
	c := cluster.NewStandaloneCluster("node-1")
	err := c.Join(context.Background(), "10.0.0.1:7946")
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("Join: expected ErrFeatureLocked, got %v", err)
	}
}

func TestStandaloneCluster_LeaveReturnsFeatureLocked(t *testing.T) {
	c := cluster.NewStandaloneCluster("node-1")
	err := c.Leave(context.Background())
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("Leave: expected ErrFeatureLocked, got %v", err)
	}
}

func TestStandaloneCluster_MembersReturnsFeatureLocked(t *testing.T) {
	c := cluster.NewStandaloneCluster("node-1")
	_, err := c.Members(context.Background())
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("Members: expected ErrFeatureLocked, got %v", err)
	}
}

func TestStandaloneCluster_LocalNode(t *testing.T) {
	c := cluster.NewStandaloneCluster("node-1")
	info := c.LocalNode()
	if info.ID != "node-1" {
		t.Errorf("expected node-1, got %s", info.ID)
	}
	if info.State != cluster.NodeStateLeader {
		t.Errorf("standalone should be leader, got %s", info.State)
	}
}

func TestStandaloneCluster_IsLeader(t *testing.T) {
	c := cluster.NewStandaloneCluster("node-1")
	if !c.IsLeader() {
		t.Error("standalone should always report as leader")
	}
}

func TestStandaloneCluster_CloseNoError(t *testing.T) {
	c := cluster.NewStandaloneCluster("node-1")
	if err := c.Close(); err != nil {
		t.Errorf("Close: expected nil, got %v", err)
	}
}

// ── StandaloneElection tests ────────────────────────────────────────────────

func TestStandaloneElection_CampaignReturnsFeatureLocked(t *testing.T) {
	e := cluster.NewStandaloneElection()
	err := e.Campaign(context.Background())
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("Campaign: expected ErrFeatureLocked, got %v", err)
	}
}

func TestStandaloneElection_ResignReturnsFeatureLocked(t *testing.T) {
	e := cluster.NewStandaloneElection()
	err := e.Resign(context.Background())
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("Resign: expected ErrFeatureLocked, got %v", err)
	}
}

func TestStandaloneElection_LeaderReturnsFeatureLocked(t *testing.T) {
	e := cluster.NewStandaloneElection()
	_, err := e.Leader(context.Background())
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("Leader: expected ErrFeatureLocked, got %v", err)
	}
}

func TestStandaloneElection_WatchReturnsFeatureLocked(t *testing.T) {
	e := cluster.NewStandaloneElection()
	_, err := e.Watch(context.Background())
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("Watch: expected ErrFeatureLocked, got %v", err)
	}
}

// ── StandaloneReplicator tests ──────────────────────────────────────────────

func TestStandaloneReplicator_StartReturnsFeatureLocked(t *testing.T) {
	r := cluster.NewStandaloneReplicator()
	err := r.StartReplication(context.Background(), "10.0.0.1:7947")
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("StartReplication: expected ErrFeatureLocked, got %v", err)
	}
}

func TestStandaloneReplicator_StopReturnsFeatureLocked(t *testing.T) {
	r := cluster.NewStandaloneReplicator()
	err := r.StopReplication(context.Background())
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("StopReplication: expected ErrFeatureLocked, got %v", err)
	}
}

func TestStandaloneReplicator_StatusReturnsFeatureLocked(t *testing.T) {
	r := cluster.NewStandaloneReplicator()
	_, err := r.Status(context.Background())
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("Status: expected ErrFeatureLocked, got %v", err)
	}
}

func TestStandaloneReplicator_SnapshotReturnsFeatureLocked(t *testing.T) {
	r := cluster.NewStandaloneReplicator()
	err := r.Snapshot(context.Background())
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("Snapshot: expected ErrFeatureLocked, got %v", err)
	}
}

// ── StandaloneConsensus tests ───────────────────────────────────────────────

func TestStandaloneConsensus_ProposeReturnsFeatureLocked(t *testing.T) {
	c := cluster.NewStandaloneConsensus()
	err := c.Propose(context.Background(), []byte("data"))
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("Propose: expected ErrFeatureLocked, got %v", err)
	}
}

func TestStandaloneConsensus_CommitReturnsFeatureLocked(t *testing.T) {
	c := cluster.NewStandaloneConsensus()
	err := c.Commit(context.Background(), 1)
	if !errors.Is(err, cluster.ErrFeatureLocked) {
		t.Errorf("Commit: expected ErrFeatureLocked, got %v", err)
	}
}

func TestStandaloneConsensus_Applied(t *testing.T) {
	c := cluster.NewStandaloneConsensus()
	if got := c.Applied(); got != 0 {
		t.Errorf("Applied: expected 0, got %d", got)
	}
}

func TestStandaloneConsensus_IsHealthy(t *testing.T) {
	c := cluster.NewStandaloneConsensus()
	if !c.IsHealthy() {
		t.Error("standalone consensus should report healthy")
	}
}

// ── Interface compliance tests ──────────────────────────────────────────────

func TestInterfaceCompliance(t *testing.T) {
	// These compile-time checks are in standalone.go via var _ = ... lines.
	// This test exercises them at runtime to ensure no nil pointer issues.
	var hc cluster.HACluster = cluster.NewStandaloneCluster("test")
	if hc == nil {
		t.Fatal("StandaloneCluster should implement HACluster")
	}

	var le cluster.LeaderElection = cluster.NewStandaloneElection()
	if le == nil {
		t.Fatal("StandaloneElection should implement LeaderElection")
	}

	var rep cluster.Replicator = cluster.NewStandaloneReplicator()
	if rep == nil {
		t.Fatal("StandaloneReplicator should implement Replicator")
	}

	var cp cluster.ConsensusProtocol = cluster.NewStandaloneConsensus()
	if cp == nil {
		t.Fatal("StandaloneConsensus should implement ConsensusProtocol")
	}
}
