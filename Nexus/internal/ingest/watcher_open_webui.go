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

// OpenWebUIWatcher is a v0.1.3 scaffold. The real parser is planned for
// v0.1.4. Open WebUI stores conversations in a webui.db SQLite database,
// typically at ~/.open-webui/webui.db or in a Docker volume.
//
// This file exists so that the v0.1.3 interface is stable and so that the
// manager can report "detected, not yet supported" for users who have Open
// WebUI installed.
type OpenWebUIWatcher struct {
	mu    sync.Mutex
	state WatcherState
}

func NewOpenWebUIWatcher() *OpenWebUIWatcher {
	return &OpenWebUIWatcher{state: StateDisabled}
}

func (w *OpenWebUIWatcher) Name() string      { return "open_webui" }
func (w *OpenWebUIWatcher) SourceName() string { return "ingest.open_webui" }

func (w *OpenWebUIWatcher) DefaultPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{filepath.Join(home, ".open-webui")}
}

func (w *OpenWebUIWatcher) Detect(ctx context.Context) (bool, string, error) {
	for _, p := range w.DefaultPaths() {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return true, p, nil
		}
	}
	return false, "", nil
}

func (w *OpenWebUIWatcher) Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error) {
	return nil, ErrNotImplemented
}

func (w *OpenWebUIWatcher) State() WatcherState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *OpenWebUIWatcher) SetState(s WatcherState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = s
}
