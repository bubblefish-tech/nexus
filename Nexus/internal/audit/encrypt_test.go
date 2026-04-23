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

package audit_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/audit"
	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
	"github.com/bubblefish-tech/nexus/internal/wal"
)

// newTestMKM creates a MasterKeyManager with a known password and temp salt.
func newTestMKM(t *testing.T, password string) *nexuscrypto.MasterKeyManager {
	t.Helper()
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := nexuscrypto.NewMasterKeyManager(password, saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	return mkm
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestPayloadCrypto_DisabledWhenNoPassword verifies NewPayloadCrypto returns nil
// when the MKM is disabled (no password).
func TestPayloadCrypto_DisabledWhenNoPassword(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "") // empty password → IsEnabled() false
	if !mkm.IsEnabled() {
		pc := audit.NewPayloadCrypto(mkm)
		if pc != nil {
			t.Error("expected nil PayloadCrypto when MKM disabled")
		}
	}
}

// TestPayloadCrypto_NilMKM verifies NewPayloadCrypto returns nil for nil MKM.
func TestPayloadCrypto_NilMKM(t *testing.T) {
	t.Helper()
	if audit.NewPayloadCrypto(nil) != nil {
		t.Error("expected nil PayloadCrypto for nil MKM")
	}
}

// TestInteractionPayload_RoundTrip verifies encrypt → decrypt restores all fields.
func TestInteractionPayload_RoundTrip(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "test-password-1")
	if !mkm.IsEnabled() {
		t.Skip("MKM not enabled")
	}
	pc := audit.NewPayloadCrypto(mkm)

	orig := audit.InteractionRecord{
		RecordID:       "rt-001",
		RequestID:      "req-abc",
		Timestamp:      time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
		Source:         "claude-code",
		ActorType:      "agent",
		ActorID:        "agent-xyz",
		EffectiveIP:    "10.0.0.1",
		OperationType:  "write",
		Endpoint:       "/inbound/claude",
		HTTPMethod:     "POST",
		HTTPStatusCode: 200,
		PayloadID:      "pay-001",
		Destination:    "sqlite",
		Subject:        "test-subject",
		PolicyDecision: "allowed",
		LatencyMs:      12.5,
	}

	// Encrypt.
	enc := orig
	dir := t.TempDir()
	w, err := wal.Open(dir, 50, newTestLogger())
	if err != nil {
		t.Fatalf("Open WAL: %v", err)
	}
	defer w.Close()

	aw := audit.NewWALWriter(w, nil)
	aw.SetEncryption(mkm)
	if err := aw.Submit(enc); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Read encrypted WAL entry.
	if err := w.Close(); err != nil {
		t.Fatalf("Close WAL: %v", err)
	}
	segs, err := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	if err != nil || len(segs) == 0 {
		t.Fatal("no WAL segments found")
	}
	data, _ := os.ReadFile(segs[0])
	var encRec audit.InteractionRecord
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 2 {
			continue
		}
		jsonField := parts[0]
		if jsonField == wal.StartSentinel && len(parts) >= 4 {
			jsonField = parts[1]
		}
		var entry wal.Entry
		if json.Unmarshal([]byte(jsonField), &entry) != nil {
			continue
		}
		if entry.EntryType == wal.EntryTypeAudit {
			if json.Unmarshal(entry.Payload, &encRec) != nil {
				continue
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("audit entry not found in WAL")
	}

	// Verify chain fields are plaintext.
	if encRec.RecordID == "" {
		t.Error("RecordID should be plaintext after encryption")
	}
	if encRec.Timestamp.IsZero() {
		t.Error("Timestamp should be plaintext after encryption")
	}
	if encRec.EncryptionVersion != 1 {
		t.Errorf("EncryptionVersion = %d, want 1", encRec.EncryptionVersion)
	}
	if len(encRec.PayloadEncrypted) == 0 {
		t.Error("PayloadEncrypted should be non-empty")
	}

	// Verify sensitive fields are cleared.
	if encRec.Source != "" {
		t.Errorf("Source should be empty after encryption, got %q", encRec.Source)
	}
	if encRec.ActorID != "" {
		t.Errorf("ActorID should be empty after encryption, got %q", encRec.ActorID)
	}
	if encRec.EffectiveIP != "" {
		t.Errorf("EffectiveIP should be empty after encryption, got %q", encRec.EffectiveIP)
	}

	// Decrypt and verify restored fields.
	if err := audit.DecryptInteractionPayload(pc, &encRec); err != nil {
		t.Fatalf("DecryptInteractionPayload: %v", err)
	}
	if encRec.Source != orig.Source {
		t.Errorf("Source = %q, want %q", encRec.Source, orig.Source)
	}
	if encRec.ActorID != orig.ActorID {
		t.Errorf("ActorID = %q, want %q", encRec.ActorID, orig.ActorID)
	}
	if encRec.EffectiveIP != orig.EffectiveIP {
		t.Errorf("EffectiveIP = %q, want %q", encRec.EffectiveIP, orig.EffectiveIP)
	}
	if encRec.Endpoint != orig.Endpoint {
		t.Errorf("Endpoint = %q, want %q", encRec.Endpoint, orig.Endpoint)
	}
	if encRec.PayloadID != orig.PayloadID {
		t.Errorf("PayloadID = %q, want %q", encRec.PayloadID, orig.PayloadID)
	}
	if encRec.LatencyMs != orig.LatencyMs {
		t.Errorf("LatencyMs = %v, want %v", encRec.LatencyMs, orig.LatencyMs)
	}
	if encRec.EncryptionVersion != 0 {
		t.Errorf("EncryptionVersion after decrypt = %d, want 0", encRec.EncryptionVersion)
	}
	if encRec.PayloadEncrypted != nil {
		t.Error("PayloadEncrypted should be nil after decrypt")
	}
}

// TestControlPayload_RoundTrip verifies encrypt → decrypt restores all fields.
func TestControlPayload_RoundTrip(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "test-password-ctrl")
	if !mkm.IsEnabled() {
		t.Skip("MKM not enabled")
	}
	pc := audit.NewPayloadCrypto(mkm)

	orig := audit.ControlEventRecord{
		RecordID:   "ctrl-001",
		EventType:  audit.ControlEventGrantCreated,
		Actor:      "admin",
		ActorType:  "admin",
		TargetID:   "grant-abc",
		TargetType: "grant",
		AgentID:    "agent-1",
		Capability: "nexus_write",
		EntityJSON: json.RawMessage(`{"grant_id":"grant-abc"}`),
		Decision:   "allowed",
		Reason:     "test reason",
		Timestamp:  time.Date(2026, 4, 18, 1, 0, 0, 0, time.UTC),
	}

	rec := orig
	dir := t.TempDir()
	w, err := wal.Open(dir, 50, newTestLogger())
	if err != nil {
		t.Fatalf("Open WAL: %v", err)
	}
	defer w.Close()

	aw := audit.NewWALWriter(w, nil)
	aw.SetEncryption(mkm)
	if err := aw.SubmitControl(rec); err != nil {
		t.Fatalf("SubmitControl: %v", err)
	}

	// Read encrypted WAL entry.
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	segs, _ := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	data, _ := os.ReadFile(segs[0])
	var encRec audit.ControlEventRecord
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 2 {
			continue
		}
		jsonField := parts[0]
		if jsonField == wal.StartSentinel && len(parts) >= 4 {
			jsonField = parts[1]
		}
		var entry wal.Entry
		if json.Unmarshal([]byte(jsonField), &entry) != nil {
			continue
		}
		if entry.EntryType == wal.EntryTypeAudit {
			if json.Unmarshal(entry.Payload, &encRec) != nil {
				continue
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("control audit entry not found in WAL")
	}

	// Verify chain fields are plaintext.
	if encRec.RecordID == "" {
		t.Error("RecordID should be plaintext")
	}
	if encRec.EventType != audit.ControlEventGrantCreated {
		t.Errorf("EventType = %q, want grant_created", encRec.EventType)
	}
	if encRec.EncryptionVersion != 1 {
		t.Errorf("EncryptionVersion = %d, want 1", encRec.EncryptionVersion)
	}

	// Verify payload fields are cleared.
	if encRec.Actor != "" {
		t.Errorf("Actor should be empty, got %q", encRec.Actor)
	}
	if encRec.AgentID != "" {
		t.Errorf("AgentID should be empty, got %q", encRec.AgentID)
	}
	if encRec.Capability != "" {
		t.Errorf("Capability should be empty, got %q", encRec.Capability)
	}

	// Hash must be non-empty (computed over encrypted envelope).
	if encRec.Hash == "" {
		t.Error("Hash should be set (computed over encrypted envelope)")
	}

	// Decrypt and verify.
	if err := audit.DecryptControlPayload(pc, &encRec); err != nil {
		t.Fatalf("DecryptControlPayload: %v", err)
	}
	if encRec.Actor != orig.Actor {
		t.Errorf("Actor = %q, want %q", encRec.Actor, orig.Actor)
	}
	if encRec.AgentID != orig.AgentID {
		t.Errorf("AgentID = %q, want %q", encRec.AgentID, orig.AgentID)
	}
	if encRec.Capability != orig.Capability {
		t.Errorf("Capability = %q, want %q", encRec.Capability, orig.Capability)
	}
	if encRec.Reason != orig.Reason {
		t.Errorf("Reason = %q, want %q", encRec.Reason, orig.Reason)
	}
	if !bytes.Equal(encRec.EntityJSON, orig.EntityJSON) {
		t.Errorf("EntityJSON = %s, want %s", encRec.EntityJSON, orig.EntityJSON)
	}
}

// TestInteractionPayload_WrongKeyFails verifies decryption fails with wrong key.
func TestInteractionPayload_WrongKeyFails(t *testing.T) {
	t.Helper()
	mkm1 := newTestMKM(t, "password-one")
	mkm2 := newTestMKM(t, "password-two")
	if !mkm1.IsEnabled() || !mkm2.IsEnabled() {
		t.Skip("MKM not enabled")
	}
	pc2 := audit.NewPayloadCrypto(mkm2)

	dir := t.TempDir()
	w, err := wal.Open(dir, 50, newTestLogger())
	if err != nil {
		t.Fatalf("Open WAL: %v", err)
	}

	aw := audit.NewWALWriter(w, nil)
	aw.SetEncryption(mkm1)
	if err := aw.Submit(audit.InteractionRecord{
		RecordID:      "wrong-key-test",
		Source:        "secret-source",
		OperationType: "write",
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	_ = w.Close()

	segs, _ := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	data, _ := os.ReadFile(segs[0])
	var encRec audit.InteractionRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 2 {
			continue
		}
		jsonField := parts[0]
		if jsonField == wal.StartSentinel && len(parts) >= 4 {
			jsonField = parts[1]
		}
		var entry wal.Entry
		if json.Unmarshal([]byte(jsonField), &entry) != nil {
			continue
		}
		if entry.EntryType == wal.EntryTypeAudit {
			_ = json.Unmarshal(entry.Payload, &encRec)
			break
		}
	}

	// Decryption with wrong key must fail.
	if err := audit.DecryptInteractionPayload(pc2, &encRec); err == nil {
		t.Error("expected decryption error with wrong key, got nil")
	}
}

// TestControlPayload_WrongKeyFails verifies decryption fails with wrong key.
func TestControlPayload_WrongKeyFails(t *testing.T) {
	t.Helper()
	mkm1 := newTestMKM(t, "ctrl-pass-a")
	mkm2 := newTestMKM(t, "ctrl-pass-b")
	if !mkm1.IsEnabled() || !mkm2.IsEnabled() {
		t.Skip("MKM not enabled")
	}
	pc2 := audit.NewPayloadCrypto(mkm2)

	dir := t.TempDir()
	w, _ := wal.Open(dir, 50, newTestLogger())
	aw := audit.NewWALWriter(w, nil)
	aw.SetEncryption(mkm1)
	_ = aw.SubmitControl(audit.ControlEventRecord{
		RecordID:  "ctrl-wrong-key",
		EventType: audit.ControlEventActionDenied,
		AgentID:   "agent-secret",
	})
	_ = w.Close()

	segs, _ := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	data, _ := os.ReadFile(segs[0])
	var encRec audit.ControlEventRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 2 {
			continue
		}
		jsonField := parts[0]
		if jsonField == wal.StartSentinel && len(parts) >= 4 {
			jsonField = parts[1]
		}
		var entry wal.Entry
		if json.Unmarshal([]byte(jsonField), &entry) != nil {
			continue
		}
		if entry.EntryType == wal.EntryTypeAudit {
			_ = json.Unmarshal(entry.Payload, &encRec)
			break
		}
	}

	if err := audit.DecryptControlPayload(pc2, &encRec); err == nil {
		t.Error("expected decryption error with wrong key, got nil")
	}
}

// TestDecryptInteractionPayload_Plaintext verifies no-op on unencrypted record.
func TestDecryptInteractionPayload_Plaintext(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "some-password")
	if !mkm.IsEnabled() {
		t.Skip()
	}
	pc := audit.NewPayloadCrypto(mkm)

	rec := audit.InteractionRecord{
		RecordID: "plain-rec",
		Source:   "original-source",
	}
	// No encryption, DecryptInteractionPayload should be a no-op.
	if err := audit.DecryptInteractionPayload(pc, &rec); err != nil {
		t.Fatalf("unexpected error on plaintext record: %v", err)
	}
	if rec.Source != "original-source" {
		t.Error("Source should be unchanged for plaintext record")
	}
}

