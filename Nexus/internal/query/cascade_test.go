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

package query_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/query"
)

// ---------------------------------------------------------------------------
// mockQuerier — controllable test double for destination.Querier
// ---------------------------------------------------------------------------

// mockQuerier records the last QueryParams it received and returns a fixed
// result. It is NOT thread-safe; create one per test.
type mockQuerier struct {
	result     destination.QueryResult
	err        error
	lastParams destination.QueryParams
}

func (m *mockQuerier) Query(params destination.QueryParams) (destination.QueryResult, error) {
	m.lastParams = params
	return m.result, m.err
}

// callDetector wraps a Querier and records whether Query was called.
type callDetector struct {
	inner  destination.Querier
	called *bool
}

func (c *callDetector) Query(p destination.QueryParams) (destination.QueryResult, error) {
	*c.called = true
	return c.inner.Query(p)
}

// ---------------------------------------------------------------------------
// Helper constructors
// ---------------------------------------------------------------------------

func makeRecord(id, content string) destination.TranslatedPayload {
	return destination.TranslatedPayload{
		PayloadID: id,
		Content:   content,
		Timestamp: time.Now().UTC(),
	}
}

func defaultQuery() query.CanonicalQuery {
	return query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "testns",
		Profile:     "balanced",
		Limit:       20,
	}
}

// ---------------------------------------------------------------------------
// Stage 0 — Policy Gate: CanRead=false denied
// ---------------------------------------------------------------------------

func TestCascade_Stage0_CanReadFalse_Denied(t *testing.T) {
	mq := &mockQuerier{result: destination.QueryResult{Records: []destination.TranslatedPayload{}}}
	runner := query.New(mq, nil)
	src := &config.Source{Name: "s", Namespace: "ns", CanRead: false}

	result, err := runner.Run(context.Background(), src, defaultQuery())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Denial == nil {
		t.Fatal("expected Denial, got nil")
	}
	if result.Denial.Code != "source_not_permitted_to_read" {
		t.Errorf("Denial.Code = %q; want source_not_permitted_to_read", result.Denial.Code)
	}
	// Stage 3 must NOT have been called when Stage 0 denies.
	if mq.lastParams.Destination != "" {
		t.Error("querier was called despite Stage 0 denial")
	}
}

// ---------------------------------------------------------------------------
// Stage 0 — Policy Gate: table-driven checks
// ---------------------------------------------------------------------------

