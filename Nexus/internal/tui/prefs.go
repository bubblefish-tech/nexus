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

package tui

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// TUIPrefs holds user preferences loaded from tui_prefs.toml.
type TUIPrefs struct {
	Sidebar SidebarPrefs `toml:"sidebar"`
}

// SidebarPrefs controls which sections appear in the sidebar and in what order.
type SidebarPrefs struct {
	// Sections lists section names in display order. Empty means use defaults.
	Sections []string `toml:"sections"`
	// Hidden lists section names to exclude from the sidebar.
	Hidden []string `toml:"hidden"`
}

// defaultSectionOrder is the canonical section order when no prefs are set.
var defaultSectionOrder = []string{"Daemon", "Sources", "Destinations", "Ports", "Health"}

// LoadPrefs reads tui_prefs.toml from configDir. Returns nil (no error) when
// the file is missing — callers should treat nil as DefaultPrefs().
func LoadPrefs(configDir string) (*TUIPrefs, error) {
	path := filepath.Join(configDir, "tui_prefs.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var p TUIPrefs
	if _, err := toml.Decode(string(data), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// DefaultPrefs returns the built-in default preferences.
func DefaultPrefs() *TUIPrefs {
	return &TUIPrefs{
		Sidebar: SidebarPrefs{
			Sections: append([]string{}, defaultSectionOrder...),
		},
	}
}

// ApplySidebarOrder reorders sections per prefs and removes hidden ones.
// sectionMap maps section name → index in the full sections slice.
// Returns the ordered, filtered list of section names.
func (p *TUIPrefs) ApplySidebarOrder(available []string) []string {
	if p == nil {
		return available
	}

	// Build hidden set.
	hidden := make(map[string]bool, len(p.Sidebar.Hidden))
	for _, h := range p.Sidebar.Hidden {
		hidden[h] = true
	}

	// Determine order: use prefs.Sections if non-empty, else keep original order.
	order := p.Sidebar.Sections
	if len(order) == 0 {
		order = available
	}

	// Emit sections in order, skipping hidden and unknown ones.
	avail := make(map[string]bool, len(available))
	for _, s := range available {
		avail[s] = true
	}

	seen := make(map[string]bool)
	var result []string
	for _, name := range order {
		if hidden[name] || !avail[name] || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}

	// Append any available sections not mentioned in prefs order (but not hidden).
	for _, name := range available {
		if !seen[name] && !hidden[name] {
			result = append(result, name)
		}
	}

	return result
}
