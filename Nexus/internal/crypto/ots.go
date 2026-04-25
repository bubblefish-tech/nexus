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
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// OTS file format magic bytes: "\x00OpenTimestamps\x00\x00Proof\x00\xbf\x89\xe2\xe8\x84\xe8\x92\x94"
// Simplified: we store a header + calendar server response as the .ots proof.
var otsMagic = []byte{0x00, 0x4f, 0x70, 0x65, 0x6e, 0x54, 0x69, 0x6d,
	0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x73, 0x00, 0x00, 0x50, 0x72,
	0x6f, 0x6f, 0x66, 0x00, 0xbf, 0x89, 0xe2, 0xe8, 0x84, 0xe8, 0x92, 0x94}

// DefaultCalendarServers is the list of public OTS calendar servers.
var DefaultCalendarServers = []string{
	"https://a.pool.opentimestamps.org",
	"https://b.pool.opentimestamps.org",
	"https://a.pool.eternitywall.com",
}

// OTSClient submits Merkle roots to OpenTimestamps calendar servers and
// manages .ots proof files. This is an optional feature: failures are
// non-fatal and logged.
//
// Reference: Tech Spec MT.12 — OpenTimestamps Bitcoin Anchoring.
type OTSClient struct {
	// CalendarServers is the list of calendar server URLs to try.
	CalendarServers []string

	// ProofDir is the directory where .ots proof files are stored.
	ProofDir string

	// HTTPClient is the HTTP client used for calendar requests. If nil,
	// a default client with a 30-second timeout is used.
	HTTPClient HTTPDoer

	// Timeout is the per-request timeout for calendar submissions.
	// Zero means 30 seconds.
	Timeout time.Duration
}

// HTTPDoer is the interface satisfied by *http.Client. Allows test injection.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Errors returned by OTS operations.
var (
	ErrOTSNoServers   = errors.New("crypto: ots: no calendar servers configured")
	ErrOTSAllFailed   = errors.New("crypto: ots: all calendar servers failed")
	ErrOTSBadDigest   = errors.New("crypto: ots: digest must be 32 bytes")
	ErrOTSNoProofDir  = errors.New("crypto: ots: proof directory not set")
	ErrOTSBadProof    = errors.New("crypto: ots: invalid proof file (bad magic)")
	ErrOTSProofNotFound = errors.New("crypto: ots: proof file not found")
)

// AnchorResult holds the result of an OTS anchoring operation.
type AnchorResult struct {
	// DigestHex is the hex-encoded digest that was anchored.
	DigestHex string `json:"digest_hex"`

	// CalendarServer is the URL of the calendar server that accepted.
	CalendarServer string `json:"calendar_server"`

	// ProofPath is the filesystem path to the .ots proof file.
	ProofPath string `json:"proof_path"`

	// Timestamp is when the anchor was submitted.
	Timestamp time.Time `json:"timestamp"`
}

// NewOTSClient creates an OTSClient with sensible defaults.
func NewOTSClient(proofDir string) *OTSClient {
	return &OTSClient{
		CalendarServers: DefaultCalendarServers,
		ProofDir:        proofDir,
		Timeout:         30 * time.Second,
	}
}

func (c *OTSClient) httpClient() HTTPDoer {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: c.timeout()}
}

func (c *OTSClient) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 30 * time.Second
}

// AnchorRoot submits a 32-byte Merkle root digest to OTS calendar servers.
// It tries each server in order and returns on the first success. The
// calendar response is stored as a .ots proof file in ProofDir.
//
// This is an optional, best-effort operation. Callers should log but not
// fail on errors.
func (c *OTSClient) AnchorRoot(ctx context.Context, digest [32]byte) (*AnchorResult, error) {
	if len(c.CalendarServers) == 0 {
		return nil, ErrOTSNoServers
	}
	if c.ProofDir == "" {
		return nil, ErrOTSNoProofDir
	}

	client := c.httpClient()
	digestHex := hex.EncodeToString(digest[:])

	var lastErr error
	for _, server := range c.CalendarServers {
		url := server + "/digest"

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(digest[:]))
		if err != nil {
			lastErr = fmt.Errorf("create request for %s: %w", server, err)
			continue
		}
		req.Header.Set("Content-Type", "application/x-opentimestamps-digest")
		req.Header.Set("Accept", "application/x-opentimestamps-proof")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("submit to %s: %w", server, err)
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB max
		resp.Body.Close()

		if err != nil {
			lastErr = fmt.Errorf("read response from %s: %w", server, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("server %s returned %d", server, resp.StatusCode)
			continue
		}

		// Build the .ots proof file: magic + digest + calendar response.
		proof := buildOTSProof(digest[:], body)

		// Write proof file.
		proofPath, err := c.writeProof(digestHex, proof)
		if err != nil {
			return nil, fmt.Errorf("crypto: ots: write proof: %w", err)
		}

		return &AnchorResult{
			DigestHex:      digestHex,
			CalendarServer: server,
			ProofPath:      proofPath,
			Timestamp:      time.Now().UTC(),
		}, nil
	}

	return nil, fmt.Errorf("%w: %v", ErrOTSAllFailed, lastErr)
}

// VerifyAnchor reads a .ots proof file and verifies that it contains a valid
// proof for the given digest. This performs structural validation only (magic
// bytes + digest match). Full Bitcoin verification requires a Bitcoin node
// and is out of scope for the community edition.
func (c *OTSClient) VerifyAnchor(digest [32]byte) error {
	if c.ProofDir == "" {
		return ErrOTSNoProofDir
	}

	digestHex := hex.EncodeToString(digest[:])
	proofPath := filepath.Join(c.ProofDir, digestHex+".ots")

	data, err := os.ReadFile(proofPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrOTSProofNotFound
		}
		return fmt.Errorf("crypto: ots: read proof: %w", err)
	}

	return validateOTSProof(data, digest[:])
}

// ProofPath returns the expected filesystem path for a proof file given a
// hex-encoded digest.
func (c *OTSClient) ProofPath(digestHex string) string {
	return filepath.Join(c.ProofDir, digestHex+".ots")
}

// writeProof writes a .ots proof file to ProofDir. The file is named
// <digestHex>.ots and written with mode 0600.
func (c *OTSClient) writeProof(digestHex string, proof []byte) (string, error) {
	if err := os.MkdirAll(c.ProofDir, 0700); err != nil {
		return "", fmt.Errorf("create proof dir: %w", err)
	}
	path := filepath.Join(c.ProofDir, digestHex+".ots")
	if err := os.WriteFile(path, proof, 0600); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return path, nil
}

// buildOTSProof constructs an OTS proof file from the digest and calendar
// server response: magic (31 bytes) + digest (32 bytes) + response.
func buildOTSProof(digest, calendarResponse []byte) []byte {
	buf := make([]byte, 0, len(otsMagic)+len(digest)+len(calendarResponse))
	buf = append(buf, otsMagic...)
	buf = append(buf, digest...)
	buf = append(buf, calendarResponse...)
	return buf
}

// validateOTSProof checks that a proof file has the correct magic bytes and
// contains the expected digest.
func validateOTSProof(data, expectedDigest []byte) error {
	if len(data) < len(otsMagic)+32 {
		return ErrOTSBadProof
	}
	if !bytes.Equal(data[:len(otsMagic)], otsMagic) {
		return ErrOTSBadProof
	}
	storedDigest := data[len(otsMagic) : len(otsMagic)+32]
	if !bytes.Equal(storedDigest, expectedDigest) {
		return fmt.Errorf("%w: digest mismatch", ErrOTSBadProof)
	}
	return nil
}
