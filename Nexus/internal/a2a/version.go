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

package a2a

import "github.com/bubblefish-tech/nexus/internal/version"

const (
	// ProtocolVersion is the A2A wire protocol version this implementation targets.
	ProtocolVersion = "1.0"

	// ImplementationName identifies this implementation in Agent Cards and logs.
	ImplementationName = "nexus-a2a"

	// GovernanceExtensionURI is the namespace for the Nexus governance extension.
	GovernanceExtensionURI = "sh.bubblefish.nexus.governance/v1"
)

// ImplementationVersion returns the Nexus release version, which is the
// version of this A2A implementation.
func ImplementationVersion() string {
	return version.Version
}
