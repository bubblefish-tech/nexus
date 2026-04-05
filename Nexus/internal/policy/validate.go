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

package policy

import "fmt"

// Validate checks that every allowed_destinations entry in each PolicyEntry
// names a known destination. It also checks for duplicate entries within the
// same source's allowed_destinations list.
//
// knownDestinations is the set of destination names loaded from the
// destinations/ directory. Build must fail (SCHEMA_ERROR) on the first
// violation; callers must not proceed to Compile on error.
//
// Reference: Tech Spec Section 6.1, Phase 1 Behavioral Contract item 2.
func Validate(entries []PolicyEntry, knownDestinations map[string]bool) error {
	for _, e := range entries {
		seen := make(map[string]bool, len(e.AllowedDestinations))
		for _, dest := range e.AllowedDestinations {
			if !knownDestinations[dest] {
				return fmt.Errorf("SCHEMA_ERROR: source %q policy: allowed_destinations references unknown destination %q",
					e.Source, dest)
			}
			if seen[dest] {
				return fmt.Errorf("SCHEMA_ERROR: source %q policy: duplicate allowed_destinations entry %q",
					e.Source, dest)
			}
			seen[dest] = true
		}
	}
	return nil
}