// TestDecryptControlPayload_Plaintext verifies no-op on unencrypted record.
func TestDecryptControlPayload_Plaintext(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "some-password-ctrl")
	if !mkm.IsEnabled() {
		t.Skip()
	}
	pc := audit.NewPayloadCrypto(mkm)

	rec := audit.ControlEventRecord{
		RecordID:  "ctrl-plain",
		Actor:     "original-actor",
		EventType: audit.ControlEventGrantCreated,
	}
	if err := audit.DecryptControlPayload(pc, &rec); err != nil {
		t.Fatalf("unexpected error on plaintext record: %v", err)
	}
	if rec.Actor != "original-actor" {
		t.Error("Actor should be unchanged for plaintext record")
	}
}

// TestChainVerifiable_WithoutDecryptionKey verifies chain fields are plaintext.
func TestChainVerifiable_WithoutDecryptionKey(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "chain-verify-pass")
	if !mkm.IsEnabled() {
		t.Skip()
	}

	dir := t.TempDir()
	w, err := wal.Open(dir, 50, newTestLogger())
	if err != nil {
		t.Fatalf("Open WAL: %v", err)
	}

	aw := audit.NewWALWriter(w, nil)
	aw.SetEncryption(mkm)

	recs := []audit.ControlEventRecord{
		{RecordID: "chain-v-1", EventType: audit.ControlEventGrantCreated, AgentID: "ag-1", Actor: "admin"},
		{RecordID: "chain-v-2", EventType: audit.ControlEventTaskCreated, AgentID: "ag-2", Actor: "admin"},
		{RecordID: "chain-v-3", EventType: audit.ControlEventActionExecuted, AgentID: "ag-3", Actor: "admin"},
	}
	for _, r := range recs {
		if err := aw.SubmitControl(r); err != nil {
			t.Fatalf("SubmitControl %s: %v", r.RecordID, err)
		}
	}
	_ = w.Close()

	segs, _ := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	data, _ := os.ReadFile(segs[0])

	type chainFields struct {
		RecordID          string `json:"record_id"`
		EventType         string `json:"event_type"`
		Hash              string `json:"hash"`
		EncryptionVersion int    `json:"encryption_version"`
		Actor             string `json:"actor"`
	}

	var chainRecs []chainFields
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 2 {
			continue
		}
		jsonField := parts[0]
		if jsonField == wal.StartSentinel && len(parts) >= 4 {
			jsonField = parts[1]
		}
		var entry wal.Entry
		if json.Unmarshal([]byte(jsonField), &entry) != nil {
			continue
		}
		if entry.EntryType == wal.EntryTypeAudit {
			var cf chainFields
			if json.Unmarshal(entry.Payload, &cf) == nil {
				chainRecs = append(chainRecs, cf)
			}
		}
	}

	if len(chainRecs) != 3 {
		t.Fatalf("expected 3 chain records, got %d", len(chainRecs))
	}
	for _, cf := range chainRecs {
		if cf.RecordID == "" {
			t.Error("RecordID should be plaintext (chain metadata)")
		}
		if cf.EventType == "" {
			t.Error("EventType should be plaintext (chain metadata)")
		}
		if cf.Hash == "" {
			t.Error("Hash should be set (chain metadata)")
		}
		if cf.EncryptionVersion != 1 {
			t.Errorf("EncryptionVersion = %d, want 1", cf.EncryptionVersion)
		}
		// Sensitive field must NOT appear in plaintext.
		if cf.Actor != "" {
			t.Errorf("Actor should be encrypted, found plaintext value %q", cf.Actor)
		}
	}
}

