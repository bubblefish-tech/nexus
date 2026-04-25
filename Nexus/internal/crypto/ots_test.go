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

package crypto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// mockHTTPDoer is a test double for HTTPDoer.
type mockHTTPDoer struct {
	responses []*http.Response
	errors    []error
	calls     int
}

func (m *mockHTTPDoer) Do(_ *http.Request) (*http.Response, error) {
	idx := m.calls
	m.calls++
	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return nil, fmt.Errorf("no mock response for call %d", idx)
}

func okResponse(body []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func errResponse(code int) *http.Response {
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(bytes.NewReader(nil)),
	}
}

func testDigest() [32]byte {
	var d [32]byte
	for i := range d {
		d[i] = byte(i)
	}
	return d
}

func TestAnchorRoot_Success(t *testing.T) {
	dir := t.TempDir()
	calendarBody := []byte("calendar-proof-data")

	client := &OTSClient{
		CalendarServers: []string{"https://test.example.com"},
		ProofDir:        dir,
		HTTPClient: &mockHTTPDoer{
			responses: []*http.Response{okResponse(calendarBody)},
			errors:    []error{nil},
		},
	}

	digest := testDigest()
	result, err := client.AnchorRoot(context.Background(), digest)
	if err != nil {
		t.Fatalf("AnchorRoot: %v", err)
	}
	if result.CalendarServer != "https://test.example.com" {
		t.Errorf("calendar_server = %q", result.CalendarServer)
	}
	if result.ProofPath == "" {
		t.Fatal("proof_path is empty")
	}

	// Verify the proof file exists and is valid.
	if err := client.VerifyAnchor(digest); err != nil {
		t.Fatalf("VerifyAnchor: %v", err)
	}
}

func TestAnchorRoot_FirstServerFails_FallsThrough(t *testing.T) {
	dir := t.TempDir()

	client := &OTSClient{
		CalendarServers: []string{"https://bad.example.com", "https://good.example.com"},
		ProofDir:        dir,
		HTTPClient: &mockHTTPDoer{
			responses: []*http.Response{nil, okResponse([]byte("proof"))},
			errors:    []error{fmt.Errorf("connection refused"), nil},
		},
	}

	result, err := client.AnchorRoot(context.Background(), testDigest())
	if err != nil {
		t.Fatalf("AnchorRoot: %v", err)
	}
	if result.CalendarServer != "https://good.example.com" {
		t.Errorf("expected fallback server, got %q", result.CalendarServer)
	}
}

func TestAnchorRoot_AllServersFail(t *testing.T) {
	dir := t.TempDir()

	client := &OTSClient{
		CalendarServers: []string{"https://a.example.com", "https://b.example.com"},
		ProofDir:        dir,
		HTTPClient: &mockHTTPDoer{
			errors: []error{fmt.Errorf("fail-a"), fmt.Errorf("fail-b")},
		},
	}

	_, err := client.AnchorRoot(context.Background(), testDigest())
	if !errors.Is(err, ErrOTSAllFailed) {
		t.Fatalf("expected ErrOTSAllFailed, got %v", err)
	}
}

func TestAnchorRoot_NoServers(t *testing.T) {
	client := &OTSClient{
		CalendarServers: nil,
		ProofDir:        t.TempDir(),
	}

	_, err := client.AnchorRoot(context.Background(), testDigest())
	if !errors.Is(err, ErrOTSNoServers) {
		t.Fatalf("expected ErrOTSNoServers, got %v", err)
	}
}

func TestAnchorRoot_NoProofDir(t *testing.T) {
	client := &OTSClient{
		CalendarServers: DefaultCalendarServers,
		ProofDir:        "",
	}

	_, err := client.AnchorRoot(context.Background(), testDigest())
	if !errors.Is(err, ErrOTSNoProofDir) {
		t.Fatalf("expected ErrOTSNoProofDir, got %v", err)
	}
}

func TestAnchorRoot_ServerReturns500(t *testing.T) {
	dir := t.TempDir()

	client := &OTSClient{
		CalendarServers: []string{"https://a.example.com"},
		ProofDir:        dir,
		HTTPClient: &mockHTTPDoer{
			responses: []*http.Response{errResponse(500)},
			errors:    []error{nil},
		},
	}

	_, err := client.AnchorRoot(context.Background(), testDigest())
	if !errors.Is(err, ErrOTSAllFailed) {
		t.Fatalf("expected ErrOTSAllFailed for 500, got %v", err)
	}
}

