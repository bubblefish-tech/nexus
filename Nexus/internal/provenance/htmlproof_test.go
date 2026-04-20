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

package provenance_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/provenance"
)

func TestGenerateHTML_BasicStructure(t *testing.T) {
	t.Helper()
	bundle := &provenance.ProofBundle{
		Version: 1,
		Memory: provenance.ProofMemory{
			PayloadID:   "test-payload-id",
			Source:      "test-source",
			Subject:     "test subject",
			Content:     "hello world",
			ContentHash: provenance.ContentHash("hello world"),
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		},
		GeneratedAt: time.Now().UTC(),
	}
	data, err := provenance.GenerateHTML(bundle)
	if err != nil {
		t.Fatalf("GenerateHTML error: %v", err)
	}
	html := string(data)
	if !strings.HasPrefix(html, "<!DOCTYPE html>") {
		t.Error("expected HTML doctype")
	}
	if !strings.Contains(html, "BubbleFish Nexus") {
		t.Error("expected BubbleFish Nexus in HTML")
	}
	if !strings.Contains(html, "Proof Verification") {
		t.Error("expected Proof Verification in title")
	}
}

func TestGenerateHTML_EmbedsBundleJSON(t *testing.T) {
	t.Helper()
	bundle := &provenance.ProofBundle{
		Version: 1,
		Memory: provenance.ProofMemory{
			PayloadID: "embed-test-id",
			Source:    "embed-source",
			Content:   "embedded content",
		},
		DaemonPubKey: "abcd1234",
		GeneratedAt:  time.Now().UTC(),
	}
	data, err := provenance.GenerateHTML(bundle)
	if err != nil {
		t.Fatalf("GenerateHTML error: %v", err)
	}
	html := string(data)
	if !strings.Contains(html, "embed-test-id") {
		t.Error("expected payload_id in embedded JSON")
	}
	if !strings.Contains(html, "abcd1234") {
		t.Error("expected daemon_pubkey in embedded JSON")
	}
}

func TestGenerateHTML_ValidJSON(t *testing.T) {
	t.Helper()
	bundle := &provenance.ProofBundle{
		Version: 1,
		Memory: provenance.ProofMemory{
			PayloadID: "json-test",
			Content:   `content with "quotes" and special chars: <>&`,
		},
		GeneratedAt: time.Now().UTC(),
	}
	data, err := provenance.GenerateHTML(bundle)
	if err != nil {
		t.Fatalf("GenerateHTML error: %v", err)
	}
	// Extract embedded JSON from the PROOF = ... assignment.
	html := string(data)
	start := strings.Index(html, "var PROOF = ")
	if start < 0 {
		t.Fatal("PROOF assignment not found in HTML")
	}
	start += len("var PROOF = ")
	end := strings.Index(html[start:], ";\n")
	if end < 0 {
		t.Fatal("PROOF end not found")
	}
	jsonStr := html[start : start+end]
	var out provenance.ProofBundle
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		t.Fatalf("embedded JSON invalid: %v", err)
	}
	if out.Memory.PayloadID != "json-test" {
		t.Errorf("got PayloadID %q, want json-test", out.Memory.PayloadID)
	}
}

func TestGenerateHTML_ContentHashCheck(t *testing.T) {
	t.Helper()
	content := "provenance test content"
	bundle := &provenance.ProofBundle{
		Version: 1,
		Memory: provenance.ProofMemory{
			PayloadID:   "hash-test",
			Content:     content,
			ContentHash: provenance.ContentHash(content),
		},
		GeneratedAt: time.Now().UTC(),
	}
	data, err := provenance.GenerateHTML(bundle)
	if err != nil {
		t.Fatalf("GenerateHTML error: %v", err)
	}
	html := string(data)
	// Should contain both the content and its hash for JS verification.
	hash := provenance.ContentHash(content)
	if !strings.Contains(html, hash) {
		t.Errorf("expected content hash %s in HTML", hash)
	}
}

func TestGenerateHTML_Idempotent(t *testing.T) {
	t.Helper()
	bundle := &provenance.ProofBundle{
		Version:     1,
		Memory:      provenance.ProofMemory{PayloadID: "idem-test"},
		GeneratedAt: time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC),
	}
	a, err1 := provenance.GenerateHTML(bundle)
	b, err2 := provenance.GenerateHTML(bundle)
	if err1 != nil || err2 != nil {
		t.Fatalf("errors: %v %v", err1, err2)
	}
	if string(a) != string(b) {
		t.Error("GenerateHTML is not idempotent")
	}
}
