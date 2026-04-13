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
	"runtime"
	"sync"
)

// PerplexityCometWatcher is a v0.1.3 scaffold. The real parser is planned for
// v0.1.5. Perplexity Comet stores conversation data in the browser profile
// cache, which varies by OS and browser. Parsing requires decoding browser
// cache format, which is high-risk and not stable across versions.
//
// This file exists so that the v0.1.3 interface is stable and so that the
// manager can report "detected, not yet supported" for users who have
// Perplexity Comet installed.
type PerplexityCometWatcher struct {
	mu    sync.Mutex
	state WatcherState
}

func NewPerplexityCometWatcher() *PerplexityCometWatcher {
	return &PerplexityCometWatcher{state: StateDisabled}
}

func (w *PerplexityCometWatcher) Name() string      { return "perplexity_comet" }
func (w *PerplexityCometWatcher) SourceName() string { return "ingest.perplexity_comet" }

func (w *PerplexityCometWatcher) DefaultPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "Perplexity")}
	case "windows":
		appdata := os.Getenv("LOCALAPPDATA")
		if appdata == "" {
			return nil
		}
		return []string{filepath.Join(appdata, "Perplexity")}
	default:
		return []string{filepath.Join(home, ".config", "perplexity")}
	}
}

func (w *PerplexityCometWatcher) Detect(ctx context.Context) (bool, string, error) {
	for _, p := range w.DefaultPaths() {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return true, p, nil
		}
	}
	return false, "", nil
}

func (w *PerplexityCometWatcher) Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error) {
	return nil, ErrNotImplemented
}

func (w *PerplexityCometWatcher) State() WatcherState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *PerplexityCometWatcher) SetState(s WatcherState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = s
}
