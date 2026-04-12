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
	"os"

	"github.com/BubbleFish-Nexus/internal/provenance"
)

// runVerify executes `bubblefish verify <proof-file.json>`.
// It performs standalone cryptographic verification of a proof bundle
// without requiring a running daemon.
//
// Exit codes:
//
//	0 = valid
//	1 = invalid (signature, chain, or daemon identity failure)
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.6.
func runVerify(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish verify <proof-file.json>")
		os.Exit(1)
	}

	path := args[0]
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish verify: read %s: %v\n", path, err)
		os.Exit(1)
	}

	var bundle provenance.ProofBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish verify: parse proof bundle: %v\n", err)
		os.Exit(1)
	}

	result := provenance.VerifyProofBundle(&bundle)

	// Print result as JSON.
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))

	if result.Valid {
		fmt.Println("\nbubblefish verify: VALID")
		os.Exit(0)
	}

	fmt.Printf("\nbubblefish verify: INVALID — %s: %s\n", result.ErrorCode, result.ErrorMessage)
	os.Exit(1)
}
