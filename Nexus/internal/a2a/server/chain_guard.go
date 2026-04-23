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

package server

import (
	"encoding/json"
	"fmt"

	"github.com/bubblefish-tech/nexus/internal/a2a"
)

// DefaultMaxChainDepth is the maximum number of hops allowed in an
// agent-to-agent invocation chain before the request is rejected.
const DefaultMaxChainDepth = 4

// ExtractChainDepth reads the governance extension from a message and returns
// the current chain depth. Returns 0 if no governance extension is present.
func ExtractChainDepth(msg *a2a.Message) int {
	if msg == nil || msg.Extensions == nil {
		return 0
	}

	var extMap map[string]json.RawMessage
	if err := json.Unmarshal(msg.Extensions, &extMap); err != nil {
		return 0
	}

	raw, ok := extMap[a2a.GovernanceExtensionURI]
	if !ok {
		return 0
	}

	var gov a2a.GovernanceExtension
	if err := json.Unmarshal(raw, &gov); err != nil {
		return 0
	}

	return gov.ChainDepth
}

// IncrementChainDepth clones the message, increments the chainDepth in the
// governance extension, and returns the new message. If the incremented depth
// would exceed maxDepth, an error is returned.
func IncrementChainDepth(msg *a2a.Message, maxDepth int) (*a2a.Message, error) {
	if maxDepth <= 0 {
		maxDepth = DefaultMaxChainDepth
	}

	currentDepth := ExtractChainDepth(msg)
	newDepth := currentDepth + 1

	if newDepth > maxDepth {
		return nil, fmt.Errorf("chain depth %d exceeds maximum %d", newDepth, maxDepth)
	}

	// Clone the message.
	cloned := *msg

	// Parse existing extensions or create new map.
	var extMap map[string]json.RawMessage
	if msg.Extensions != nil {
		if err := json.Unmarshal(msg.Extensions, &extMap); err != nil {
			extMap = make(map[string]json.RawMessage)
		}
	} else {
		extMap = make(map[string]json.RawMessage)
	}

	// Parse existing governance extension or create new one.
	var gov a2a.GovernanceExtension
	if raw, ok := extMap[a2a.GovernanceExtensionURI]; ok {
		_ = json.Unmarshal(raw, &gov) // best-effort parse
	}

	gov.ChainDepth = newDepth
	gov.MaxChainDepth = maxDepth

	govJSON, err := json.Marshal(gov)
	if err != nil {
		return nil, fmt.Errorf("marshal governance extension: %w", err)
	}
	extMap[a2a.GovernanceExtensionURI] = govJSON

	extJSON, err := json.Marshal(extMap)
	if err != nil {
		return nil, fmt.Errorf("marshal extensions: %w", err)
	}
	cloned.Extensions = extJSON

	return &cloned, nil
}
