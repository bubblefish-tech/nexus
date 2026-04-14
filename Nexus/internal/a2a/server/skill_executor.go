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

package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/BubbleFish-Nexus/internal/a2a"
)

// SkillFunc is the signature of a skill execution function.
type SkillFunc func(ctx context.Context, input *a2a.DataPart, files []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error)

// InMemorySkillExecutor maps skill names to functions for testing.
type InMemorySkillExecutor struct {
	mu     sync.RWMutex
	skills map[string]SkillFunc
}

// NewInMemorySkillExecutor creates an InMemorySkillExecutor with the given skill functions.
func NewInMemorySkillExecutor(skills map[string]SkillFunc) *InMemorySkillExecutor {
	m := make(map[string]SkillFunc, len(skills))
	for k, v := range skills {
		m[k] = v
	}
	return &InMemorySkillExecutor{skills: m}
}

// Execute implements SkillExecutor.
func (e *InMemorySkillExecutor) Execute(ctx context.Context, skill string, input *a2a.DataPart, files []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error) {
	e.mu.RLock()
	fn, ok := e.skills[skill]
	e.mu.RUnlock()

	if !ok {
		return nil, nil, fmt.Errorf("skill %q not registered in executor", skill)
	}

	return fn(ctx, input, files)
}

// RegisterSkill adds or replaces a skill function.
func (e *InMemorySkillExecutor) RegisterSkill(name string, fn SkillFunc) {
	e.mu.Lock()
	e.skills[name] = fn
	e.mu.Unlock()
}
