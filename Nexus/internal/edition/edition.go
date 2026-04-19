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

package edition

import "fmt"

// Edition describes which build of Nexus is running and which optional features are enabled.
type Edition struct {
	Name     string
	Features []string
}

// Current is the active edition. Community by default; overwritten by Enterprise/TS init.
var Current = &Edition{
	Name:     "community",
	Features: []string{},
}

// Has reports whether the given feature is enabled in the current edition.
func (e *Edition) Has(feature string) bool {
	for _, f := range e.Features {
		if f == feature {
			return true
		}
	}
	return false
}

// String returns a human-readable edition label.
func (e *Edition) String() string {
	return fmt.Sprintf("nexus/%s", e.Name)
}
