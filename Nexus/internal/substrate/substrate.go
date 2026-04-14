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

// Package substrate implements the BF-Sketch substrate for BubbleFish Nexus.
// BF-Sketch is a unified composition of binary quantization (RaBitQ),
// per-memory AES-256-GCM encryption with HKDF-derived keys, a cuckoo filter
// deletion oracle, and a forward-secure HMAC-SHA-256 hash ratchet.
//
// The substrate is controlled by the [substrate] section in daemon.toml and
// defaults to disabled in v0.1.3. When disabled, all methods are safe no-ops.
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan.
package substrate

import (
	"crypto/ed25519"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"

	"github.com/BubbleFish-Nexus/internal/canonical"
	"github.com/BubbleFish-Nexus/internal/provenance"
	"github.com/BubbleFish-Nexus/internal/secrets"
)

// Substrate is the top-level coordinator for the BF-Sketch substrate.
// A nil Substrate is safe to use; all methods return ErrDisabled or
// appropriate zero values.
type Substrate struct {
	cfg        Config
	db         *sql.DB
	canonical  *canonical.Manager
	ratchet    *RatchetManager
	cuckoo     *CuckooOracle
	auditLog   *SubstrateAuditLog
	signingKey ed25519.PrivateKey
	logger     *slog.Logger
}

// SubstrateStatus holds the operational state for CLI/API display.
type SubstrateStatus struct {
	Enabled          bool    `json:"enabled"`
	RatchetStateID   uint32  `json:"ratchet_state_id,omitempty"`
	SketchCount      int64   `json:"sketch_count,omitempty"`
	CuckooCount      uint    `json:"cuckoo_count,omitempty"`
	CuckooCapacity   uint    `json:"cuckoo_capacity,omitempty"`
	CuckooInserts    uint64  `json:"cuckoo_inserts,omitempty"`
	CuckooDeletes    uint64  `json:"cuckoo_deletes,omitempty"`
	ShreddedStates   int64   `json:"shredded_states,omitempty"`
	CanonicalDim     int     `json:"canonical_dim,omitempty"`
}

// New creates a Substrate coordinator. When disabled (cfg.Enabled == false),
// returns a disabled stub (nil-safe methods). When enabled, initializes all
// sub-components in order: canonical → ratchet → cuckoo → audit.
//
// Fail-closed: any initialization error returns (nil, error).
func New(
	cfg Config,
	db *sql.DB,
	sd *secrets.Dir,
	daemonKP *provenance.KeyPair,
	canonicalMgr *canonical.Manager,
	chainState *provenance.ChainState,
	logger *slog.Logger,
) (*Substrate, error) {
	if !cfg.Enabled {
		return &Substrate{cfg: cfg, logger: logger}, nil
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("substrate config: %w", err)
	}
	if canonicalMgr == nil || !canonicalMgr.Enabled() {
		return nil, fmt.Errorf("substrate requires canonical to be enabled")
	}
	if db == nil {
		return nil, fmt.Errorf("substrate requires a database connection")
	}

	var signingKey ed25519.PrivateKey
	if daemonKP != nil {
		signingKey = daemonKP.PrivateKey
	}

	// Initialize ratchet manager
	ratchet, err := NewRatchetManager(
		db, sd, signingKey,
		uint32(cfg.CanonicalDim(canonicalMgr)),
		uint32(cfg.SketchBits),
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("substrate: init ratchet: %w", err)
	}

	// Initialize cuckoo oracle: load persisted, or rebuild from DB
	cuckooOracle, err := LoadCuckooOracle(db, cfg.CuckooCapacity)
	if err != nil {
		logger.Warn("substrate: cuckoo load failed, rebuilding from DB",
			"component", "substrate", "error", err)
		cuckooOracle, err = RebuildFromDB(db, cfg.CuckooCapacity, logger)
		if err != nil {
			return nil, fmt.Errorf("substrate: init cuckoo: %w", err)
		}
	}

	auditLog := NewSubstrateAuditLog(chainState)

	s := &Substrate{
		cfg:        cfg,
		db:         db,
		canonical:  canonicalMgr,
		ratchet:    ratchet,
		cuckoo:     cuckooOracle,
		auditLog:   auditLog,
		signingKey: signingKey,
		logger:     logger,
	}

	logger.Info("substrate initialized",
		"component", "substrate",
		"ratchet_state_id", ratchet.Current().StateID,
		"cuckoo_count", cuckooOracle.Count(),
	)

	return s, nil
}

