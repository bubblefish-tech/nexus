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

package bridge

import (
	"encoding/json"
	"fmt"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/client"
)

// MCPToNA2A converts MCP tool arguments into an NA2A Message, skill name,
// and send configuration. The MCP args are expected to contain:
//   - "agent" (string, required): target agent name
//   - "skill" (string, optional): skill to invoke
//   - "input" (string or object, required): the message content
//   - "blocking" (bool, optional): wait for completion
//   - "timeout_ms" (number, optional): timeout in milliseconds
func MCPToNA2A(args map[string]interface{}, sourceIdentity string) (*a2a.Message, string, *client.SendConfig, error) {
	// Extract input.
	input, ok := args["input"]
	if !ok {
		return nil, "", nil, fmt.Errorf("bridge: missing required field 'input'")
	}

	// Build message parts based on input type.
	var parts []a2a.Part
	switch v := input.(type) {
	case string:
		parts = append(parts, a2a.NewTextPart(v))
	default:
		// Treat as structured data.
		data, err := json.Marshal(v)
		if err != nil {
			return nil, "", nil, fmt.Errorf("bridge: marshal input: %w", err)
		}
		parts = append(parts, a2a.NewDataPart(data))
	}

	msg := a2a.NewMessage(a2a.RoleUser, parts...)

	// Extract skill.
	skill, _ := args["skill"].(string)

	// Build send config.
	cfg := &client.SendConfig{}
	if b, ok := args["blocking"].(bool); ok {
		cfg.Blocking = b
	}
	if t, ok := args["timeout_ms"].(float64); ok {
		cfg.TimeoutMs = int64(t)
	}

	return &msg, skill, cfg, nil
}

// NA2AToMCP converts an NA2A Task result back to an MCP tool result format.
// The result is a map containing the task state, artifacts, and any text output.
func NA2AToMCP(task *a2a.Task) map[string]interface{} {
	result := map[string]interface{}{
		"task_id": task.TaskID,
		"state":   string(task.Status.State),
	}

	// Extract status message if present.
	if task.Status.Message != nil {
		result["status_message"] = extractTextFromMessage(*task.Status.Message)
	}

	// Extract artifact contents.
	if len(task.Artifacts) > 0 {
		artifacts := make([]map[string]interface{}, 0, len(task.Artifacts))
		for _, art := range task.Artifacts {
			artMap := map[string]interface{}{
				"artifact_id": art.ArtifactID,
			}
			if art.Name != "" {
				artMap["name"] = art.Name
			}
			if art.Description != "" {
				artMap["description"] = art.Description
			}

			// Extract parts.
			var textParts []string
			var dataParts []json.RawMessage
			for _, pw := range art.Parts {
				switch p := pw.Part.(type) {
				case a2a.TextPart:
					textParts = append(textParts, p.Text)
				case a2a.DataPart:
					dataParts = append(dataParts, p.Data)
				}
			}
			if len(textParts) > 0 {
				artMap["text"] = textParts
			}
			if len(dataParts) > 0 {
				artMap["data"] = dataParts
			}

			artifacts = append(artifacts, artMap)
		}
		result["artifacts"] = artifacts
	}

	return result
}

// extractTextFromMessage concatenates all text parts in a message.
func extractTextFromMessage(msg a2a.Message) string {
	var text string
	for _, pw := range msg.Parts {
		if tp, ok := pw.Part.(a2a.TextPart); ok {
			if text != "" {
				text += "\n"
			}
			text += tp.Text
		}
	}
	return text
}
