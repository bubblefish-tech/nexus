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
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAPIURL = "http://localhost:8080"
	EnvAPIURL     = "NEXUS_API_URL"
	EnvAdminToken = "NEXUS_ADMIN_TOKEN"
)

// ResolveBaseURL returns the API base URL using this priority:
//
//  1. cliFlag (from --api-url flag)
//  2. NEXUS_API_URL environment variable
//  3. DefaultAPIURL
func ResolveBaseURL(cliFlag string) string {
	if cliFlag != "" {
		return cliFlag
	}
	if v := os.Getenv(EnvAPIURL); v != "" {
		return v
	}
	return DefaultAPIURL
}

// ResolveAdminToken returns the admin bearer token using this priority:
//
//  1. cliFlag (from --admin-token flag)
//  2. NEXUS_ADMIN_TOKEN environment variable
//  3. "" (empty — no token; /api/* calls will 401)
func ResolveAdminToken(cliFlag string) string {
	if cliFlag != "" {
		return cliFlag
	}
	return os.Getenv(EnvAdminToken)
}

// Client is an HTTP client for the Nexus admin API.
type Client struct {
	base   string
	token  string
	http   *http.Client
	ctx    context.Context
	cancel context.CancelFunc
}

// NewClient creates a new admin API client.
func NewClient(base, token string) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		base:   base,
		token:  token,
		http:   &http.Client{Timeout: 5 * time.Second},
		ctx:    ctx,
		cancel: cancel,
	}
}

// Close cancels all in-flight requests.
func (c *Client) Close() {
	c.cancel()
}

// HasToken reports whether the client was configured with a bearer token.
func (c *Client) HasToken() bool { return c.token != "" }

// addAuth attaches the Bearer token to requests on /api/* paths only.
// SSE /stream/* and unauthenticated probe endpoints do not receive the header.
func (c *Client) addAuth(req *http.Request) {
	if c.token != "" && strings.HasPrefix(req.URL.Path, "/api/") {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func (c *Client) get(path string, out any) error {
	req, err := http.NewRequestWithContext(c.ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	c.addAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &HTTPError{Status: resp.StatusCode, Path: path}
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

// Agents fetches GET /api/control/agents.
func (c *Client) Agents() ([]AgentSummary, error) {
	var r AgentsResponse
	if err := c.get("/api/control/agents", &r); err != nil {
		return nil, err
	}
	return r.Agents, nil
}

// QuarantineList fetches GET /api/quarantine.
func (c *Client) QuarantineList(limit int) (*QuarantineResponse, error) {
	var r QuarantineResponse
	return &r, c.get("/api/quarantine?limit="+strconv.Itoa(limit), &r)
}

// QuarantineCount fetches GET /api/quarantine/count.
func (c *Client) QuarantineCount() (*QuarantineCountResponse, error) {
	var r QuarantineCountResponse
	return &r, c.get("/api/quarantine/count", &r)
}

// Grants fetches GET /api/control/grants.
func (c *Client) Grants() (*GrantsResponse, error) {
	var r GrantsResponse
	return &r, c.get("/api/control/grants", &r)
}

// Approvals fetches GET /api/control/approvals.
func (c *Client) Approvals() (*ApprovalsResponse, error) {
	var r ApprovalsResponse
	return &r, c.get("/api/control/approvals", &r)
}

// Tasks fetches GET /api/control/tasks.
func (c *Client) Tasks() (*TasksResponse, error) {
	var r TasksResponse
	return &r, c.get("/api/control/tasks", &r)
}

// ListMemories returns the most recent memories via GET /api/memories.
// This is the default listing endpoint; time-travel uses a separate path.
func (c *Client) ListMemories(limit, offset int) (*MemoryListResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	path := "/api/memories?limit=" + strconv.Itoa(limit)
	var r MemoryListResponse
	return &r, c.get(path, &r)
}

// SearchMemories performs a query across all memories via GET /api/memories?q=...
func (c *Client) SearchMemories(query string, limit int) (*MemoryListResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	path := "/api/memories?q=" + url.QueryEscape(query) + "&limit=" + strconv.Itoa(limit)
	var r MemoryListResponse
	return &r, c.get(path, &r)
}

// GetMemory fetches a single memory by ID via GET /api/memories/{id}.
func (c *Client) GetMemory(id string) (*MemoryDetail, error) {
	var r MemoryDetail
	return &r, c.get("/api/memories/"+url.PathEscape(id), &r)
}

// Stats fetches GET /api/stats — aggregated dashboard statistics.
func (c *Client) Stats() (*AggregatedStats, error) {
	var r AggregatedStats
	return &r, c.get("/api/stats", &r)
}

// SigningStatus fetches GET /api/crypto/signing.
func (c *Client) SigningStatus() (*SigningStatus, error) {
	var r SigningStatus
	return &r, c.get("/api/crypto/signing", &r)
}

// CryptoProfileStatus fetches GET /api/crypto/profile.
func (c *Client) CryptoProfileStatus() (*CryptoProfile, error) {
	var r CryptoProfile
	return &r, c.get("/api/crypto/profile", &r)
}

// MasterKeyStatusInfo fetches GET /api/crypto/master.
func (c *Client) MasterKeyStatusInfo() (*MasterKeyStatus, error) {
	var r MasterKeyStatus
	return &r, c.get("/api/crypto/master", &r)
}

// RatchetStatusInfo fetches GET /api/crypto/ratchet.
func (c *Client) RatchetStatusInfo() (*RatchetStatus, error) {
	var r RatchetStatus
	return &r, c.get("/api/crypto/ratchet", &r)
}
