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
	"context"
	"encoding/json"
	"sync"

	"github.com/BubbleFish-Nexus/internal/a2a"
)

// Handler handles a single JSON-RPC method call.
type Handler interface {
	Handle(ctx context.Context, method string, params json.RawMessage) (result interface{}, err *ErrorObject)
}

// HandlerFunc is an adapter to allow the use of ordinary functions as Handlers.
type HandlerFunc func(ctx context.Context, method string, params json.RawMessage) (interface{}, *ErrorObject)

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, method string, params json.RawMessage) (interface{}, *ErrorObject) {
	return f(ctx, method, params)
}

// MethodRouter dispatches JSON-RPC requests to registered handlers by method name.
type MethodRouter struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewMethodRouter creates an empty MethodRouter.
func NewMethodRouter() *MethodRouter {
	return &MethodRouter{
		handlers: make(map[string]Handler),
	}
}

// Register adds a handler for the given method name.
func (r *MethodRouter) Register(method string, handler Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[method] = handler
}

// RegisterFunc adds a HandlerFunc for the given method name.
func (r *MethodRouter) RegisterFunc(method string, fn HandlerFunc) {
	r.Register(method, fn)
}

// Dispatch routes a single request to its handler and returns the response.
func (r *MethodRouter) Dispatch(ctx context.Context, req *Request) *Response {
	r.mu.RLock()
	h, ok := r.handlers[req.Method]
	r.mu.RUnlock()

	if !ok {
		return NewErrorResponse(req.ID, &ErrorObject{
			Code:    a2a.CodeMethodNotFound,
			Message: "method not found: " + req.Method,
		})
	}

	result, errObj := h.Handle(ctx, req.Method, req.Params)
	if errObj != nil {
		return NewErrorResponse(req.ID, errObj)
	}

	resp, err := NewResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, &ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "failed to marshal result: " + err.Error(),
		})
	}
	return resp
}

// DispatchBatch routes a batch of requests and returns a batch of responses.
// Notifications (detected by a zero-value ID that is null) produce no response
// unless dispatched as *Request objects with explicit null IDs.
func (r *MethodRouter) DispatchBatch(ctx context.Context, reqs []*Request) []*Response {
	responses := make([]*Response, 0, len(reqs))
	for _, req := range reqs {
		resp := r.Dispatch(ctx, req)
		responses = append(responses, resp)
	}
	return responses
}
