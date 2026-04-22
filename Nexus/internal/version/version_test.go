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

package version

import (
	"strings"
	"testing"
)

func TestVersion_NonEmpty(t *testing.T) {
	t.Helper()
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}

func TestVersion_SemverFormat(t *testing.T) {
	t.Helper()
	parts := strings.Split(Version, ".")
	if len(parts) < 2 {
		t.Fatalf("Version %q does not look like semver (expected at least major.minor)", Version)
	}
}