// TestDifferentRecordsDifferentKeys verifies two records have different
// encrypted blobs even with the same plaintext payload.
func TestDifferentRecordsDifferentKeys(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "diff-key-pass")
	if !mkm.IsEnabled() {
		t.Skip()
	}

	dir := t.TempDir()
	w, _ := wal.Open(dir, 50, newTestLogger())
	aw := audit.NewWALWriter(w, nil)
	aw.SetEncryption(mkm)

	for i, id := range []string{"diff-1", "diff-2"} {
		_ = aw.Submit(audit.InteractionRecord{
			RecordID:      id,
			Source:        "same-source",
			OperationType: "write",
			LatencyMs:     float64(i),
		})
	}
	_ = w.Close()

	segs, _ := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	data, _ := os.ReadFile(segs[0])

	var blobs [][]byte
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 2 {
			continue
		}
		jsonField := parts[0]
		if jsonField == wal.StartSentinel && len(parts) >= 4 {
			jsonField = parts[1]
		}
		var entry wal.Entry
		if json.Unmarshal([]byte(jsonField), &entry) != nil {
			continue
		}
		if entry.EntryType == wal.EntryTypeAudit {
			var rec audit.InteractionRecord
			if json.Unmarshal(entry.Payload, &rec) == nil && rec.EncryptionVersion == 1 {
				blobs = append(blobs, rec.PayloadEncrypted)
			}
		}
	}

	if len(blobs) != 2 {
		t.Fatalf("expected 2 encrypted blobs, got %d", len(blobs))
	}
	if bytes.Equal(blobs[0], blobs[1]) {
		t.Error("different records should produce different ciphertext blobs")
	}
}