func TestVerifyAnchor_ProofNotFound(t *testing.T) {
	client := &OTSClient{ProofDir: t.TempDir()}
	err := client.VerifyAnchor(testDigest())
	if !errors.Is(err, ErrOTSProofNotFound) {
		t.Fatalf("expected ErrOTSProofNotFound, got %v", err)
	}
}

func TestVerifyAnchor_BadMagic(t *testing.T) {
	dir := t.TempDir()
	digest := testDigest()

	// Write a file with bad magic bytes.
	badProof := make([]byte, 100)
	copy(badProof, []byte("NOT-OTS-MAGIC"))
	path := filepath.Join(dir, fmt.Sprintf("%x.ots", digest))
	if err := os.WriteFile(path, badProof, 0600); err != nil {
		t.Fatal(err)
	}

	client := &OTSClient{ProofDir: dir}
	err := client.VerifyAnchor(digest)
	if !errors.Is(err, ErrOTSBadProof) {
		t.Fatalf("expected ErrOTSBadProof, got %v", err)
	}
}

func TestVerifyAnchor_DigestMismatch(t *testing.T) {
	dir := t.TempDir()
	digest := testDigest()

	// Write a valid magic but wrong digest.
	wrongDigest := [32]byte{0xff}
	proof := buildOTSProof(wrongDigest[:], []byte("response"))
	path := filepath.Join(dir, fmt.Sprintf("%x.ots", digest))
	if err := os.WriteFile(path, proof, 0600); err != nil {
		t.Fatal(err)
	}

	client := &OTSClient{ProofDir: dir}
	err := client.VerifyAnchor(digest)
	if !errors.Is(err, ErrOTSBadProof) {
		t.Fatalf("expected ErrOTSBadProof for digest mismatch, got %v", err)
	}
}

func TestVerifyAnchor_NoProofDir(t *testing.T) {
	client := &OTSClient{ProofDir: ""}
	err := client.VerifyAnchor(testDigest())
	if !errors.Is(err, ErrOTSNoProofDir) {
		t.Fatalf("expected ErrOTSNoProofDir, got %v", err)
	}
}

func TestVerifyAnchor_TruncatedProof(t *testing.T) {
	dir := t.TempDir()
	digest := testDigest()

	// Write a truncated file (shorter than magic + digest).
	path := filepath.Join(dir, fmt.Sprintf("%x.ots", digest))
	if err := os.WriteFile(path, otsMagic[:10], 0600); err != nil {
		t.Fatal(err)
	}

	client := &OTSClient{ProofDir: dir}
	err := client.VerifyAnchor(digest)
	if !errors.Is(err, ErrOTSBadProof) {
		t.Fatalf("expected ErrOTSBadProof for truncated proof, got %v", err)
	}
}

func TestNewOTSClient_Defaults(t *testing.T) {
	c := NewOTSClient("/tmp/proofs")
	if c.ProofDir != "/tmp/proofs" {
		t.Errorf("proof_dir = %q", c.ProofDir)
	}
	if len(c.CalendarServers) != len(DefaultCalendarServers) {
		t.Errorf("calendar servers count = %d, want %d", len(c.CalendarServers), len(DefaultCalendarServers))
	}
}

func TestProofPath(t *testing.T) {
	c := &OTSClient{ProofDir: "/data/proofs"}
	expected := filepath.Join("/data/proofs", "abcdef.ots")
	if got := c.ProofPath("abcdef"); got != expected {
		t.Errorf("ProofPath = %q, want %q", got, expected)
	}
}

func TestBuildOTSProof_Structure(t *testing.T) {
	digest := testDigest()
	response := []byte("calendar-response")
	proof := buildOTSProof(digest[:], response)

	if len(proof) != len(otsMagic)+32+len(response) {
		t.Fatalf("proof length = %d, expected %d", len(proof), len(otsMagic)+32+len(response))
	}
	if !bytes.Equal(proof[:len(otsMagic)], otsMagic) {
		t.Error("proof missing OTS magic bytes")
	}
	if !bytes.Equal(proof[len(otsMagic):len(otsMagic)+32], digest[:]) {
		t.Error("proof missing digest")
	}
	if !bytes.Equal(proof[len(otsMagic)+32:], response) {
		t.Error("proof missing calendar response")
	}
}
