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

// Package hotreload implements live config reloading for BubbleFish Nexus
// source files. It watches the sources directory with fsnotify and applies
// config changes atomically via an RWMutex.
//
// Hot reload scope (per Tech Spec Section 14.1):
//   - Source config changes: applied atomically. In-flight handlers complete
//     with the old config. Auth hot path uses RLock; reload uses Lock.
//   - Destination config changes: WARN logged, old config kept. Restart required.
//   - Compiled JSON: written atomically (temp file + fsync + os.Rename).
//
// INVARIANTS:
//   - NEVER apply destination config changes via hot reload.
//   - NEVER write compiled JSON non-atomically.
//   - watchLoop goroutine exits cleanly when stop channel is closed.
//   - NEVER use Lock() on the auth hot path. Only RLock().
package hotreload

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/fsutil"
	"github.com/BubbleFish-Nexus/internal/signing"
	"github.com/fsnotify/fsnotify"
)

// Config holds all parameters required to construct a Watcher.
type Config struct {
	// SourcesDir is the directory watched for *.toml source config files.
	SourcesDir string

	// ConfigDir is the root config directory (used for compiled JSON output).
	ConfigDir string

	// Mu is the shared RWMutex that guards the daemon's config pointer.
	// Auth hot path must use RLock; reload uses Lock.
	Mu *sync.RWMutex

	// Snapshot returns the current *config.Config. The Watcher calls this
	// while holding Mu.RLock() — the caller must not acquire any additional locks.
	Snapshot func() *config.Config

	// Apply atomically installs a new *config.Config. The Watcher calls this
	// while holding Mu.Lock() — the caller must not acquire any additional locks.
	Apply func(*config.Config)

	// Reload loads a fresh *config.Config from disk. Called by the Watcher
	// outside of any mutex; must be safe to call from a goroutine.
	Reload func() (*config.Config, error)

	// SigningKey is the resolved signing key bytes. When non-nil, the watcher
	// re-verifies compiled config signatures before applying a reload.
	// Nil means signing is disabled — no verification is performed.
	// NEVER log this value.
	SigningKey []byte

	// SigningEvent is called when a config signature verification failure
	// occurs during hot reload. May be nil if signing is disabled.
	SigningEvent signing.SecurityEventFunc

	// Logger is the structured logger for watcher events.
	Logger *slog.Logger
}

