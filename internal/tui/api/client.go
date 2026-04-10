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

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client is an HTTP client for the Nexus admin API.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// NewClient creates a new admin API client.
func NewClient(base, token string) *Client {
	return &Client{
		base:  base,
		token: token,
		http:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Status fetches GET /api/status.
func (c *Client) Status() (*StatusResponse, error) {
	var r StatusResponse
	return &r, c.get("/api/status", &r)
}

// Health fetches GET /health.
func (c *Client) Health() (bool, error) {
	var r HealthResponse
	if err := c.get("/health", &r); err != nil {
		return false, err
	}
	return r.Status == "ok", nil
}

// Ready fetches GET /ready.
func (c *Client) Ready() (bool, error) {
	var r HealthResponse
	if err := c.get("/ready", &r); err != nil {
		return false, err
	}
	return r.Status == "ready", nil
}

// Lint fetches GET /api/lint.
func (c *Client) Lint() (*LintResponse, error) {
	var r LintResponse
	return &r, c.get("/api/lint", &r)
}

// SecurityEvents fetches GET /api/security/events.
func (c *Client) SecurityEvents(limit int) (*SecurityEventsResponse, error) {
	var r SecurityEventsResponse
	return &r, c.get("/api/security/events?limit="+strconv.Itoa(limit), &r)
}

// SecuritySummary fetches GET /api/security/summary.
func (c *Client) SecuritySummary() (*SecuritySummaryResponse, error) {
	var r SecuritySummaryResponse
	return &r, c.get("/api/security/summary", &r)
}

// Conflicts fetches GET /api/conflicts.
func (c *Client) Conflicts(opts ConflictOpts) (*ConflictsResponse, error) {
	var r ConflictsResponse
	q := url.Values{}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	path := "/api/conflicts"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return &r, c.get(path, &r)
}

// TimeTravel fetches GET /api/timetravel.
func (c *Client) TimeTravel(opts TimeTravelOpts) (*TimeTravelResponse, error) {
	var r TimeTravelResponse
	q := url.Values{}
	if opts.AsOf != "" {
		q.Set("as_of", opts.AsOf)
	}
	if opts.Subject != "" {
		q.Set("subject", opts.Subject)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	path := "/api/timetravel"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return &r, c.get(path, &r)
}

// AuditLog fetches GET /api/audit/log.
func (c *Client) AuditLog(limit int) (*AuditResponse, error) {
	var r AuditResponse
	return &r, c.get("/api/audit/log?limit="+strconv.Itoa(limit), &r)
}

// Config fetches GET /api/config.
func (c *Client) Config() (*ConfigResponse, error) {
	var r ConfigResponse
	return &r, c.get("/api/config", &r)
}