func TestCascade_Stage0_PolicyGate_TableDriven(t *testing.T) {
	cases := []struct {
		name     string
		canRead  bool
		dests    []string
		ops      []string
		modes    []string
		destName string
		profile  string
		wantCode string // empty = no denial expected
	}{
		{
			name:     "can_read false",
			canRead:  false,
			wantCode: "source_not_permitted_to_read",
		},
		{
			name:     "destination not in allowed list",
			canRead:  true,
			dests:    []string{"postgres"},
			destName: "sqlite",
			wantCode: "destination_not_allowed",
		},
		{
			name:     "read not in allowed operations",
			canRead:  true,
			ops:      []string{"write"},
			wantCode: "operation_not_allowed",
		},
		{
			name:     "profile not in allowed retrieval modes",
			canRead:  true,
			modes:    []string{"fast"},
			profile:  "deep",
			wantCode: "retrieval_mode_not_allowed",
		},
		{
			name:     "all allowed — no denial",
			canRead:  true,
			dests:    []string{"sqlite"},
			ops:      []string{"write", "read"},
			modes:    []string{"balanced", "fast"},
			wantCode: "",
		},
		{
			name:     "empty policy lists — no denial",
			canRead:  true,
			wantCode: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			mq := &mockQuerier{result: destination.QueryResult{Records: []destination.TranslatedPayload{}}}
			runner := query.New(mq, nil)

			src := &config.Source{
				Name:      "s",
				Namespace: "ns",
				CanRead:   tc.canRead,
				Policy: config.SourcePolicyConfig{
					AllowedDestinations:   tc.dests,
					AllowedOperations:     tc.ops,
					AllowedRetrievalModes: tc.modes,
				},
			}

			destName := tc.destName
			if destName == "" {
				destName = "sqlite"
			}
			profile := tc.profile
			if profile == "" {
				profile = "balanced"
			}
			q := query.CanonicalQuery{
				Destination: destName,
				Namespace:   "ns",
				Profile:     profile,
				Limit:       20,
			}

			result, err := runner.Run(context.Background(), src, q)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantCode == "" {
				if result.Denial != nil {
					t.Errorf("expected no denial, got %q", result.Denial.Code)
				}
			} else {
				if result.Denial == nil {
					t.Fatal("expected Denial, got nil")
				}
				if result.Denial.Code != tc.wantCode {
					t.Errorf("Denial.Code = %q; want %q", result.Denial.Code, tc.wantCode)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Stage 3 — Structured Lookup
// ---------------------------------------------------------------------------

func TestCascade_Stage3_StructuredLookup_ReturnsRecords(t *testing.T) {
	want := []destination.TranslatedPayload{
		makeRecord("id-1", "first memory"),
		makeRecord("id-2", "second memory"),
	}
	mq := &mockQuerier{
		result: destination.QueryResult{
			Records:    want,
			HasMore:    false,
			NextCursor: "",
		},
	}
	runner := query.New(mq, nil)
	src := &config.Source{Name: "s", Namespace: "ns", CanRead: true}

	q := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "ns",
		Subject:     "user:42",
		Profile:     "fast",
		Limit:       10,
	}

	result, err := runner.Run(context.Background(), src, q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Denial != nil {
		t.Fatalf("unexpected denial: %v", result.Denial)
	}
	if len(result.Records) != 2 {
		t.Fatalf("len(Records) = %d; want 2", len(result.Records))
	}
	if result.Records[0].PayloadID != "id-1" {
		t.Errorf("Records[0].PayloadID = %q; want id-1", result.Records[0].PayloadID)
	}
	if result.Records[1].PayloadID != "id-2" {
		t.Errorf("Records[1].PayloadID = %q; want id-2", result.Records[1].PayloadID)
	}
	if result.RetrievalStage != 3 {
		t.Errorf("RetrievalStage = %d; want 3", result.RetrievalStage)
	}

	// Verify querier received correct (parameterized) arguments.
	if mq.lastParams.Destination != "sqlite" {
		t.Errorf("querier Destination = %q; want sqlite", mq.lastParams.Destination)
	}
	if mq.lastParams.Subject != "user:42" {
		t.Errorf("querier Subject = %q; want user:42", mq.lastParams.Subject)
	}
	if mq.lastParams.Limit != 10 {
		t.Errorf("querier Limit = %d; want 10", mq.lastParams.Limit)
	}
}

func TestCascade_Stage3_ProfileForwarded(t *testing.T) {
	mq := &mockQuerier{result: destination.QueryResult{Records: []destination.TranslatedPayload{}}}
	runner := query.New(mq, nil)
	src := &config.Source{Name: "s", Namespace: "ns", CanRead: true}

	q := query.CanonicalQuery{
		Destination: "sqlite",
		Profile:     "deep",
		Limit:       20,
	}
	result, err := runner.Run(context.Background(), src, q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Profile != "deep" {
		t.Errorf("result.Profile = %q; want deep", result.Profile)
	}
	if mq.lastParams.Profile != "deep" {
		t.Errorf("querier Profile = %q; want deep", mq.lastParams.Profile)
	}
}

// ---------------------------------------------------------------------------
// Cursor-based pagination
// ---------------------------------------------------------------------------

func TestCascade_Pagination_FirstPage_ReturnsNextCursor(t *testing.T) {
	records := make([]destination.TranslatedPayload, 20)
	for i := range records {
		records[i] = makeRecord(fmt.Sprintf("id-%d", i+1), "content")
	}

	mq := &mockQuerier{
		result: destination.QueryResult{
			Records:    records,
			HasMore:    true,
			NextCursor: destination.EncodeCursor(20),
		},
	}
	runner := query.New(mq, nil)
	src := &config.Source{Name: "s", Namespace: "ns", CanRead: true}

	q := query.CanonicalQuery{
		Destination:  "sqlite",
		Profile:      "balanced",
		Limit:        20,
		CursorOffset: 0, // first page
	}

	result, err := runner.Run(context.Background(), src, q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasMore {
		t.Error("HasMore = false; want true")
	}
	if result.NextCursor == "" {
		t.Error("NextCursor is empty on first page with more results")
	}

	// Decode the cursor and verify it encodes offset 20.
	offset, err := destination.DecodeCursor(result.NextCursor)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if offset != 20 {
		t.Errorf("decoded cursor offset = %d; want 20", offset)
	}
}

func TestCascade_Pagination_SecondPage_ReceivesCursor(t *testing.T) {
	page2Records := []destination.TranslatedPayload{makeRecord("id-21", "p2")}
	mq := &mockQuerier{
		result: destination.QueryResult{
			Records:    page2Records,
			HasMore:    false,
			NextCursor: "",
		},
	}
	runner := query.New(mq, nil)
	src := &config.Source{Name: "s", Namespace: "ns", CanRead: true}

	// Simulate a client passing the cursor from page 1.
	page1Cursor := destination.EncodeCursor(20)
	offset, err := destination.DecodeCursor(page1Cursor)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}

	q := query.CanonicalQuery{
		Destination:  "sqlite",
		Profile:      "balanced",
		Limit:        20,
		CursorOffset: offset,
		RawCursor:    page1Cursor,
	}

	result, err := runner.Run(context.Background(), src, q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("len(Records) = %d; want 1", len(result.Records))
	}
	if result.HasMore {
		t.Error("HasMore = true; want false on last page")
	}

	// Verify the querier received a cursor representing offset 20.
	decodedOffset, err := destination.DecodeCursor(mq.lastParams.Cursor)
	if err != nil {
		t.Fatalf("querier cursor decode: %v", err)
	}
	if decodedOffset != 20 {
		t.Errorf("querier cursor offset = %d; want 20", decodedOffset)
	}
}

// ---------------------------------------------------------------------------
// Query limit capping via Normalize
// ---------------------------------------------------------------------------

func TestNormalize_LimitCappedAt200(t *testing.T) {
	q, err := query.Normalize(destination.QueryParams{
		Destination: "sqlite",
		Limit:       500, // exceeds MaxQueryLimit
	})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if q.Limit != destination.MaxQueryLimit {
		t.Errorf("Limit = %d; want %d (MaxQueryLimit)", q.Limit, destination.MaxQueryLimit)
	}
}

func TestNormalize_ZeroLimit_DefaultsTo20(t *testing.T) {
	q, err := query.Normalize(destination.QueryParams{
		Destination: "sqlite",
		Limit:       0,
	})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if q.Limit != destination.DefaultQueryLimit {
		t.Errorf("Limit = %d; want %d (DefaultQueryLimit)", q.Limit, destination.DefaultQueryLimit)
	}
}

func TestNormalize_EmptyProfile_DefaultsToBalanced(t *testing.T) {
	q, err := query.Normalize(destination.QueryParams{
		Destination: "sqlite",
		Profile:     "",
	})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if q.Profile != "balanced" {
		t.Errorf("Profile = %q; want balanced", q.Profile)
	}
}

func TestNormalize_InvalidCursor_ReturnsError(t *testing.T) {
	_, err := query.Normalize(destination.QueryParams{
		Destination: "sqlite",
		Cursor:      "!!!notbase64!!!",
	})
	if err == nil {
		t.Fatal("expected error for invalid cursor, got nil")
	}
}

func TestNormalize_ValidCursor_DecodesOffset(t *testing.T) {
	cursor := destination.EncodeCursor(42)
	q, err := query.Normalize(destination.QueryParams{
		Destination: "sqlite",
		Cursor:      cursor,
	})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if q.CursorOffset != 42 {
		t.Errorf("CursorOffset = %d; want 42", q.CursorOffset)
	}
	if q.RawCursor != cursor {
		t.Errorf("RawCursor = %q; want %q", q.RawCursor, cursor)
	}
}

// ---------------------------------------------------------------------------
// Stage execution order — Stage 0 must block before Stage 3 runs
// ---------------------------------------------------------------------------

func TestCascade_StageOrder_PolicyBlocksBeforeQuery(t *testing.T) {
	called := false
	inner := &mockQuerier{result: destination.QueryResult{}}
	runner := query.New(&callDetector{inner: inner, called: &called}, nil)

	src := &config.Source{
		Name:    "s",
		CanRead: false, // Stage 0 must deny
	}

	_, err := runner.Run(context.Background(), src, defaultQuery())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("querier was called after Stage 0 denial — stage execution order violated")
	}
}
