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

package daemon

import "net/http"

// RBACEngine is the seam for Enterprise role-based access control.
// Community edition leaves this nil; all nil checks must be done at call sites.
type RBACEngine interface {
	// CheckPermission returns nil if the request is permitted, or an error describing
	// the denial. The error text is safe to surface to the caller.
	CheckPermission(r *http.Request, permission string) error
}

// BoundaryEnforcer is the seam for TS-grade data-boundary enforcement.
// Community edition leaves this nil.
type BoundaryEnforcer interface {
	// ValidateEmbeddingEndpoint returns an error if the endpoint URL is outside
	// the permitted boundary (e.g., external network access is prohibited).
	ValidateEmbeddingEndpoint(endpoint string) error

	// EnforceTLS reports whether all outbound connections must use TLS.
	EnforceTLS() bool
}

// ClassificationMarker is the seam for government data-classification marking.
// Community edition leaves this nil.
type ClassificationMarker interface {
	// Level returns the classification label (e.g., "UNCLASSIFIED", "SECRET").
	Level() string

	// Banner returns the banner string that must appear in every HTTP response.
	Banner() string

	// MarkResponse adds classification headers and banners to the response writer.
	MarkResponse(w http.ResponseWriter)
}
