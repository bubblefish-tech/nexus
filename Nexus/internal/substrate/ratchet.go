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

// Forward-secure HMAC-SHA-256 hash ratchet for substrate.
//
// The ratchet construction is from:
//   Bellare and Yee, "Forward-Security in Private-Key Cryptography" (2003)
//
// Construction: S_{i+1} = HMAC-SHA-256(S_i, advance_label)
//
// Because HMAC-SHA-256 is one-way, given S_{i+1} you cannot recover S_i.
// This is the anchor of the substrate's cryptographic erasure claim.
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.4.
package substrate

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BubbleFish-Nexus/internal/secrets"
)

// Domain separation constants. Part of the on-disk format.
const (
	ratchetAdvanceLabelV1 = "bubblefish-ratchet-advance-v1"
	ratchetInitLabelV1    = "bubblefish-ratchet-init-v1"
	ratchetSecretName     = "ratchet.state"
)

// RatchetState is a single forward-secure state.
type RatchetState struct {
	StateID      uint32
	StateBytes   [32]byte
	CreatedAt    time.Time
	ShreddedAt   *time.Time // nil if active
	CanonicalDim uint32
	SketchBits   uint32
	Signature    []byte // Ed25519 signature over metadata; empty if no key
}

// RatchetManager owns the current ratchet state and handles advances.
// Thread-safety: the current state is held in an atomic.Pointer for
// lock-free reads on the hot path (sketch computation). Writes (advance,
// shred, persist) take the mutex.
type RatchetManager struct {
	mu           sync.Mutex
	current      atomic.Pointer[RatchetState]
	db           *sql.DB
	sd           *secrets.Dir
	signingKey   ed25519.PrivateKey // nil when signing disabled
	canonicalDim uint32
	sketchBits   uint32
	logger       *slog.Logger
}

// NewRatchetManager creates a ratchet manager. If no state exists in the DB,
// it initializes a new one from crypto/rand. If a state exists, it loads the
// active (non-shredded) one.
func NewRatchetManager(
	db *sql.DB,
	sd *secrets.Dir,
	signingKey ed25519.PrivateKey, // nil if signing disabled
	canonicalDim, sketchBits uint32,
	logger *slog.Logger,
) (*RatchetManager, error) {
	m := &RatchetManager{
		db:           db,
		sd:           sd,
		signingKey:   signingKey,
		canonicalDim: canonicalDim,
		sketchBits:   sketchBits,
		logger:       logger,
	}
	state, err := m.loadOrInitialize()
	if err != nil {
		return nil, err
	}
	m.current.Store(state)
	return m, nil
}

// Current returns the active ratchet state. Safe for concurrent use.
// Hot path: called on every sketch computation.
func (m *RatchetManager) Current() *RatchetState {
	return m.current.Load()
}

// Advance produces a new ratchet state by applying HMAC-SHA-256 to the
// current state with the advance label. The old state is shredded.
//
// Forward security: once this function returns, the old state bytes are
// not recoverable from the new state or from persistent storage.
func (m *RatchetManager) Advance(reason string) (*RatchetState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.current.Load()
	if old == nil {
		return nil, errors.New("ratchet advance: no current state")
	}

	// S_{i+1} = HMAC-SHA-256(S_i, advance_label)
	mac := hmac.New(sha256.New, old.StateBytes[:])
	mac.Write([]byte(ratchetAdvanceLabelV1))
	newBytes := mac.Sum(nil)
	var newBytesArr [32]byte
	copy(newBytesArr[:], newBytes)

	// Persist the new state
	newState, err := m.persistNewState(newBytesArr)
	if err != nil {
		return nil, fmt.Errorf("persist new ratchet state: %w", err)
	}

	// Chaos kill point: new state persisted but old state not yet shredded.
	ChaosKillPoint("ratchet_advance_after_insert")

	// Shred the old state in the DB
	if err := m.shredState(old.StateID); err != nil {
		return nil, fmt.Errorf("shred old ratchet state: %w", err)
	}

	// Update in-memory pointer. After this store, new calls to Current()
	// return newState. Existing goroutines may still hold old via their
	// local pointer — we must NOT zeroize old.StateBytes because those
	// goroutines may still be reading it. The DB state_bytes are already
	// zeroed by shredState() above; the in-memory copy will be GC'd when
	// all references are dropped.
	m.current.Store(newState)

	// Update the secrets file (best effort; DB is ground truth)
	if err := m.sd.WriteSecret(ratchetSecretName, newState.StateBytes[:]); err != nil {
		m.logger.Warn("ratchet: failed to update secret file, DB is ground truth",
			"error", err, "new_state_id", newState.StateID)
	}

	m.logger.Info("ratchet advanced",
		"reason", reason,
		"old_state_id", old.StateID,
		"new_state_id", newState.StateID,
	)

	return newState, nil
}

// Shutdown persists the current state to the secrets file.
func (m *RatchetManager) Shutdown() error {
	current := m.current.Load()
	if current == nil {
		return nil
	}
	return m.sd.WriteSecret(ratchetSecretName, current.StateBytes[:])
}

// ─── Persistence ────────────────────────────────────────────────────────────

