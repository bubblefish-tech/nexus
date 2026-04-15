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

// ToolDefinition describes an MCP tool exposed by the bridge.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ToolDefinitions returns the MCP tool schemas for all 9 bridge tools.
func (b *Bridge) ToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "a2a_list_agents",
			Description: "List all registered A2A agents and their status.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"status": map[string]interface{}{
						"type":        "string",
						"description": "Filter by agent status: active, suspended, retired. Omit for all.",
						"enum":        []string{"active", "suspended", "retired"},
					},
				},
			},
		},
		{
			Name:        "a2a_describe_agent",
			Description: "Get detailed information about a specific A2A agent including its skills and capabilities.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"agent"},
				"properties": map[string]interface{}{
					"agent": map[string]interface{}{
						"type":        "string",
						"description": "Agent name or ID.",
					},
				},
			},
		},
		{
			Name:        "a2a_send_to_agent",
			Description: "Send a message to an A2A agent and receive a task result. This is the primary way to invoke agent skills.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"agent", "input"},
				"properties": map[string]interface{}{
					"agent": map[string]interface{}{
						"type":        "string",
						"description": "Target agent name.",
					},
					"skill": map[string]interface{}{
						"type":        "string",
						"description": "Skill to invoke on the target agent.",
					},
					"input": map[string]interface{}{
						"type":        "string",
						"description": "Message content to send to the agent.",
					},
					"blocking": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, wait for the task to complete before returning.",
						"default":     true,
					},
					"timeout_ms": map[string]interface{}{
						"type":        "number",
						"description": "Timeout in milliseconds.",
					},
				},
			},
		},
		{
			Name:        "a2a_stream_to_agent",
			Description: "Send a message to an A2A agent with streaming responses.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"agent", "input"},
				"properties": map[string]interface{}{
					"agent": map[string]interface{}{
						"type":        "string",
						"description": "Target agent name.",
					},
					"skill": map[string]interface{}{
						"type":        "string",
						"description": "Skill to invoke.",
					},
					"input": map[string]interface{}{
						"type":        "string",
						"description": "Message content.",
					},
				},
			},
		},
		{
			Name:        "a2a_get_task",
			Description: "Get the status and result of an existing A2A task.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"task_id"},
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to look up.",
					},
					"include_history": map[string]interface{}{
						"type":        "boolean",
						"description": "Include full message history.",
						"default":     false,
					},
				},
			},
		},
		{
			Name:        "a2a_resume_task",
			Description: "Resume a task that is waiting for input by sending a follow-up message.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"task_id", "input"},
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to resume.",
					},
					"input": map[string]interface{}{
						"type":        "string",
						"description": "Follow-up input.",
					},
				},
			},
		},
		{
			Name:        "a2a_cancel_task",
			Description: "Cancel a running A2A task.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"task_id"},
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to cancel.",
					},
					"reason": map[string]interface{}{
						"type":        "string",
						"description": "Reason for cancellation.",
					},
				},
			},
		},
		{
			Name:        "a2a_list_pending_approvals",
			Description: "List governance approval requests waiting for human decision.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "a2a_list_grants",
			Description: "List all governance grants between agents.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}
