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

package destination

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/nexuserr"
)

type failDest struct{ noopDest }

func (d *failDest) Write(_ TranslatedPayload) error {
	return errors.New("connection refused")
}

type noopDest struct{}

func (d *noopDest) Name() string                                                           { return "noop" }
func (d *noopDest) Write(_ TranslatedPayload) error                                        { return nil }
func (d *noopDest) Read(_ context.Context, _ string) (*Memory, error)                      { return nil, nil }
func (d *noopDest) Search(_ context.Context, _ *Query) ([]*Memory, error)                  { return nil, nil }
func (d *noopDest) Delete(_ context.Context, _ string) error                               { return nil }
func (d *noopDest) VectorSearch(_ context.Context, _ []float32, _ int) ([]*Memory, error)  { return nil, nil }
func (d *noopDest) Migrate(_ context.Context, _ int) error                                 { return nil }
func (d *noopDest) Health(_ context.Context) (*HealthStatus, error)                        { return &HealthStatus{OK: true}, nil }
func (d *noopDest) Close() error                                                           { return nil }

type dupKeyDest struct{ noopDest }

func (d *dupKeyDest) Write(_ TranslatedPayload) error {
	return nexuserr.ErrDuplicateKey
}

func TestBreaker_NormalOperationsPassThrough(t *testing.T) {
	t.Helper()
	bw := NewBreakerWrapper(&noopDest{}, "test", slog.Default())
	if err := bw.Write(TranslatedPayload{}); err != nil {
		t.Errorf("write through breaker failed: %v", err)
	}
}

func TestBreaker_DuplicateKeyDoesNotTrip(t *testing.T) {
	t.Helper()
	bw := NewBreakerWrapper(&dupKeyDest{}, "test", slog.Default())
	for i := 0; i < 10; i++ {
		err := bw.Write(TranslatedPayload{})
		if err == nil {
			t.Fatal("expected error from dupKeyDest")
		}
		if errors.Is(err, nexuserr.ErrCircuitOpen) {
			t.Fatal("circuit breaker tripped on duplicate key — should not count as failure")
		}
	}
}

func TestBreaker_TripsAfterConsecutiveFailures(t *testing.T) {
	t.Helper()
	bw := NewBreakerWrapper(&failDest{}, "test", slog.Default())
	for i := 0; i < 6; i++ {
		_ = bw.Write(TranslatedPayload{})
	}
	err := bw.Write(TranslatedPayload{})
	if !errors.Is(err, nexuserr.ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen after 6 failures, got: %v", err)
	}
}

func TestBreaker_Name(t *testing.T) {
	t.Helper()
	bw := NewBreakerWrapper(&noopDest{}, "sqlite", slog.Default())
	if bw.Name() != "noop" {
		t.Errorf("Name() = %q, want %q", bw.Name(), "noop")
	}
}
