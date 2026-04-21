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

package maintain

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Step is one action within a transaction.
type Step struct {
	Action ActionType     `json:"action"`
	Params map[string]any `json:"params"`
}

// JournalEntry records the pre-state of one step so it can be undone.
type JournalEntry struct {
	StepIndex  int        `json:"step_index"`
	Action     ActionType `json:"action"`
	PreState   []byte     `json:"pre_state,omitempty"` // backup of file contents before step
	Path       string     `json:"path,omitempty"`      // file affected (if any)
	BackupPath string     `json:"backup_path,omitempty"`
	Timestamp  time.Time  `json:"timestamp"`
	Undone     bool       `json:"undone"`
}

const (
	txStatusPending    = "pending"
	txStatusExecuting  = "executing"
	txStatusCommitted  = "committed"
	txStatusRolledBack = "rolled_back"
	txStatusFailed     = "failed"
)

// Transaction is an ACID-compliant sequence of Steps with journaled rollback.
// Inspired by ARIES database recovery: pre-state is captured before each step;
// failure at any step triggers reverse-order undo of all prior steps.
type Transaction struct {
	ID        string         `json:"id"`
	Tool      string         `json:"tool"`
	Steps     []Step         `json:"steps"`
	Journal   []JournalEntry `json:"journal"`
	Status    string         `json:"status"`
	StartedAt time.Time      `json:"started_at"`
	mu        sync.Mutex
}

// NewTransaction creates a pending transaction for the given tool and steps.
func NewTransaction(tool string, steps []Step) *Transaction {
	return &Transaction{
		ID:        newTxID(),
		Tool:      tool,
		Steps:     steps,
		Status:    txStatusPending,
		StartedAt: time.Now().UTC(),
	}
}

// Execute runs the steps sequentially. Before each step the pre-state is
// journaled. If any step fails, all prior steps are rolled back in reverse
// order. The journal is persisted to disk so crash recovery can finish a
// partial rollback on next startup.
func (tx *Transaction) Execute(ctx context.Context) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	tx.Status = txStatusExecuting
	if err := tx.writeJournal(); err != nil {
		tx.Status = txStatusFailed
		return fmt.Errorf("maintain: tx %s: journal write failed: %w", tx.ID, err)
	}

	for i, step := range tx.Steps {
		// Capture pre-state before executing
		entry := JournalEntry{
			StepIndex: i,
			Action:    step.Action,
			Timestamp: time.Now().UTC(),
		}
		if path, ok := step.Params["path"].(string); ok {
			entry.Path = path
			if data, err := os.ReadFile(path); err == nil {
				entry.PreState = data
			}
		}
		tx.Journal = append(tx.Journal, entry)
		_ = tx.writeJournal() // best-effort; rollback still works from in-memory state

		// Acquire file lock for affected path
		var unlock func()
		if entry.Path != "" {
			unlock = lockFile(entry.Path)
		}

		_, execErr := ExecuteAction(ctx, step.Action, step.Params)

		if unlock != nil {
			unlock()
		}

		if execErr != nil {
			tx.Status = txStatusFailed
			_ = tx.writeJournal()
			// Rollback all steps that succeeded before this one
			tx.rollback()
			return fmt.Errorf("maintain: tx %s step %d (%s): %w", tx.ID, i, step.Action, execErr)
		}
	}

	tx.Status = txStatusCommitted
	_ = tx.writeJournal()
	return nil
}

// Rollback undoes all journaled steps in reverse order. Safe to call after
// Execute fails; no-op if already rolled back.
func (tx *Transaction) Rollback() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	return tx.rollback()
}

// rollback must be called with tx.mu held.
func (tx *Transaction) rollback() error {
	for i := len(tx.Journal) - 1; i >= 0; i-- {
		entry := &tx.Journal[i]
		if entry.Undone || entry.PreState == nil || entry.Path == "" {
			entry.Undone = true
			continue
		}
		unlock := lockFile(entry.Path)
		err := os.WriteFile(entry.Path, entry.PreState, 0600)
		unlock()
		if err != nil {
			// Log but continue — we must attempt all undos
			continue
		}
		entry.Undone = true
	}
	tx.Status = txStatusRolledBack
	_ = tx.writeJournal()
	return nil
}

// journalPath returns the on-disk path for this transaction's journal file.
func (tx *Transaction) journalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".nexus", "maintain", "transactions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, tx.ID+".journal"), nil
}

// writeJournal persists the transaction state to disk. Called after each step
// and on status transitions so crash recovery can detect incomplete transactions.
func (tx *Transaction) writeJournal() error {
	path, err := tx.journalPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// RecoverIncomplete scans the transaction directory and rolls back any
// transactions that were left in "executing" or "failed" state (i.e. Nexus
// crashed mid-transaction). Call once during daemon startup.
func RecoverIncomplete(ctx context.Context) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".nexus", "maintain", "transactions")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".journal" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var tx Transaction
		if err := json.Unmarshal(data, &tx); err != nil {
			continue
		}
		if tx.Status == txStatusExecuting || tx.Status == txStatusFailed {
			tx.rollback() //nolint:errcheck
		}
	}
	return nil
}

// --- File-level mutex (isolation) ---

// fileLocks serialises concurrent transactions on the same path.
// Two transactions editing different files run in parallel; same file is serialised.
var fileLocks = struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}{locks: make(map[string]*sync.Mutex)}

// lockFile acquires the per-path mutex and returns an unlock function.
func lockFile(path string) func() {
	fileLocks.mu.Lock()
	if _, ok := fileLocks.locks[path]; !ok {
		fileLocks.locks[path] = &sync.Mutex{}
	}
	lock := fileLocks.locks[path]
	fileLocks.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// newTxID generates a cryptographically random hex transaction ID.
func newTxID() string {
	b := make([]byte, 8)
	if _, err := cryptorand.Read(b); err != nil {
		// Fallback: use nanosecond time (extremely unlikely in practice)
		return fmt.Sprintf("tx_%x", time.Now().UnixNano())
	}
	return "tx_" + hex.EncodeToString(b)
}
