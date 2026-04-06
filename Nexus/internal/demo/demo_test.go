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

package demo

import (
	"testing"
	"time"
)

func TestAnalyse(t *testing.T) {
	t.Helper()

	type record = struct {
		PayloadID      string `json:"payload_id"`
		IdempotencyKey string `json:"idempotency_key"`
	}

	tests := []struct {
		name            string
		results         []record
		expectedIDs     map[string]string
		wantPass        bool
		wantRecovered   int
		wantDuplicates  int
		wantMissingLen  int
	}{
		{
			name: "perfect recovery — 50/50, 0 duplicates",
			results: func() []record {
				recs := make([]record, 50)
				for i := range recs {
					recs[i] = record{
						PayloadID:      fakeID(i),
						IdempotencyKey: fakeKey(i),
					}
				}
				return recs
			}(),
			expectedIDs: func() map[string]string {
				m := make(map[string]string, 50)
				for i := 0; i < 50; i++ {
					m[fakeKey(i)] = fakeID(i)
				}
				return m
			}(),
			wantPass:       true,
			wantRecovered:  50,
			wantDuplicates: 0,
			wantMissingLen: 0,
		},
		{
			name: "missing 5 records",
			results: func() []record {
				recs := make([]record, 45)
				for i := range recs {
					recs[i] = record{
						PayloadID:      fakeID(i),
						IdempotencyKey: fakeKey(i),
					}
				}
				return recs
			}(),
			expectedIDs: func() map[string]string {
				m := make(map[string]string, 50)
				for i := 0; i < 50; i++ {
					m[fakeKey(i)] = fakeID(i)
				}
				return m
			}(),
			wantPass:       false,
			wantRecovered:  45,
			wantDuplicates: 0,
			wantMissingLen: 5,
		},
		{
			name: "duplicate payload_ids detected",
			results: func() []record {
				recs := make([]record, 50)
				for i := range recs {
					recs[i] = record{
						PayloadID:      fakeID(i),
						IdempotencyKey: fakeKey(i),
					}
				}
				// Add a duplicate (same payload_id, same key).
				recs = append(recs, record{
					PayloadID:      fakeID(0),
					IdempotencyKey: fakeKey(0),
				})
				return recs
			}(),
			expectedIDs: func() map[string]string {
				m := make(map[string]string, 50)
				for i := 0; i < 50; i++ {
					m[fakeKey(i)] = fakeID(i)
				}
				return m
			}(),
			wantPass:       false,
			wantRecovered:  50,
			wantDuplicates: 1,
			wantMissingLen: 0,
		},
		{
			name:    "empty results — total loss",
			results: []record{},
			expectedIDs: func() map[string]string {
				m := make(map[string]string, 50)
				for i := 0; i < 50; i++ {
					m[fakeKey(i)] = fakeID(i)
				}
				return m
			}(),
			wantPass:       false,
			wantRecovered:  0,
			wantDuplicates: 0,
			wantMissingLen: 50,
		},
		{
			name: "extra non-demo records ignored",
			results: func() []record {
				recs := make([]record, 50)
				for i := range recs {
					recs[i] = record{
						PayloadID:      fakeID(i),
						IdempotencyKey: fakeKey(i),
					}
				}
				// Extra non-demo record.
				recs = append(recs, record{
					PayloadID:      "extra-payload-999",
					IdempotencyKey: "unrelated-key",
				})
				return recs
			}(),
			expectedIDs: func() map[string]string {
				m := make(map[string]string, 50)
				for i := 0; i < 50; i++ {
					m[fakeKey(i)] = fakeID(i)
				}
				return m
			}(),
			wantPass:       true,
			wantRecovered:  50,
			wantDuplicates: 0,
			wantMissingLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Analyse(tt.results, tt.expectedIDs)

			if result.Pass != tt.wantPass {
				t.Errorf("Pass = %v, want %v", result.Pass, tt.wantPass)
			}
			if result.TotalRecovered != tt.wantRecovered {
				t.Errorf("TotalRecovered = %d, want %d", result.TotalRecovered, tt.wantRecovered)
			}
			if result.Duplicates != tt.wantDuplicates {
				t.Errorf("Duplicates = %d, want %d", result.Duplicates, tt.wantDuplicates)
			}
			if len(result.MissingKeys) != tt.wantMissingLen {
				t.Errorf("len(MissingKeys) = %d, want %d", len(result.MissingKeys), tt.wantMissingLen)
			}
			if result.TotalWritten != len(tt.expectedIDs) {
				t.Errorf("TotalWritten = %d, want %d", result.TotalWritten, len(tt.expectedIDs))
			}
		})
	}
}

func TestWaitReady_InvalidURL(t *testing.T) {
	// waitReady against a non-listening port should time out quickly.
	err := waitReady("http://127.0.0.1:1", 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected error from waitReady on closed port, got nil")
	}
}

// fakeID generates a deterministic payload ID for test index i.
func fakeID(i int) string {
	return fmtID("payload", i)
}

// fakeKey generates a deterministic idempotency key for test index i.
func fakeKey(i int) string {
	return fmtID("demo", i)
}

func fmtID(prefix string, i int) string {
	return prefix + "-" + padInt(i+1)
}

func padInt(n int) string {
	s := ""
	if n < 100 {
		s += "0"
	}
	if n < 10 {
		s += "0"
	}
	return s + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
