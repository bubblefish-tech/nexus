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

import (
	"crypto/rand"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// ID prefixes for each NA2A entity type.
const (
	PrefixMessage  = "msg_"
	PrefixTask     = "tsk_"
	PrefixContext   = "ctx_"
	PrefixArtifact = "art_"
	PrefixGrant    = "gnt_"
	PrefixAudit    = "aud_"
	PrefixApproval = "apr_"
	PrefixAgent    = "agt_"
)

// AllPrefixes returns all defined ID prefixes for validation.
func AllPrefixes() []string {
	return []string{
		PrefixMessage, PrefixTask, PrefixContext, PrefixArtifact,
		PrefixGrant, PrefixAudit, PrefixApproval, PrefixAgent,
	}
}

// entropy is a pool of monotonic entropy sources for ULID generation.
// Each goroutine gets its own entropy source via the pool to avoid contention.
var entropyPool = sync.Pool{
	New: func() interface{} {
		return ulid.Monotonic(rand.Reader, 0)
	},
}

func newULID() string {
	entropy := entropyPool.Get().(*ulid.MonotonicEntropy)
	defer entropyPool.Put(entropy)
	id, err := ulid.New(ulid.Timestamp(time.Now()), entropy)
	if err != nil {
		// Fall back to non-monotonic if entropy is exhausted.
		// This should never happen with crypto/rand.
		id = ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader)
	}
	return id.String()
}

// NewMessageID generates a new msg_ prefixed ULID.
func NewMessageID() string { return PrefixMessage + newULID() }

// NewTaskID generates a new tsk_ prefixed ULID.
func NewTaskID() string { return PrefixTask + newULID() }

// NewContextID generates a new ctx_ prefixed ULID.
func NewContextID() string { return PrefixContext + newULID() }

// NewArtifactID generates a new art_ prefixed ULID.
func NewArtifactID() string { return PrefixArtifact + newULID() }

// NewGrantID generates a new gnt_ prefixed ULID.
func NewGrantID() string { return PrefixGrant + newULID() }

// NewAuditID generates a new aud_ prefixed ULID.
func NewAuditID() string { return PrefixAudit + newULID() }

// NewApprovalID generates a new apr_ prefixed ULID.
func NewApprovalID() string { return PrefixApproval + newULID() }

// NewAgentID generates a new agt_ prefixed ULID.
func NewAgentID() string { return PrefixAgent + newULID() }

// ValidateID checks that an ID has a known prefix and a valid ULID suffix.
func ValidateID(id string) error {
	for _, p := range AllPrefixes() {
		if strings.HasPrefix(id, p) {
			suffix := id[len(p):]
			if len(suffix) != 26 {
				return NewErrorf(CodeInvalidParams, "invalid ID %q: ULID suffix must be 26 characters", id)
			}
			if _, err := ulid.Parse(suffix); err != nil {
				return NewErrorf(CodeInvalidParams, "invalid ID %q: bad ULID: %v", id, err)
			}
			return nil
		}
	}
	return NewErrorf(CodeInvalidParams, "invalid ID %q: unknown prefix", id)
}

// IDPrefix returns the prefix portion of an ID, or "" if unrecognized.
func IDPrefix(id string) string {
	for _, p := range AllPrefixes() {
		if strings.HasPrefix(id, p) {
			return p
		}
	}
	return ""
}
