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

package api

import "strings"

// HintForEndpoint returns a user-facing hint string for an API error.
// Callers pass the endpoint path and the classified error kind.
// The hint is displayed in the TUI empty-state panel for the affected screen.
func HintForEndpoint(endpoint string, kind ErrorKind) string {
	if kind == ErrKindForbidden {
		return "Admin token required — set NEXUS_ADMIN_TOKEN or pass --admin-token"
	}
	if kind == ErrKindNotFound {
		switch {
		case strings.HasPrefix(endpoint, "/api/crypto"):
			return "Audit signing not configured"
		case strings.HasPrefix(endpoint, "/api/quarantine"):
			return "Quarantine subsystem not enabled"
		case strings.HasPrefix(endpoint, "/api/memories"):
			return "Memory store not initialized"
		case strings.HasPrefix(endpoint, "/api/control/approvals"):
			return "Governance not enabled"
		case strings.HasPrefix(endpoint, "/api/provenance"):
			return "Provenance chain not initialized"
		case strings.HasPrefix(endpoint, "/stream/"):
			return "Live stream not available on this daemon"
		default:
			return "Feature not available"
		}
	}
	if kind == ErrKindNetwork {
		return "Cannot reach daemon — is it running? Try: nexus start"
	}
	return ""
}
