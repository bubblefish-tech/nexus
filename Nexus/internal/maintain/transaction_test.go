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

package maintain_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/maintain"
)

// TestTransaction_Success runs a 3-step transaction and verifies committed status.
func TestTransaction_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(path, []byte(`{"v":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	steps := []maintain.Step{
		{Action: maintain.ActionBackupFile, Params: map[string]any{"path": path}},
		{Action: maintain.ActionSetConfigKey, Params: map[string]any{"path": path, "key": "v", "value": float64(2)}},
		{Action: maintain.ActionVerifyConfig, Params: map[string]any{"path": path}},
	}
	tx := maintain.NewTransaction("test-tool", steps)
	if err := tx.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tx.Status != "committed" {
		t.Errorf("expected committed, got %s", tx.Status)
	}
}

// TestTransaction_RollbackOnFailure verifies that when step 1 fails, the file
// modified by step 0 is restored to its pre-transaction state.
func TestTransaction_RollbackOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	original := `{"original":true}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	steps := []maintain.Step{
		// Step 0 modifies the file
		{Action: maintain.ActionSetConfigKey, Params: map[string]any{"path": path, "key": "x", "value": "changed"}},
		// Step 1 will fail (missing.json does not exist)
		{Action: maintain.ActionVerifyConfig, Params: map[string]any{"path": filepath.Join(dir, "missing.json")}},
	}
	tx := maintain.NewTransaction("test-tool", steps)
	err := tx.Execute(context.Background())
	if err == nil {
		t.Fatal("expected Execute to fail")
	}
	// File must be restored to original
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("rollback failed: got %q, expected %q", string(data), original)
	}
}

// TestTransaction_UniqueIDs verifies no two transactions share an ID.
func TestTransaction_UniqueIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		tx := maintain.NewTransaction("tool", nil)
		if seen[tx.ID] {
			t.Errorf("duplicate ID: %s", tx.ID)
		}
		seen[tx.ID] = true
	}
}

// TestTransaction_ConcurrentDifferentFiles verifies parallel transactions on
// different files complete without deadlock or error.
func TestTransaction_ConcurrentDifferentFiles(t *testing.T) {
	dir := t.TempDir()
	makeFile := func(name string) string {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, []byte(`{"tool":"x"}`), 0600)
		return p
	}
	pathA, pathB := makeFile("a.json"), makeFile("b.json")

	run := func(path string) error {
		steps := []maintain.Step{
			{Action: maintain.ActionSetConfigKey, Params: map[string]any{"path": path, "key": "tool", "value": "updated"}},
		}
		return maintain.NewTransaction("tool", steps).Execute(context.Background())
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs[0] = run(pathA) }()
	go func() { defer wg.Done(); errs[1] = run(pathB) }()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
}

// TestTransaction_ConcurrentSameFile verifies concurrent transactions on the
// same file are serialised without corrupting the JSON.
func TestTransaction_ConcurrentSameFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.json")
	if err := os.WriteFile(path, []byte(`{"count":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			steps := []maintain.Step{
				{Action: maintain.ActionSetConfigKey, Params: map[string]any{"path": path, "key": "k", "value": "1"}},
			}
			errs[i] = maintain.NewTransaction("tool", steps).Execute(context.Background())
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	// File must remain valid JSON after concurrent writes
	if _, err := maintain.ExecuteAction(context.Background(), maintain.ActionVerifyConfig, map[string]any{"path": path}); err != nil {
		t.Errorf("file corrupted: %v", err)
	}
}

// TestTransaction_ExplicitRollback exercises the public Rollback() method on a
// committed transaction to restore the file to its pre-transaction state.
func TestTransaction_ExplicitRollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	original := `{"orig":true}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	steps := []maintain.Step{
		{Action: maintain.ActionSetConfigKey, Params: map[string]any{"path": path, "key": "orig", "value": false}},
	}
	tx := maintain.NewTransaction("tool", steps)
	if err := tx.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("explicit rollback failed: got %q", string(data))
	}
}

// TestTransaction_CrashRecovery simulates a crash mid-transaction by writing a
// journal file with status "executing" and pre-state captured, then verifying
// RecoverIncomplete restores the file.
func TestTransaction_CrashRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	original := `{"before":"crash"}`
	// File currently shows partial write (crash happened after step ran)
	if err := os.WriteFile(path, []byte(`{"before":"crash","partial":true}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Construct the journal that would have been written before the crash
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	journalDir := filepath.Join(home, ".nexus", "maintain", "transactions")
	if err := os.MkdirAll(journalDir, 0700); err != nil {
		t.Fatal(err)
	}

	txID := "tx_test_crash_recovery"
	crashTx := maintain.Transaction{
		ID:        txID,
		Tool:      "test-tool",
		Status:    "executing",
		StartedAt: time.Now().UTC(),
		Steps: []maintain.Step{
			{Action: maintain.ActionSetConfigKey, Params: map[string]any{"path": path, "key": "partial", "value": true}},
		},
		Journal: []maintain.JournalEntry{
			{
				StepIndex: 0,
				Action:    maintain.ActionSetConfigKey,
				PreState:  []byte(original),
				Path:      path,
				Timestamp: time.Now().UTC(),
				Undone:    false,
			},
		},
	}
	journalData, err := json.Marshal(&crashTx)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(journalDir, txID+".journal")
	if err := os.WriteFile(journalPath, journalData, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(journalPath) })

	if err := maintain.RecoverIncomplete(context.Background()); err != nil {
		t.Fatalf("RecoverIncomplete: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("crash recovery failed: file is %q, expected %q", string(data), original)
	}
}
