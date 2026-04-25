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

package cluster

import (
	"context"
	"errors"
	"time"
)

// ErrFeatureLocked is returned by all HA cluster operations in the Community
// edition. Clustering requires Nexus Enterprise.
var ErrFeatureLocked = errors.New("Clustering requires Nexus Enterprise. See bubblefish.sh/enterprise")

// NodeID uniquely identifies a node in a Nexus cluster.
type NodeID string

// NodeState represents the current state of a cluster node.
type NodeState int

const (
	// NodeStateUnknown is the default/uninitialized state.
	NodeStateUnknown NodeState = iota
	// NodeStateFollower indicates a node that replicates from the leader.
	NodeStateFollower
	// NodeStateCandidate indicates a node participating in leader election.
	NodeStateCandidate
	// NodeStateLeader indicates the active leader handling writes.
	NodeStateLeader
)

// String returns a human-readable node state label.
func (s NodeState) String() string {
	switch s {
	case NodeStateFollower:
		return "follower"
	case NodeStateCandidate:
		return "candidate"
	case NodeStateLeader:
		return "leader"
	default:
		return "unknown"
	}
}

// NodeInfo describes a node in the cluster.
type NodeInfo struct {
	ID      NodeID
	Address string
	State   NodeState
	LastSeen time.Time
}

// ReplicationStatus describes the replication state between leader and follower.
type ReplicationStatus struct {
	LeaderID   NodeID
	FollowerID NodeID
	Lag        time.Duration
	BytesBehind int64
	Healthy    bool
}

// ── Interfaces ──────────────────────────────────────────────────────────────

// HACluster is the top-level interface for multi-node clustering.
// Community edition returns ErrFeatureLocked for all methods.
type HACluster interface {
	// Join adds this node to a cluster at the given address.
	Join(ctx context.Context, clusterAddr string) error

	// Leave gracefully removes this node from the cluster.
	Leave(ctx context.Context) error

	// Members returns the current cluster membership list.
	Members(ctx context.Context) ([]NodeInfo, error)

	// LocalNode returns information about the local node.
	LocalNode() NodeInfo

	// IsLeader reports whether the local node is the current leader.
	IsLeader() bool

	// Close shuts down cluster operations.
	Close() error
}

// LeaderElection manages leader election within the cluster.
// Community edition returns ErrFeatureLocked for all methods.
type LeaderElection interface {
	// Campaign starts a leader election campaign for the local node.
	Campaign(ctx context.Context) error

	// Resign voluntarily gives up leadership.
	Resign(ctx context.Context) error

	// Leader returns the current leader's NodeID.
	Leader(ctx context.Context) (NodeID, error)

	// Watch returns a channel that receives the new leader's NodeID
	// whenever leadership changes. The channel is closed when the
	// context is cancelled.
	Watch(ctx context.Context) (<-chan NodeID, error)
}

// Replicator handles data replication between cluster nodes.
// Community edition returns ErrFeatureLocked for all methods.
type Replicator interface {
	// StartReplication begins replicating from the leader to this node.
	StartReplication(ctx context.Context, leaderAddr string) error

	// StopReplication stops ongoing replication.
	StopReplication(ctx context.Context) error

	// Status returns the current replication status.
	Status(ctx context.Context) (ReplicationStatus, error)

	// Snapshot triggers a point-in-time snapshot for catch-up replication.
	Snapshot(ctx context.Context) error
}

// ConsensusProtocol defines the consensus algorithm interface.
// Community edition returns ErrFeatureLocked for all methods.
type ConsensusProtocol interface {
	// Propose submits a value for consensus.
	Propose(ctx context.Context, data []byte) error

	// Commit confirms that a value has been committed by a quorum.
	Commit(ctx context.Context, index uint64) error

	// Applied returns the last applied consensus index.
	Applied() uint64

	// IsHealthy reports whether the consensus subsystem is operating normally.
	IsHealthy() bool
}