func (m *RatchetManager) loadOrInitialize() (*RatchetState, error) {
	row := m.db.QueryRow(`
		SELECT state_id, created_at, state_bytes, canonical_dim, sketch_bits, signature
		FROM substrate_ratchet_states
		WHERE shredded_at IS NULL
		ORDER BY state_id DESC
		LIMIT 1
	`)
	var (
		stateID      uint32
		createdNano  int64
		stateBytes   []byte
		canonicalDim uint32
		sketchBits   uint32
		signature    []byte
	)
	err := row.Scan(&stateID, &createdNano, &stateBytes, &canonicalDim, &sketchBits, &signature)
	if errors.Is(err, sql.ErrNoRows) {
		return m.initializeFirstState()
	}
	if err != nil {
		return nil, fmt.Errorf("load ratchet state: %w", err)
	}
	if len(stateBytes) != 32 {
		return nil, fmt.Errorf("load ratchet state: invalid state_bytes length %d", len(stateBytes))
	}
	if isAllZero(stateBytes) {
		return nil, errors.New("load ratchet state: active state bytes are zeroed, DB inconsistent")
	}

	state := &RatchetState{
		StateID:      stateID,
		CreatedAt:    time.Unix(0, createdNano),
		CanonicalDim: canonicalDim,
		SketchBits:   sketchBits,
		Signature:    signature,
	}
	copy(state.StateBytes[:], stateBytes)
	return state, nil
}

func (m *RatchetManager) initializeFirstState() (*RatchetState, error) {
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, fmt.Errorf("generate ratchet entropy: %w", err)
	}
	// S_0 = HMAC-SHA-256(entropy, init_label)
	mac := hmac.New(sha256.New, entropy[:])
	mac.Write([]byte(ratchetInitLabelV1))
	initBytes := mac.Sum(nil)
	var initBytesArr [32]byte
	copy(initBytesArr[:], initBytes)

	// Zeroize entropy
	for i := range entropy {
		entropy[i] = 0
	}

	state, err := m.persistNewState(initBytesArr)
	if err != nil {
		return nil, fmt.Errorf("persist first ratchet state: %w", err)
	}

	if err := m.sd.WriteSecret(ratchetSecretName, state.StateBytes[:]); err != nil {
		m.logger.Warn("ratchet init: failed to write secret file", "error", err)
	}

	m.logger.Info("ratchet initialized", "state_id", state.StateID)
	return state, nil
}

func (m *RatchetManager) persistNewState(stateBytes [32]byte) (*RatchetState, error) {
	createdAt := time.Now()
	sig := m.signMetadata(0, createdAt) // stateID not known yet; sign without it

	result, err := m.db.Exec(`
		INSERT INTO substrate_ratchet_states
		(created_at, shredded_at, state_bytes, canonical_dim, sketch_bits, signature)
		VALUES (?, NULL, ?, ?, ?, ?)
	`, createdAt.UnixNano(), stateBytes[:], m.canonicalDim, m.sketchBits, sig)
	if err != nil {
		return nil, err
	}
	stateID64, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &RatchetState{
		StateID:      uint32(stateID64),
		StateBytes:   stateBytes,
		CreatedAt:    createdAt,
		CanonicalDim: m.canonicalDim,
		SketchBits:   m.sketchBits,
		Signature:    sig,
	}, nil
}

func (m *RatchetManager) shredState(stateID uint32) error {
	zeroBytes := make([]byte, 32)
	_, err := m.db.Exec(`
		UPDATE substrate_ratchet_states
		SET shredded_at = ?, state_bytes = ?
		WHERE state_id = ?
	`, time.Now().UnixNano(), zeroBytes, stateID)
	return err
}

func (m *RatchetManager) signMetadata(stateID uint32, createdAt time.Time) []byte {
	if m.signingKey == nil {
		return []byte{} // empty, not nil — DB column is NOT NULL
	}
	buf := make([]byte, 4+8+4+4) // stateID + createdAt + canonicalDim + sketchBits
	binary.LittleEndian.PutUint32(buf[0:4], stateID)
	binary.LittleEndian.PutUint64(buf[4:12], uint64(createdAt.UnixNano()))
	binary.LittleEndian.PutUint32(buf[12:16], m.canonicalDim)
	binary.LittleEndian.PutUint32(buf[16:20], m.sketchBits)
	return ed25519.Sign(m.signingKey, buf)
}

// ─── Zeroization ────────────────────────────────────────────────────────────

// zeroizeStateBytes overwrites a 32-byte state buffer with zeros.
// Best-effort: Go does not guarantee the compiler won't retain a copy.
func zeroizeStateBytes(state *[32]byte) {
	for i := range state {
		state[i] = 0
	}
	runtime.KeepAlive(state)
}

// isAllZero returns true if every byte in the slice is zero.
func isAllZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// ─── Rotation Scheduler ─────────────────────────────────────────────────────

// RotationScheduler runs periodic ratchet advances on a goroutine.
type RotationScheduler struct {
	manager *RatchetManager
	period  time.Duration
	stopCh  chan struct{}
	doneCh  chan struct{}
	logger  *slog.Logger
}

// NewRotationScheduler creates a scheduler that advances the ratchet
// every period. Call Start() to begin, Stop() to terminate.
func NewRotationScheduler(m *RatchetManager, period time.Duration, logger *slog.Logger) *RotationScheduler {
	return &RotationScheduler{
		manager: m,
		period:  period,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
		logger:  logger,
	}
}

// Start begins the rotation goroutine.
func (s *RotationScheduler) Start() {
	go s.run()
}

// Stop halts the rotation goroutine and waits for it to exit.
func (s *RotationScheduler) Stop() {
	close(s.stopCh)
	<-s.doneCh
}

func (s *RotationScheduler) run() {
	defer close(s.doneCh)
	ticker := time.NewTicker(s.period)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := s.manager.Advance("scheduled-rotation"); err != nil {
				s.logger.Error("scheduled ratchet advance failed", "error", err)
			}
		case <-s.stopCh:
			return
		}
	}
}