// TestHashCoversEncryptedPayload verifies the ControlEventRecord Hash field
// differs between encrypted and plaintext versions of the same record.
func TestHashCoversEncryptedPayload(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "hash-covers-enc")
	if !mkm.IsEnabled() {
		t.Skip()
	}

	plainRec := audit.ControlEventRecord{
		RecordID:   "hash-cov-1",
		EventType:  audit.ControlEventGrantCreated,
		AgentID:    "agent-hash",
		Capability: "nexus_write",
	}
	plainRec.Hash = plainRec.ComputeHash()

	// Submit with encryption enabled.
	dir := t.TempDir()
	w, _ := wal.Open(dir, 50, newTestLogger())
	aw := audit.NewWALWriter(w, nil)
	aw.SetEncryption(mkm)
	_ = aw.SubmitControl(audit.ControlEventRecord{
		RecordID:   "hash-cov-1",
		EventType:  audit.ControlEventGrantCreated,
		AgentID:    "agent-hash",
		Capability: "nexus_write",
	})
	_ = w.Close()

	segs, _ := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	data, _ := os.ReadFile(segs[0])
	var encHash string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 2 {
			continue
		}
		jsonField := parts[0]
		if jsonField == wal.StartSentinel && len(parts) >= 4 {
			jsonField = parts[1]
		}
		var entry wal.Entry
		if json.Unmarshal([]byte(jsonField), &entry) != nil {
			continue
		}
		if entry.EntryType == wal.EntryTypeAudit {
			var rec audit.ControlEventRecord
			if json.Unmarshal(entry.Payload, &rec) == nil {
				encHash = rec.Hash
			}
			break
		}
	}

	if encHash == "" {
		t.Fatal("encrypted record has no Hash")
	}
	if encHash == plainRec.Hash {
		t.Error("encrypted and plaintext hashes should differ (encrypted covers blob, not plaintext fields)")
	}
}

