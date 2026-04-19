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

package jsonrpc

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/bubblefish-tech/nexus/internal/a2a"
)

// Version is the JSON-RPC protocol version.
const Version = "2.0"

// ID is a JSON-RPC request/response identifier.
// It may be a string, an int64, or null (represented by IsNull=true).
type ID struct {
	str    string
	num    int64
	isStr  bool
	isNum  bool
	IsNull bool
}

// StringID creates a string-typed ID.
func StringID(s string) ID {
	return ID{str: s, isStr: true}
}

// NumberID creates a number-typed ID.
func NumberID(n int64) ID {
	return ID{num: n, isNum: true}
}

// NullID creates a null ID.
func NullID() ID {
	return ID{IsNull: true}
}

// String returns a human-readable representation of the ID.
func (id ID) String() string {
	switch {
	case id.isStr:
		return id.str
	case id.isNum:
		return strconv.FormatInt(id.num, 10)
	default:
		return "null"
	}
}

// StringValue returns the string value and true if the ID is a string.
func (id ID) StringValue() (string, bool) {
	return id.str, id.isStr
}

// NumberValue returns the number value and true if the ID is a number.
func (id ID) NumberValue() (int64, bool) {
	return id.num, id.isNum
}

// MarshalJSON implements json.Marshaler.
func (id ID) MarshalJSON() ([]byte, error) {
	switch {
	case id.isStr:
		return json.Marshal(id.str)
	case id.isNum:
		return json.Marshal(id.num)
	default:
		return []byte("null"), nil
	}
}

// UnmarshalJSON implements json.Unmarshaler.
func (id *ID) UnmarshalJSON(data []byte) error {
	*id = ID{} // reset

	if string(data) == "null" {
		id.IsNull = true
		return nil
	}

	// Try string first.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		id.str = s
		id.isStr = true
		return nil
	}

	// Try number.
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		id.num = n
		id.isNum = true
		return nil
	}

	return fmt.Errorf("jsonrpc: id must be string, number, or null; got %s", string(data))
}

// Request is a JSON-RPC 2.0 request object.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      ID              `json:"id"`
}

// Response is a JSON-RPC 2.0 response object.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
	ID      ID              `json:"id"`
}

// Notification is a JSON-RPC 2.0 notification (request with no id).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ErrorObject is the JSON-RPC 2.0 error object on the wire.
type ErrorObject struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *ErrorObject) Error() string {
	return fmt.Sprintf("jsonrpc: error %d: %s", e.Code, e.Message)
}

// FromA2AError converts an a2a.Error to a jsonrpc.ErrorObject.
func FromA2AError(e *a2a.Error) *ErrorObject {
	if e == nil {
		return nil
	}
	return &ErrorObject{
		Code:    e.Code,
		Message: e.Message,
		Data:    e.Data,
	}
}

// NewRequest creates a new JSON-RPC 2.0 Request. params is marshaled to JSON.
// Pass nil for no params.
func NewRequest(id ID, method string, params interface{}) (*Request, error) {
	r := &Request{
		JSONRPC: Version,
		Method:  method,
		ID:      id,
	}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("jsonrpc: marshal params: %w", err)
		}
		r.Params = data
	}
	return r, nil
}

// NewResponse creates a new JSON-RPC 2.0 success Response. result is marshaled.
func NewResponse(id ID, result interface{}) (*Response, error) {
	r := &Response{
		JSONRPC: Version,
		ID:      id,
	}
	if result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("jsonrpc: marshal result: %w", err)
		}
		r.Result = data
	}
	return r, nil
}

// NewErrorResponse creates a new JSON-RPC 2.0 error Response.
func NewErrorResponse(id ID, err *ErrorObject) *Response {
	return &Response{
		JSONRPC: Version,
		Error:   err,
		ID:      id,
	}
}

// NewNotification creates a JSON-RPC 2.0 notification (no id).
func NewNotification(method string, params interface{}) (*Notification, error) {
	n := &Notification{
		JSONRPC: Version,
		Method:  method,
	}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("jsonrpc: marshal params: %w", err)
		}
		n.Params = data
	}
	return n, nil
}