// CanonicalDim returns the canonical dimension from the canonical manager,
// or 1024 as default.
func (c Config) CanonicalDim(mgr *canonical.Manager) int {
	if mgr != nil && mgr.Enabled() {
		// The canonical manager's config holds the dim.
		// For now, return the default since we can't access the config.
		// TODO(shawn): expose CanonicalDim() on canonical.Manager.
	}
	return 1024
}

// Enabled reports whether the substrate is active.
func (s *Substrate) Enabled() bool {
	if s == nil {
		return false
	}
	return s.cfg.Enabled
}

// CurrentRatchetState returns the active ratchet state. Nil-safe.
func (s *Substrate) CurrentRatchetState() *RatchetState {
	if s == nil || s.ratchet == nil {
		return nil
	}
	return s.ratchet.Current()
}

// RotateRatchet manually advances the ratchet. Nil-safe.
func (s *Substrate) RotateRatchet(reason string) (*RatchetState, error) {
	if s == nil || s.ratchet == nil {
		return nil, ErrDisabled
	}
	state, err := s.ratchet.Advance(reason)
	if err != nil {
		return nil, err
	}
	// Emit audit event
	if s.auditLog != nil {
		s.auditLog.EmitRatchetAdvanced(state.StateID-1, state.StateID, reason)
	}
	return state, nil
}

// CuckooLookup checks if a memory ID is in the cuckoo filter. Nil-safe.
func (s *Substrate) CuckooLookup(memoryID string) bool {
	if s == nil || s.cuckoo == nil {
		return false
	}
	return s.cuckoo.Lookup(memoryID)
}

