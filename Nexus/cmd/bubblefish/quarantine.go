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
	"net/url"
	"os"
	"text/tabwriter"
	"time"
)

// runQuarantine dispatches the `bubblefish quarantine` subcommands.
func runQuarantine(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish quarantine <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  list              list quarantined records (unreviewed by default)")
		fmt.Fprintln(os.Stderr, "  approve --id <id> approve a quarantined record")
		fmt.Fprintln(os.Stderr, "  reject  --id <id> reject a quarantined record")
		os.Exit(1)
	}
	cl, err := loadControlClient(os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish quarantine: %v\n", err)
		os.Exit(1)
	}
	switch args[0] {
	case "list":
		if err := doQuarantineList(cl, args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish quarantine list: %v\n", err)
			os.Exit(1)
		}
	case "approve":
		if err := doQuarantineDecide(cl, args[1:], "approve"); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish quarantine approve: %v\n", err)
			os.Exit(1)
		}
	case "reject":
		if err := doQuarantineDecide(cl, args[1:], "reject"); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish quarantine reject: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "quarantine: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func doQuarantineList(c *controlClient, args []string) error {
	var sourceFilter, limitStr string
	var asJSON, includeReviewed bool
	if err := parseFlags(args,
		map[string]*string{"source": &sourceFilter, "limit": &limitStr},
		map[string]*bool{"json": &asJSON, "include-reviewed": &includeReviewed},
	); err != nil {
		return err
	}

	params := url.Values{}
	if sourceFilter != "" {
		params.Set("source", url.QueryEscape(sourceFilter))
	}
	if includeReviewed {
		params.Set("include_reviewed", "true")
	}
	if limitStr != "" {
		params.Set("limit", limitStr)
	}

	path := "/api/quarantine"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	resp, err := c.get(path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Records []struct {
			ID                string  `json:"id"`
			OriginalPayloadID string  `json:"original_payload_id"`
			SourceName        string  `json:"source_name"`
			RuleID            string  `json:"rule_id"`
			QuarantineReason  string  `json:"quarantine_reason"`
			QuarantinedAtMs   int64   `json:"quarantined_at_ms"`
			ReviewAction      *string `json:"review_action"`
		} `json:"records"`
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if asJSON {
		enc := json.NewEncoder(c.out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	tw := tabwriter.NewWriter(c.out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "ID\tPayload ID\tSource\tRule\tStatus\tQuarantined At\n")
	for _, r := range result.Records {
		status := "pending"
		if r.ReviewAction != nil {
			status = *r.ReviewAction
		}
		qTime := time.UnixMilli(r.QuarantinedAtMs).UTC().Format(time.RFC3339)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.ID, r.OriginalPayloadID, r.SourceName, r.RuleID, status, qTime)
	}
	_ = tw.Flush()
	fmt.Fprintf(c.out, "\n%d record(s)\n", result.Count)
	return nil
}

func doQuarantineDecide(c *controlClient, args []string, action string) error {
	var id string
	if err := parseFlags(args,
		map[string]*string{"id": &id},
		map[string]*bool{},
	); err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("--id is required")
	}

	resp, err := c.post("/api/quarantine/"+url.PathEscape(id)+"/"+action,
		map[string]string{"reviewed_by": "cli"})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		ID           string `json:"id"`
		ReviewAction string `json:"review_action"`
		ReviewedBy   string `json:"reviewed_by"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Fprintf(c.out, "quarantine record %s: %s by %s\n", result.ID, result.ReviewAction, result.ReviewedBy)
	return nil
}
