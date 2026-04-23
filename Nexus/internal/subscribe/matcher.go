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

package subscribe

import (
	"context"
	"math"
	"sync"
)

const defaultThreshold = 0.65

type EmbedFunc func(ctx context.Context, text string) ([]float32, error)

type Matcher struct {
	store     *Store
	embedFn   EmbedFunc
	threshold float64

	mu       sync.RWMutex
	cache    map[string][]float32 // subscription ID → filter embedding
}

func NewMatcher(store *Store, embedFn EmbedFunc) *Matcher {
	return &Matcher{
		store:     store,
		embedFn:   embedFn,
		threshold: defaultThreshold,
		cache:     make(map[string][]float32),
	}
}

func (m *Matcher) Match(ctx context.Context, content string) ([]*Subscription, error) {
	if m.embedFn == nil {
		return nil, nil
	}

	contentVec, err := m.embedFn(ctx, content)
	if err != nil {
		return nil, err
	}
	if len(contentVec) == 0 {
		return nil, nil
	}

	subs := m.store.All()
	if len(subs) == 0 {
		return nil, nil
	}

	var matched []*Subscription
	for _, sub := range subs {
		filterVec, err := m.GetFilterEmbedding(ctx, sub)
		if err != nil || len(filterVec) == 0 {
			continue
		}
		sim := CosineSimilarity(contentVec, filterVec)
		if sim >= m.threshold {
			matched = append(matched, sub)
		}
	}
	return matched, nil
}

func (m *Matcher) GetFilterEmbedding(ctx context.Context, sub *Subscription) ([]float32, error) {
	m.mu.RLock()
	vec, ok := m.cache[sub.ID]
	m.mu.RUnlock()
	if ok {
		return vec, nil
	}

	vec, err := m.embedFn(ctx, sub.Filter)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.cache[sub.ID] = vec
	m.mu.Unlock()

	return vec, nil
}

func (m *Matcher) InvalidateCache(subscriptionID string) {
	m.mu.Lock()
	delete(m.cache, subscriptionID)
	m.mu.Unlock()
}

func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
