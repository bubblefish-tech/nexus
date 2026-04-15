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

package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/BubbleFish-Nexus/internal/mcp/bridge"
)

// a2aToolDefs converts bridge.ToolDefinitions into MCP toolDef structs
// for inclusion in the tools/list response.
func a2aToolDefs(b *bridge.Bridge) []toolDef {
	defs := b.ToolDefinitions()
	out := make([]toolDef, 0, len(defs))
	for _, d := range defs {
		td := toolDef{
			Name:        d.Name,
			Description: d.Description,
		}
		// Convert the bridge's map[string]interface{} schema to our inputSchema.
		td.InputSchema = convertBridgeSchema(d.InputSchema)
		out = append(out, td)
	}
	return out
}

// convertBridgeSchema converts a bridge tool's InputSchema (map[string]interface{})
// to the MCP server's inputSchema struct.
func convertBridgeSchema(raw map[string]interface{}) inputSchema {
	schema := inputSchema{
		Type:       "object",
		Properties: make(map[string]propDef),
	}

	if props, ok := raw["properties"].(map[string]interface{}); ok {
		for name, v := range props {
			p := propDef{}
			if vm, ok := v.(map[string]interface{}); ok {
				if t, ok := vm["type"].(string); ok {
					p.Type = t
				}
				if d, ok := vm["description"].(string); ok {
					p.Description = d
				}
			}
			schema.Properties[name] = p
		}
	}

	if req, ok := raw["required"].([]string); ok {
		schema.Required = req
	}

	return schema
}

// callA2ABridgeTool dispatches an A2A bridge tool call. The bridge
// handlers accept map[string]interface{} args and return (interface{}, error).
func (s *Server) callA2ABridgeTool(w http.ResponseWriter, r *http.Request, req rpcRequest, toolName string, args json.RawMessage) {
	if s.a2aBridge == nil {
		s.writeToolError(w, r, req.ID, "A2A is not enabled")
		return
	}

	// Parse args into map[string]interface{}.
	var argsMap map[string]interface{}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argsMap); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid arguments: "+err.Error())
			return
		}
	}
	if argsMap == nil {
		argsMap = make(map[string]interface{})
	}

	ctx := r.Context()

	// Attach MCP client info to context for source identity derivation.
	ctx = bridge.WithClientInfo(ctx, s.sourceName, "")

	var result interface{}
	var err error

	switch toolName {
	case "a2a_list_agents":
		result, err = s.a2aBridge.HandleA2AListAgents(ctx, argsMap)
	case "a2a_describe_agent":
		result, err = s.a2aBridge.HandleA2ADescribeAgent(ctx, argsMap)
	case "a2a_send_to_agent":
		result, err = s.a2aBridge.HandleA2ASendToAgent(ctx, argsMap)
	case "a2a_stream_to_agent":
		result, err = s.a2aBridge.HandleA2AStreamToAgent(ctx, argsMap)
	case "a2a_get_task":
		result, err = s.a2aBridge.HandleA2AGetTask(ctx, argsMap)
	case "a2a_resume_task":
		result, err = s.a2aBridge.HandleA2AResumeTask(ctx, argsMap)
	case "a2a_cancel_task":
		result, err = s.a2aBridge.HandleA2ACancelTask(ctx, argsMap)
	case "a2a_list_pending_approvals":
		result, err = s.a2aBridge.HandleA2AListPendingApprovals(ctx, argsMap)
	case "a2a_list_grants":
		result, err = s.a2aBridge.HandleA2AListGrants(ctx, argsMap)
	default:
		s.writeRPCError(w, r, req.ID, rpcMethodNotFound, fmt.Sprintf("unknown A2A tool %q", toolName))
		return
	}

	if err != nil {
		s.writeToolError(w, r, req.ID, err.Error())
		return
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.writeRPCError(w, r, req.ID, rpcInternalError, "marshal result: "+err.Error())
		return
	}

	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(resultJSON)}},
	})
}