// TestSetEncryption_NilMKM verifies SetEncryption with nil MKM leaves crypto
// disabled (Submit still works, no encryption applied).
func TestSetEncryption_NilMKM(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	w, err := wal.Open(dir, 50, newTestLogger())
	if err != nil {
		t.Fatalf("Open WAL: %v", err)
	}

	aw := audit.NewWALWriter(w, nil)
	aw.SetEncryption(nil) // nil MKM → no-op

	rec := audit.InteractionRecord{
		RecordID:      "nil-mkm-test",
		Source:        "visible-source",
		OperationType: "write",
	}
	if err := aw.Submit(rec); err != nil {
		t.Fatalf("Submit with nil MKM encryption: %v", err)
	}
	_ = w.Close()

	segs, _ := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	data, _ := os.ReadFile(segs[0])
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 2 {
			continue
		}
		jsonField := parts[0]
		if jsonField == wal.StartSentinel && len(parts) >= 4 {
			jsonField = parts[1]
		}
		var entry wal.Entry
		if json.Unmarshal([]byte(jsonField), &entry) != nil {
			continue
		}
		if entry.EntryType == wal.EntryTypeAudit {
			var got audit.InteractionRecord
			if err := json.Unmarshal(entry.Payload, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.EncryptionVersion != 0 {
				t.Errorf("EncryptionVersion = %d, want 0 (no encryption)", got.EncryptionVersion)
			}
			if got.Source != "visible-source" {
				t.Errorf("Source = %q, want visible-source (no encryption applied)", got.Source)
			}
			return
		}
	}
	t.Error("audit entry not found")
}

