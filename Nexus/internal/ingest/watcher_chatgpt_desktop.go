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

// ChatGPTDesktopWatcher is a v0.1.3 scaffold. The real parser is planned for
// v0.1.4. ChatGPT Desktop stores chats in an Electron IndexedDB leveldb at
// ~/Library/Application Support/ChatGPT (macOS), %APPDATA%/ChatGPT (Windows),
// and ~/.config/ChatGPT (Linux). Parsing requires decoding leveldb snapshot
// format, which is not stable across Chrome/Electron versions.
//
// This file exists so that the v0.1.3 interface is stable and so that the
// manager can report "detected, not yet supported" for users who have ChatGPT
// Desktop installed.
type ChatGPTDesktopWatcher struct {
	mu    sync.Mutex
	state WatcherState
}

func NewChatGPTDesktopWatcher() *ChatGPTDesktopWatcher {
	return &ChatGPTDesktopWatcher{state: StateDisabled}
}

func (w *ChatGPTDesktopWatcher) Name() string      { return "chatgpt_desktop" }
func (w *ChatGPTDesktopWatcher) SourceName() string { return "ingest.chatgpt_desktop" }

func (w *ChatGPTDesktopWatcher) DefaultPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "ChatGPT")}
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return nil
		}
		return []string{filepath.Join(appdata, "ChatGPT")}
	default:
		return []string{filepath.Join(home, ".config", "ChatGPT")}
	}
}

func (w *ChatGPTDesktopWatcher) Detect(ctx context.Context) (bool, string, error) {
	for _, p := range w.DefaultPaths() {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return true, p, nil
		}
	}
	return false, "", nil
}

func (w *ChatGPTDesktopWatcher) Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error) {
	return nil, ErrNotImplemented
}

func (w *ChatGPTDesktopWatcher) State() WatcherState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *ChatGPTDesktopWatcher) SetState(s WatcherState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = s
}
