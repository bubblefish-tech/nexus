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

import "fmt"

// JSON-RPC 2.0 standard error codes.
const (
	CodeInvalidRequest  = -32600
	CodeMethodNotFound  = -32601
	CodeInvalidParams   = -32602
	CodeInternalError   = -32603
)

// NA2A-specific error codes (§12).
const (
	CodeUnauthenticated       = -32000
	CodePermissionDenied      = -32001
	CodeApprovalRequired      = -32002
	CodeRateLimited           = -32003
	CodeSkillNotFound         = -32004
	CodeInvalidInput          = -32005
	CodeTaskNotFound          = -32006
	CodeTaskNotCancelable     = -32007
	CodeTransportError        = -32008
	CodeCapabilityNotDeclared = -32009
	CodeExtensionRequired     = -32010
	CodeIncompatibleVersion   = -32011
	CodeAgentOffline          = -32012
)

// errorName maps error codes to their canonical names.
var errorName = map[int]string{
	CodeInvalidRequest:        "INVALID_REQUEST",
	CodeMethodNotFound:        "METHOD_NOT_FOUND",
	CodeInvalidParams:         "INVALID_PARAMS",
	CodeInternalError:         "INTERNAL_ERROR",
	CodeUnauthenticated:       "UNAUTHENTICATED",
	CodePermissionDenied:      "PERMISSION_DENIED",
	CodeApprovalRequired:      "APPROVAL_REQUIRED",
	CodeRateLimited:           "RATE_LIMITED",
	CodeSkillNotFound:         "SKILL_NOT_FOUND",
	CodeInvalidInput:          "INVALID_INPUT",
	CodeTaskNotFound:          "TASK_NOT_FOUND",
	CodeTaskNotCancelable:     "TASK_NOT_CANCELABLE",
	CodeTransportError:        "TRANSPORT_ERROR",
	CodeCapabilityNotDeclared: "CAPABILITY_NOT_DECLARED",
	CodeExtensionRequired:     "EXTENSION_REQUIRED",
	CodeIncompatibleVersion:   "INCOMPATIBLE_VERSION",
	CodeAgentOffline:          "AGENT_OFFLINE",
}

// AllErrorCodes returns every defined error code for enumeration and testing.
func AllErrorCodes() []int {
	codes := make([]int, 0, len(errorName))
	for c := range errorName {
		codes = append(codes, c)
	}
	return codes
}

// ErrorCodeName returns the canonical name for an error code, or "" if unknown.
func ErrorCodeName(code int) string {
	return errorName[code]
}

// Error is a structured NA2A error matching the JSON-RPC 2.0 error object
// with an optional Data field for trace IDs and additional context.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *Error) Error() string {
	name := errorName[e.Code]
	if name == "" {
		name = "UNKNOWN"
	}
	return fmt.Sprintf("a2a: %s (%d): %s", name, e.Code, e.Message)
}

// NewError creates an Error with the given code and message.
func NewError(code int, message string) *Error {
	return &Error{Code: code, Message: message}
}

// NewErrorf creates an Error with the given code and formatted message.
func NewErrorf(code int, format string, args ...interface{}) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// NewErrorWithData creates an Error with the given code, message, and data.
func NewErrorWithData(code int, message string, data interface{}) *Error {
	return &Error{Code: code, Message: message, Data: data}
}

// ErrorData is the standard data payload for NA2A errors.
// All error objects include a traceId pointing to the audit chain entry.
type ErrorData struct {
	TraceID string `json:"traceId,omitempty"`
}