// ComputeAndStoreSketch is the write-path hook. It canonicalizes the embedding,
// computes the sketch, encrypts the embedding, and stores everything.
func (s *Substrate) ComputeAndStoreSketch(memoryID string, rawEmbedding []float64, source string) error {
	if s == nil || !s.cfg.Enabled {
		return ErrDisabled
	}

	// 1. Canonicalize
	canonicalVec, _, err := s.canonical.Canonicalize(rawEmbedding, source)
	if err != nil {
		return fmt.Errorf("substrate: canonicalize: %w", err)
	}

	// 2. Get current ratchet state
	state := s.ratchet.Current()
	if state == nil {
		return fmt.Errorf("substrate: no ratchet state")
	}

	// 3. Compute sketch
	sketch, err := ComputeStoreSketch(canonicalVec, state.StateBytes, state.StateID)
	if err != nil {
		return fmt.Errorf("substrate: compute sketch: %w", err)
	}
	sketchBytes, err := sketch.Marshal()
	if err != nil {
		return fmt.Errorf("substrate: marshal sketch: %w", err)
	}

	// 4. Derive per-memory key and encrypt
	key, err := DeriveEmbeddingKey(state.StateBytes, memoryID)
	if err != nil {
		return fmt.Errorf("substrate: derive key: %w", err)
	}

	// Encode embedding as float64 LE bytes for encryption
	embBytes := encodeFloat64Slice(canonicalVec)

	encrypted, err := EncryptEmbedding(key, embBytes)
	ZeroizeKey(&key)
	if err != nil {
		return fmt.Errorf("substrate: encrypt: %w", err)
	}

	// 5. Store in DB (transaction)
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("substrate: begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE memories SET sketch = ?, embedding_ciphertext = ?, embedding_nonce = ?
		WHERE payload_id = ?
	`, sketchBytes, encrypted.Ciphertext, encrypted.Nonce, memoryID)
	if err != nil {
		return fmt.Errorf("substrate: update memories: %w", err)
	}

	_, err = tx.Exec(`
		INSERT OR REPLACE INTO substrate_memory_state (memory_id, state_id)
		VALUES (?, ?)
	`, memoryID, state.StateID)
	if err != nil {
		return fmt.Errorf("substrate: insert memory state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("substrate: commit: %w", err)
	}

	// Chaos kill point: DB committed but cuckoo not yet updated.
	ChaosKillPoint("sketch_write")

	// 6. Cuckoo insert
	if err := s.cuckoo.Insert(memoryID); err != nil {
		if err == ErrCuckooNeedsRebuild {
			s.logger.Warn("substrate: cuckoo full, rebuilding",
				"component", "substrate", "memory_id", memoryID)
			rebuilt, rebuildErr := RebuildFromDB(s.db, s.cfg.CuckooCapacity*2, s.logger)
			if rebuildErr != nil {
				return fmt.Errorf("substrate: cuckoo rebuild: %w", rebuildErr)
			}
			s.cuckoo = rebuilt
			s.cuckoo.Insert(memoryID)
		} else {
			return fmt.Errorf("substrate: cuckoo insert: %w", err)
		}
	}

	// 7. Audit event
	s.auditLog.EmitSketchWritten(memoryID, state.StateID, sketchBytes, uint32(len(canonicalVec)))

	return nil
}

// LoadStoreSketch loads a stored sketch from the DB for a given memory ID.
func (s *Substrate) LoadStoreSketch(memoryID string) (*StoreSketch, error) {
	if s == nil || !s.cfg.Enabled {
		return nil, ErrDisabled
	}
	var sketchBytes []byte
	err := s.db.QueryRow(
		`SELECT sketch FROM memories WHERE payload_id = ?`, memoryID,
	).Scan(&sketchBytes)
	if err != nil {
		return nil, fmt.Errorf("substrate: load sketch: %w", err)
	}
	if sketchBytes == nil {
		return nil, ErrCorruptSketch
	}
	return UnmarshalStoreSketch(sketchBytes)
}

// ShredMemory performs forward-secure deletion: clears ciphertext, removes
// from cuckoo, and emits an audit event. Does NOT advance the ratchet —
// call RotateRatchet separately if needed.
func (s *Substrate) ShredMemory(memoryID string) error {
	if s == nil || !s.cfg.Enabled {
		return ErrDisabled
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("substrate: shred begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE memories SET embedding_ciphertext = NULL, embedding_nonce = NULL
		WHERE payload_id = ?
	`, memoryID)
	if err != nil {
		return fmt.Errorf("substrate: shred clear ciphertext: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM substrate_memory_state WHERE memory_id = ?`, memoryID)
	if err != nil {
		return fmt.Errorf("substrate: shred delete memory state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("substrate: shred commit: %w", err)
	}

	// Chaos kill point: ciphertext cleared in DB but cuckoo still has the entry.
	ChaosKillPoint("shred_after_clear")

	s.cuckoo.Delete(memoryID)

	// Persist cuckoo
	if err := s.cuckoo.Persist(s.db); err != nil {
		s.logger.Warn("substrate: cuckoo persist after shred failed",
			"component", "substrate", "error", err)
	}

	// Audit
	currentState := s.ratchet.Current()
	s.auditLog.EmitMemoryShredded(memoryID, 0, currentState.StateID, true, false)

	return nil
}

// ProveDeletion produces a signed deletion proof for a memory.
func (s *Substrate) ProveDeletion(memoryID string) (*DeletionProof, error) {
	if s == nil || !s.cfg.Enabled {
		return nil, ErrDisabled
	}
	return ProveDeletion(s.db, s.cuckoo, s.ratchet, s.auditLog, s.signingKey, memoryID)
}

// Status returns the current operational state for CLI/API display.
func (s *Substrate) Status() SubstrateStatus {
	if s == nil || !s.cfg.Enabled {
		return SubstrateStatus{Enabled: false}
	}
	status := SubstrateStatus{
		Enabled: true,
	}
	if state := s.ratchet.Current(); state != nil {
		status.RatchetStateID = state.StateID
		status.CanonicalDim = int(state.CanonicalDim)
	}
	if s.cuckoo != nil {
		stats := s.cuckoo.Stats()
		status.CuckooCount = stats.Count
		status.CuckooCapacity = stats.Capacity
		status.CuckooInserts = stats.InsertCount
		status.CuckooDeletes = stats.DeleteCount
	}

	// Count sketches
	var sketchCount int64
	s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE sketch IS NOT NULL`).Scan(&sketchCount)
	status.SketchCount = sketchCount

	// Count shredded states
	var shreddedCount int64
	s.db.QueryRow(`SELECT COUNT(*) FROM substrate_ratchet_states WHERE shredded_at IS NOT NULL`).Scan(&shreddedCount)
	status.ShreddedStates = shreddedCount

	return status
}

// Shutdown releases resources and persists state.
func (s *Substrate) Shutdown() error {
	if s == nil || !s.cfg.Enabled {
		return nil
	}
	var firstErr error
	if s.cuckoo != nil {
		if err := s.cuckoo.Persist(s.db); err != nil {
			s.logger.Error("substrate: cuckoo persist on shutdown", "error", err)
			firstErr = err
		}
	}
	if s.ratchet != nil {
		if err := s.ratchet.Shutdown(); err != nil {
			s.logger.Error("substrate: ratchet shutdown", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// encodeFloat64Slice encodes a float64 slice as little-endian bytes.
func encodeFloat64Slice(v []float64) []byte {
	buf := make([]byte, len(v)*8)
	for i, f := range v {
		binary.LittleEndian.PutUint64(buf[i*8:(i+1)*8], math.Float64bits(f))
	}
	return buf
}
