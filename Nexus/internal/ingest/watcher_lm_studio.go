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

package ingest

import (
	"context"
	"os"
	"path/filepath"
	"sync"
)

// LMStudioWatcher is a v0.1.3 scaffold. The real parser is planned for
// v0.1.4. LM Studio stores conversations in ~/.lmstudio/conversations/*.json.
// The format is straightforward JSON with a messages array, similar to Cursor.
//
// This file exists so that the v0.1.3 interface is stable and so that the
// manager can report "detected, not yet supported" for users who have LM
// Studio installed.
type LMStudioWatcher struct {
	mu    sync.Mutex
	state WatcherState
}

func NewLMStudioWatcher() *LMStudioWatcher {
	return &LMStudioWatcher{state: StateDisabled}
}

func (w *LMStudioWatcher) Name() string      { return "lm_studio" }
func (w *LMStudioWatcher) SourceName() string { return "ingest.lm_studio" }

func (w *LMStudioWatcher) DefaultPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{filepath.Join(home, ".lmstudio", "conversations")}
}

func (w *LMStudioWatcher) Detect(ctx context.Context) (bool, string, error) {
	for _, p := range w.DefaultPaths() {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return true, p, nil
		}
	}
	return false, "", nil
}

func (w *LMStudioWatcher) Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error) {
	return nil, ErrNotImplemented
}

func (w *LMStudioWatcher) State() WatcherState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *LMStudioWatcher) SetState(s WatcherState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = s
}