// TestControlPayload_EmptyEntityJSON verifies nil EntityJSON survives round-trip.
func TestControlPayload_EmptyEntityJSON(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "empty-entity-pass")
	if !mkm.IsEnabled() {
		t.Skip()
	}
	pc := audit.NewPayloadCrypto(mkm)

	orig := audit.ControlEventRecord{
		RecordID:  "no-entity",
		EventType: audit.ControlEventActionDenied,
		AgentID:   "agent-no-entity",
		Decision:  "denied",
		// EntityJSON intentionally nil
	}

	dir := t.TempDir()
	w, _ := wal.Open(dir, 50, newTestLogger())
	aw := audit.NewWALWriter(w, nil)
	aw.SetEncryption(mkm)
	_ = aw.SubmitControl(orig)
	_ = w.Close()

	segs, _ := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	data, _ := os.ReadFile(segs[0])
	var encRec audit.ControlEventRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 2 {
			continue
		}
		jsonField := parts[0]
		if jsonField == wal.StartSentinel && len(parts) >= 4 {
			jsonField = parts[1]
		}
		var entry wal.Entry
		if json.Unmarshal([]byte(jsonField), &entry) != nil {
			continue
		}
		if entry.EntryType == wal.EntryTypeAudit {
			_ = json.Unmarshal(entry.Payload, &encRec)
			break
		}
	}

	if err := audit.DecryptControlPayload(pc, &encRec); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if encRec.Decision != orig.Decision {
		t.Errorf("Decision = %q, want %q", encRec.Decision, orig.Decision)
	}
	if encRec.EntityJSON != nil {
		t.Errorf("EntityJSON should be nil after round-trip, got %s", encRec.EntityJSON)
	}
}
