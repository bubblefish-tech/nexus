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
	"fmt"
	"log/slog"
	"time"

	"github.com/bubblefish-tech/nexus/internal/nexuserr"
	"github.com/sony/gobreaker/v2"
)

// BreakerWrapper wraps a Destination with a circuit breaker.
type BreakerWrapper struct {
	dest Destination
	cb   *gobreaker.CircuitBreaker[any]
}

// NewBreakerWrapper returns a Destination wrapped with a circuit breaker.
func NewBreakerWrapper(dest Destination, destType string, logger *slog.Logger) *BreakerWrapper {
	settings := gobreaker.Settings{
		Name:        fmt.Sprintf("dest-%s", destType),
		MaxRequests: 3,
		Interval:    60 * time.Second,
		Timeout:     10 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.ConsecutiveFailures)
			return counts.ConsecutiveFailures >= 5 ||
				(counts.Requests >= 10 && failureRatio/float64(counts.Requests) >= 0.6)
		},
		IsSuccessful: func(err error) bool {
			return !nexuserr.IsInfrastructureError(err)
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Warn("circuit breaker state change",
				"component", "destination",
				"breaker", name,
				"from", from.String(),
				"to", to.String(),
			)
		},
	}
	return &BreakerWrapper{
		dest: dest,
		cb:   gobreaker.NewCircuitBreaker[any](settings),
	}
}

func (b *BreakerWrapper) Name() string { return b.dest.Name() }

func (b *BreakerWrapper) Write(p TranslatedPayload) error {
	_, err := b.cb.Execute(func() (any, error) {
		return nil, b.dest.Write(p)
	})
	if err != nil {
		return mapBreakerError(err)
	}
	return nil
}

func (b *BreakerWrapper) Read(ctx context.Context, id string) (*Memory, error) {
	result, err := b.cb.Execute(func() (any, error) {
		return b.dest.Read(ctx, id)
	})
	if err != nil {
		return nil, mapBreakerError(err)
	}
	return result.(*Memory), nil
}

func (b *BreakerWrapper) Search(ctx context.Context, q *Query) ([]*Memory, error) {
	result, err := b.cb.Execute(func() (any, error) {
		return b.dest.Search(ctx, q)
	})
	if err != nil {
		return nil, mapBreakerError(err)
	}
	return result.([]*Memory), nil
}

func (b *BreakerWrapper) Delete(ctx context.Context, id string) error {
	_, err := b.cb.Execute(func() (any, error) {
		return nil, b.dest.Delete(ctx, id)
	})
	return mapBreakerError(err)
}

func (b *BreakerWrapper) VectorSearch(ctx context.Context, embedding []float32, limit int) ([]*Memory, error) {
	result, err := b.cb.Execute(func() (any, error) {
		return b.dest.VectorSearch(ctx, embedding, limit)
	})
	if err != nil {
		return nil, mapBreakerError(err)
	}
	return result.([]*Memory), nil
}

func (b *BreakerWrapper) Migrate(ctx context.Context, version int) error {
	return b.dest.Migrate(ctx, version)
}

func (b *BreakerWrapper) Health(ctx context.Context) (*HealthStatus, error) {
	return b.dest.Health(ctx)
}

func (b *BreakerWrapper) Close() error {
	return b.dest.Close()
}

func mapBreakerError(err error) error {
	if err == nil {
		return nil
	}
	if err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests {
		return fmt.Errorf("%w: %v", nexuserr.ErrCircuitOpen, err)
	}
	return err
}
