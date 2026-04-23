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

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/provenance"
)

// runVerify executes `nexus verify`.
//
// Modes:
//
//	nexus verify <proof-file.json>                     — verify a local file
//	nexus verify --proof <memory_id> [--url URL]       — fetch proof from daemon
//	nexus verify ... --output <path>                   — write HTML (*.html) or JSON
//
// Exit codes:
//
//	0 = valid
//	1 = invalid (signature, chain, or content-hash failure) or error
func runVerify(args []string) {
	var (
		proofMemoryID string
		outputPath    string
		daemonURL     = "http://localhost:8081"
		adminToken    = os.Getenv("NEXUS_ADMIN_KEY")
		positional    []string
	)

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--proof":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "nexus verify: --proof requires a memory_id argument")
				os.Exit(1)
			}
			proofMemoryID = args[i]
		case "--output":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "nexus verify: --output requires a path argument")
				os.Exit(1)
			}
			outputPath = args[i]
		case "--url":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "nexus verify: --url requires a URL argument")
				os.Exit(1)
			}
			daemonURL = args[i]
		case "--token":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "nexus verify: --token requires a value")
				os.Exit(1)
			}
			adminToken = args[i]
		default:
			if !strings.HasPrefix(args[i], "--") {
				positional = append(positional, args[i])
			}
		}
	}

	if proofMemoryID == "" && len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nexus verify <proof-file.json>")
		fmt.Fprintln(os.Stderr, "       nexus verify --proof <memory_id> [--url URL] [--token TOKEN] [--output proof.html]")
		os.Exit(1)
	}

	var bundle provenance.ProofBundle

	if proofMemoryID != "" {
		// Fetch proof bundle from running daemon.
		b, err := fetchProofFromDaemon(daemonURL, proofMemoryID, adminToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nexus verify: fetch proof: %v\n", err)
			os.Exit(1)
		}
		bundle = *b
	} else {
		// Read from local file.
		data, err := os.ReadFile(positional[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "nexus verify: read %s: %v\n", positional[0], err)
			os.Exit(1)
		}
		if err := json.Unmarshal(data, &bundle); err != nil {
			fmt.Fprintf(os.Stderr, "nexus verify: parse proof bundle: %v\n", err)
			os.Exit(1)
		}
	}

	if outputPath != "" {
		if err := writeVerifyOutput(&bundle, outputPath); err != nil {
			fmt.Fprintf(os.Stderr, "nexus verify: write output: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("nexus verify: written to %s\n", outputPath)
		if strings.HasSuffix(strings.ToLower(outputPath), ".html") {
			fmt.Println("Open in a browser to verify cryptographic proof client-side.")
			return
		}
	}

	result := provenance.VerifyProofBundle(&bundle)

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))

	if result.Valid {
		fmt.Println("\nnexus verify: VALID")
		os.Exit(0)
	}
	fmt.Printf("\nnexus verify: INVALID — %s: %s\n", result.ErrorCode, result.ErrorMessage)
	os.Exit(1)
}

// fetchProofFromDaemon calls GET /verify/{memory_id} on the daemon and parses
// the returned ProofBundle.
func fetchProofFromDaemon(daemonURL, memoryID, token string) (*provenance.ProofBundle, error) {
	url := strings.TrimRight(daemonURL, "/") + "/verify/" + memoryID
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon returned %d: %s", resp.StatusCode, string(body))
	}

	var bundle provenance.ProofBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &bundle, nil
}

// writeVerifyOutput writes the proof bundle to outputPath. If the path ends
// in .html, a self-contained HTML verification page is generated.
// Otherwise, the proof bundle is written as JSON.
func writeVerifyOutput(bundle *provenance.ProofBundle, outputPath string) error {
	var data []byte
	var err error

	if strings.HasSuffix(strings.ToLower(outputPath), ".html") {
		data, err = provenance.GenerateHTML(bundle)
		if err != nil {
			return err
		}
	} else {
		data, err = json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			return err
		}
	}

	dir := filepath.Dir(outputPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	return os.WriteFile(outputPath, data, 0600)
}
