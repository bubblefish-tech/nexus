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

import "strings"

// Validate checks a Message for structural correctness.
// Returns nil if valid, or a typed *Error describing the first issue found.
func (m *Message) Validate() *Error {
	if m.Kind != "message" {
		return NewError(CodeInvalidParams, "message: kind must be \"message\"")
	}
	if m.MessageID == "" {
		return NewError(CodeInvalidParams, "message: messageId is required")
	}
	if err := ValidateID(m.MessageID); err != nil {
		if e, ok := err.(*Error); ok {
			return e
		}
		return NewErrorf(CodeInvalidParams, "message: invalid messageId: %v", err)
	}
	if !ValidRole(m.Role) {
		return NewErrorf(CodeInvalidParams, "message: invalid role %q, must be \"user\" or \"agent\"", m.Role)
	}
	if len(m.Parts) == 0 {
		return NewError(CodeInvalidParams, "message: at least one part is required")
	}
	for i, pw := range m.Parts {
		if pw.Part == nil {
			return NewErrorf(CodeInvalidParams, "message: parts[%d] is nil", i)
		}
		if err := validatePart(pw.Part); err != nil {
			return NewErrorf(CodeInvalidParams, "message: parts[%d]: %s", i, err.Message)
		}
	}
	if m.ContextID != "" {
		if err := ValidateID(m.ContextID); err != nil {
			if e, ok := err.(*Error); ok {
				return e
			}
			return NewErrorf(CodeInvalidParams, "message: invalid contextId: %v", err)
		}
	}
	return nil
}

func validatePart(p Part) *Error {
	switch v := p.(type) {
	case TextPart:
		if v.Kind != "text" {
			return NewError(CodeInvalidParams, "text part: kind must be \"text\"")
		}
		if v.Text == "" {
			return NewError(CodeInvalidParams, "text part: text is required")
		}
	case FilePart:
		if v.Kind != "file" {
			return NewError(CodeInvalidParams, "file part: kind must be \"file\"")
		}
		if v.File.Bytes == nil && v.File.URI == nil {
			return NewError(CodeInvalidParams, "file part: file must have bytes or uri")
		}
	case DataPart:
		if v.Kind != "data" {
			return NewError(CodeInvalidParams, "data part: kind must be \"data\"")
		}
		if len(v.Data) == 0 {
			return NewError(CodeInvalidParams, "data part: data is required")
		}
	default:
		return NewErrorf(CodeInvalidParams, "unknown part type %T", p)
	}
	return nil
}

// Validate checks a Task for structural correctness.
func (t *Task) Validate() *Error {
	if t.Kind != "task" {
		return NewError(CodeInvalidParams, "task: kind must be \"task\"")
	}
	if t.TaskID == "" {
		return NewError(CodeInvalidParams, "task: taskId is required")
	}
	if err := ValidateID(t.TaskID); err != nil {
		if e, ok := err.(*Error); ok {
			return e
		}
		return NewErrorf(CodeInvalidParams, "task: invalid taskId: %v", err)
	}
	if !ValidTaskState(t.Status.State) {
		return NewErrorf(CodeInvalidParams, "task: invalid status.state %q", t.Status.State)
	}
	if t.Status.Timestamp == "" {
		return NewError(CodeInvalidParams, "task: status.timestamp is required")
	}
	if _, err := ParseTime(t.Status.Timestamp); err != nil {
		return NewErrorf(CodeInvalidParams, "task: invalid status.timestamp: %v", err)
	}
	if t.ContextID != "" {
		if err := ValidateID(t.ContextID); err != nil {
			if e, ok := err.(*Error); ok {
				return e
			}
			return NewErrorf(CodeInvalidParams, "task: invalid contextId: %v", err)
		}
	}
	for i, a := range t.Artifacts {
		if err := a.Validate(); err != nil {
			return NewErrorf(CodeInvalidParams, "task: artifacts[%d]: %s", i, err.Message)
		}
	}
	return nil
}

// Validate checks an Artifact for structural correctness.
func (a *Artifact) Validate() *Error {
	if a.ArtifactID == "" {
		return NewError(CodeInvalidParams, "artifact: artifactId is required")
	}
	if err := ValidateID(a.ArtifactID); err != nil {
		if e, ok := err.(*Error); ok {
			return e
		}
		return NewErrorf(CodeInvalidParams, "artifact: invalid artifactId: %v", err)
	}
	return nil
}

// Validate checks an AgentCard for structural correctness.
func (ac *AgentCard) Validate() *Error {
	if strings.TrimSpace(ac.Name) == "" {
		return NewError(CodeInvalidParams, "agent card: name is required")
	}
	if strings.TrimSpace(ac.URL) == "" {
		return NewError(CodeInvalidParams, "agent card: url is required")
	}
	if ac.ProtocolVersion == "" {
		return NewError(CodeInvalidParams, "agent card: protocolVersion is required")
	}
	if len(ac.Endpoints) == 0 {
		return NewError(CodeInvalidParams, "agent card: at least one endpoint is required")
	}
	for i, ep := range ac.Endpoints {
		if ep.URL == "" {
			return NewErrorf(CodeInvalidParams, "agent card: endpoints[%d].url is required", i)
		}
		if !ValidTransportKind(ep.Transport) {
			return NewErrorf(CodeInvalidParams, "agent card: endpoints[%d].transport %q is invalid", i, ep.Transport)
		}
	}
	for i, sk := range ac.Skills {
		if sk.ID == "" {
			return NewErrorf(CodeInvalidParams, "agent card: skills[%d].id is required", i)
		}
		if sk.Name == "" {
			return NewErrorf(CodeInvalidParams, "agent card: skills[%d].name is required", i)
		}
	}
	return nil
}

// Validate checks a GovernanceExtension for structural correctness.
func (g *GovernanceExtension) Validate() *Error {
	if g.SourceAgentID == "" {
		return NewError(CodeInvalidParams, "governance: sourceAgentId is required")
	}
	if g.TargetAgentID == "" {
		return NewError(CodeInvalidParams, "governance: targetAgentId is required")
	}
	if !ValidGovernanceDecision(g.Decision) {
		return NewErrorf(CodeInvalidParams, "governance: invalid decision %q", g.Decision)
	}
	if g.ChainDepth < 0 {
		return NewError(CodeInvalidParams, "governance: chainDepth must be >= 0")
	}
	if g.MaxChainDepth < 0 {
		return NewError(CodeInvalidParams, "governance: maxChainDepth must be >= 0")
	}
	if g.MaxChainDepth > 0 && g.ChainDepth > g.MaxChainDepth {
		return NewErrorf(CodeInvalidParams, "governance: chainDepth %d exceeds maxChainDepth %d", g.ChainDepth, g.MaxChainDepth)
	}
	return nil
}