// Watcher monitors the sources directory for *.toml changes and hot-reloads
// source configuration. All state is in struct fields — no package-level vars.
type Watcher struct {
	cfg  Config
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// New creates a Watcher from cfg. Call Start() to begin watching.
// Panics if any required field in cfg is nil.
func New(cfg Config) *Watcher {
	if cfg.Mu == nil {
		panic("hotreload: Mu must not be nil")
	}
	if cfg.Snapshot == nil {
		panic("hotreload: Snapshot must not be nil")
	}
	if cfg.Apply == nil {
		panic("hotreload: Apply must not be nil")
	}
	if cfg.Reload == nil {
		panic("hotreload: Reload must not be nil")
	}
	if cfg.Logger == nil {
		panic("hotreload: Logger must not be nil")
	}
	return &Watcher{
		cfg:  cfg,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

// Start adds the sources directory to an fsnotify watcher and launches the
// watchLoop goroutine. Returns an error if the directory cannot be watched
// (e.g. it does not exist); the daemon should log a warning and continue.
//
// Start must be called at most once. The goroutine is stopped by calling Stop().
func (w *Watcher) Start() error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("hotreload: create fsnotify watcher: %w", err)
	}

	if err := fw.Add(w.cfg.SourcesDir); err != nil {
		_ = fw.Close()
		return fmt.Errorf("hotreload: watch %q: %w", w.cfg.SourcesDir, err)
	}

	w.cfg.Logger.Info("hotreload: watching sources directory",
		"component", "hotreload",
		"dir", w.cfg.SourcesDir,
	)

	go w.watchLoop(fw)
	return nil
}

// Stop signals the watchLoop goroutine to exit and waits for it to finish.
// Safe to call multiple times; only the first call has any effect (sync.Once).
//
// Reference: Phase 0D Behavioral Contract item 10.
func (w *Watcher) Stop() {
	w.once.Do(func() { close(w.stop) })
	<-w.done
}

// watchLoop is the goroutine body. It selects on fsnotify events, errors, and
// the stop channel. Exits cleanly when stop is closed.
//
// INVARIANT: always exits by closing w.done.
func (w *Watcher) watchLoop(fw *fsnotify.Watcher) {
	defer close(w.done)
	defer func() { _ = fw.Close() }()

	for {
		select {
		case event, ok := <-fw.Events:
			if !ok {
				return
			}
			// React to writes and newly created .toml files.
			if (event.Has(fsnotify.Write) || event.Has(fsnotify.Create)) &&
				strings.HasSuffix(event.Name, ".toml") {
				w.cfg.Logger.Debug("hotreload: source file changed",
					"component", "hotreload",
					"file", event.Name,
					"op", event.Op.String(),
				)
				w.reload()
			}

		case err, ok := <-fw.Errors:
			if !ok {
				return
			}
			w.cfg.Logger.Error("hotreload: fsnotify error",
				"component", "hotreload",
				"error", err,
			)

		case <-w.stop:
			w.cfg.Logger.Debug("hotreload: stop signal received — exiting watchLoop",
				"component", "hotreload",
			)
			return
		}
	}
}

// reload loads a new config from disk, validates it for destination changes,
// and applies it atomically if safe to do so.
//
// Destination config changes are NEVER applied — WARN is logged and the old
// config remains active until the operator restarts the daemon.
//
// Reference: Tech Spec Section 14.1.
func (w *Watcher) reload() {
	newCfg, err := w.cfg.Reload()
	if err != nil {
		w.cfg.Logger.Error("hotreload: reload failed — keeping current config",
			"component", "hotreload",
			"error", err,
		)
		return
	}

	// Snapshot the current config under RLock. The snapshot is a pointer to an
	// immutable Config struct; it is safe to read fields on it after releasing
	// the lock because hot reload only swaps the pointer, never mutates in-place.
	w.cfg.Mu.RLock()
	oldCfg := w.cfg.Snapshot()
	w.cfg.Mu.RUnlock()

	// INVARIANT: destination changes are never applied via hot reload.
	if destinationsChanged(oldCfg, newCfg) {
		w.cfg.Logger.Warn("hotreload: destination config changed — restart required; keeping old config",
			"component", "hotreload",
		)
		return
	}

	// Write compiled JSON atomically before applying.
	// Temp file + fsync + os.Rename guarantees no partial writes are visible.
	if err := writeCompiledJSON(w.cfg.ConfigDir, newCfg); err != nil {
		w.cfg.Logger.Error("hotreload: write compiled JSON failed",
			"component", "hotreload",
			"error", err,
		)
		// Non-fatal: proceed with the reload even if the cache write fails.
	}

	// Re-verify config signatures if signing is enabled.
	// Reference: Tech Spec Section 6.5 — refuse reload on invalid signature.
	if w.cfg.SigningKey != nil {
		compiledDir := filepath.Join(w.cfg.ConfigDir, "compiled")
		if err := signing.VerifyAll(compiledDir, w.cfg.SigningKey, w.cfg.SigningEvent, w.cfg.Logger); err != nil {
			w.cfg.Logger.Error("hotreload: config signature verification failed — keeping current config",
				"component", "hotreload",
				"error", err,
			)
			return
		}
		w.cfg.Logger.Debug("hotreload: config signature verification passed",
			"component", "hotreload",
		)
	}

	// Apply the new config atomically under Lock.
	// In-flight handlers holding RLock complete with the old config snapshot.
	w.cfg.Mu.Lock()
	w.cfg.Apply(newCfg)
	w.cfg.Mu.Unlock()

	w.cfg.Logger.Info("hotreload: source config reloaded",
		"component", "hotreload",
		"source_count", len(newCfg.Sources),
	)
}

// destinationsChanged returns true if the set of destinations or any of their
// names/types differ between oldCfg and newCfg.
func destinationsChanged(oldCfg, newCfg *config.Config) bool {
	if len(oldCfg.Destinations) != len(newCfg.Destinations) {
		return true
	}
	// Build a map of old destinations by name for O(1) lookup.
	oldDests := make(map[string]*config.Destination, len(oldCfg.Destinations))
	for _, d := range oldCfg.Destinations {
		oldDests[d.Name] = d
	}
	for _, nd := range newCfg.Destinations {
		od, exists := oldDests[nd.Name]
		if !exists {
			return true
		}
		// Compare the fields that affect routing and connectivity.
		if od.Type != nd.Type || od.DBPath != nd.DBPath ||
			od.DSN != nd.DSN || od.URL != nd.URL {
			return true
		}
	}
	return false
}

// writeCompiledJSON serialises newCfg.Sources to JSON and writes the result
// atomically to configDir/sources.compiled.json using temp file + fsync +
// os.Rename.
//
// Reference: Tech Spec Section 14.1 (compiled JSON output), Phase 0D
// Behavioral Contract item 8.
func writeCompiledJSON(configDir string, newCfg *config.Config) error {
	if configDir == "" {
		return nil // No-op when configDir is empty (e.g. test environments).
	}

	outPath := filepath.Join(configDir, "sources.compiled.json")
	tmpPath := outPath + ".tmp"

	data, err := json.Marshal(newCfg.Sources)
	if err != nil {
		return fmt.Errorf("hotreload: marshal compiled JSON: %w", err)
	}

	// Write to temp file in the same directory (guarantees os.Rename atomicity).
	f, err := os.OpenFile(tmpPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("hotreload: open temp file: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("hotreload: write temp file: %w", err)
	}

	// fsync before rename to ensure the data is durable.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("hotreload: fsync temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("hotreload: close temp file: %w", err)
	}

	// Atomic rename — on the same filesystem, this is atomic on all platforms.
	if err := fsutil.RobustRename(tmpPath, outPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("hotreload: rename compiled JSON: %w", err)
	}

	return nil
}
