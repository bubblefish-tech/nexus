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
	"time"
)

// Compile-time interface checks.
var (
	_ HACluster         = (*StandaloneCluster)(nil)
	_ LeaderElection    = (*StandaloneElection)(nil)
	_ Replicator        = (*StandaloneReplicator)(nil)
	_ ConsensusProtocol = (*StandaloneConsensus)(nil)
)

// ── StandaloneCluster ───────────────────────────────────────────────────────

// StandaloneCluster is the Community edition no-op implementation of HACluster.
// Every method returns ErrFeatureLocked.
type StandaloneCluster struct {
	nodeID NodeID
}

// NewStandaloneCluster creates a StandaloneCluster with the given node ID.
func NewStandaloneCluster(nodeID NodeID) *StandaloneCluster {
	return &StandaloneCluster{nodeID: nodeID}
}

// Join returns ErrFeatureLocked.
func (s *StandaloneCluster) Join(_ context.Context, _ string) error {
	return ErrFeatureLocked
}

// Leave returns ErrFeatureLocked.
func (s *StandaloneCluster) Leave(_ context.Context) error {
	return ErrFeatureLocked
}

// Members returns ErrFeatureLocked.
func (s *StandaloneCluster) Members(_ context.Context) ([]NodeInfo, error) {
	return nil, ErrFeatureLocked
}

// LocalNode returns information about this standalone node.
func (s *StandaloneCluster) LocalNode() NodeInfo {
	return NodeInfo{
		ID:       s.nodeID,
		State:    NodeStateLeader, // standalone is always "leader"
		LastSeen: time.Now(),
	}
}

// IsLeader always returns true for standalone (single-node is always leader).
func (s *StandaloneCluster) IsLeader() bool {
	return true
}

// Close is a no-op for standalone.
func (s *StandaloneCluster) Close() error {
	return nil
}

// ── StandaloneElection ──────────────────────────────────────────────────────

// StandaloneElection is the Community edition no-op implementation of
// LeaderElection. Every method returns ErrFeatureLocked.
type StandaloneElection struct{}

// NewStandaloneElection creates a StandaloneElection.
func NewStandaloneElection() *StandaloneElection {
	return &StandaloneElection{}
}

// Campaign returns ErrFeatureLocked.
func (s *StandaloneElection) Campaign(_ context.Context) error {
	return ErrFeatureLocked
}

// Resign returns ErrFeatureLocked.
func (s *StandaloneElection) Resign(_ context.Context) error {
	return ErrFeatureLocked
}

// Leader returns ErrFeatureLocked.
func (s *StandaloneElection) Leader(_ context.Context) (NodeID, error) {
	return "", ErrFeatureLocked
}

// Watch returns ErrFeatureLocked.
func (s *StandaloneElection) Watch(_ context.Context) (<-chan NodeID, error) {
	return nil, ErrFeatureLocked
}

// ── StandaloneReplicator ────────────────────────────────────────────────────

// StandaloneReplicator is the Community edition no-op implementation of
// Replicator. Every method returns ErrFeatureLocked.
type StandaloneReplicator struct{}

// NewStandaloneReplicator creates a StandaloneReplicator.
func NewStandaloneReplicator() *StandaloneReplicator {
	return &StandaloneReplicator{}
}

// StartReplication returns ErrFeatureLocked.
func (s *StandaloneReplicator) StartReplication(_ context.Context, _ string) error {
	return ErrFeatureLocked
}

// StopReplication returns ErrFeatureLocked.
func (s *StandaloneReplicator) StopReplication(_ context.Context) error {
	return ErrFeatureLocked
}

// Status returns ErrFeatureLocked.
func (s *StandaloneReplicator) Status(_ context.Context) (ReplicationStatus, error) {
	return ReplicationStatus{}, ErrFeatureLocked
}

// Snapshot returns ErrFeatureLocked.
func (s *StandaloneReplicator) Snapshot(_ context.Context) error {
	return ErrFeatureLocked
}

// ── StandaloneConsensus ─────────────────────────────────────────────────────

// StandaloneConsensus is the Community edition no-op implementation of
// ConsensusProtocol. Propose and Commit return ErrFeatureLocked.
type StandaloneConsensus struct{}

// NewStandaloneConsensus creates a StandaloneConsensus.
func NewStandaloneConsensus() *StandaloneConsensus {
	return &StandaloneConsensus{}
}

// Propose returns ErrFeatureLocked.
func (s *StandaloneConsensus) Propose(_ context.Context, _ []byte) error {
	return ErrFeatureLocked
}

// Commit returns ErrFeatureLocked.
func (s *StandaloneConsensus) Commit(_ context.Context, _ uint64) error {
	return ErrFeatureLocked
}

// Applied returns 0 for standalone.
func (s *StandaloneConsensus) Applied() uint64 {
	return 0
}

// IsHealthy returns true for standalone (single-node is always healthy).
func (s *StandaloneConsensus) IsHealthy() bool {
	return true
}
