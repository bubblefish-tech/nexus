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
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
)

// taskIDParams is used by methods that take a single taskId parameter.
type taskIDParams struct {
	TaskID string `json:"taskId"`
}

// pushNotificationConfigSetParams is used by tasks/pushNotificationConfig/set.
type pushNotificationConfigSetParams struct {
	TaskID string                 `json:"taskId"`
	Config PushNotificationConfig `json:"config"`
}

// pushNotificationConfigGetParams is used by tasks/pushNotificationConfig/get.
type pushNotificationConfigGetParams struct {
	TaskID string `json:"taskId"`
}

// pushConfigMu protects the pushConfigs map on the server.
var pushConfigMu sync.RWMutex

// handleTasksGet returns a task by ID from the store.
func (s *Server) handleTasksGet(ctx context.Context, _ string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	var p taskIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: fmt.Sprintf("invalid params: %v", err),
		}
	}

	if p.TaskID == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "taskId is required",
		}
	}

	if s.taskStore == nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "no task store configured",
		}
	}

	task, err := s.taskStore.GetTask(ctx, p.TaskID)
	if err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeTaskNotFound,
			Message: fmt.Sprintf("task %q not found", p.TaskID),
		}
	}

	return task, nil
}

// handleTasksCancel cancels an active task.
func (s *Server) handleTasksCancel(ctx context.Context, _ string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	var p taskIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: fmt.Sprintf("invalid params: %v", err),
		}
	}

	if p.TaskID == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "taskId is required",
		}
	}

	if s.taskStore == nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "no task store configured",
		}
	}

	task, err := s.taskStore.GetTask(ctx, p.TaskID)
	if err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeTaskNotFound,
			Message: fmt.Sprintf("task %q not found", p.TaskID),
		}
	}

	if task.Status.State.IsTerminal() {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeTaskNotCancelable,
			Message: fmt.Sprintf("task %q is in terminal state %q and cannot be canceled", p.TaskID, task.Status.State),
		}
	}

	cancelStatus := a2a.TaskStatus{State: a2a.TaskStateCanceled, Timestamp: a2a.Now()}
	if err := s.taskStore.UpdateTaskStatus(ctx, p.TaskID, cancelStatus); err != nil {
		s.logger.ErrorContext(ctx, "task store update status error", "error", err)
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "failed to cancel task",
		}
	}
	task.Status = cancelStatus

	if s.auditSink != nil {
		_ = s.auditSink.LogTaskEvent(ctx, p.TaskID, "task.canceled", nil)
	}

	return task, nil
}

// handleTasksResubscribe returns the current task state.
// Full streaming resubscription is wired in A2A.6.
func (s *Server) handleTasksResubscribe(ctx context.Context, _ string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	var p taskIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: fmt.Sprintf("invalid params: %v", err),
		}
	}

	if p.TaskID == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "taskId is required",
		}
	}

	if s.taskStore == nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "no task store configured",
		}
	}

	task, err := s.taskStore.GetTask(ctx, p.TaskID)
	if err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeTaskNotFound,
			Message: fmt.Sprintf("task %q not found", p.TaskID),
		}
	}

	return task, nil
}

// handleTasksPushNotificationConfigSet stores push notification config for a task.
func (s *Server) handleTasksPushNotificationConfigSet(ctx context.Context, _ string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	var p pushNotificationConfigSetParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: fmt.Sprintf("invalid params: %v", err),
		}
	}

	if p.TaskID == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "taskId is required",
		}
	}

	if p.Config.URL == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "config.url is required",
		}
	}

	// Verify the task exists.
	if s.taskStore != nil {
		if _, err := s.taskStore.GetTask(ctx, p.TaskID); err != nil {
			return nil, &jsonrpc.ErrorObject{
				Code:    a2a.CodeTaskNotFound,
				Message: fmt.Sprintf("task %q not found", p.TaskID),
			}
		}
	}

	pushConfigMu.Lock()
	s.pushConfigs[p.TaskID] = p.Config
	pushConfigMu.Unlock()

	return &p.Config, nil
}

// handleTasksPushNotificationConfigGet retrieves push notification config for a task.
func (s *Server) handleTasksPushNotificationConfigGet(_ context.Context, _ string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	var p pushNotificationConfigGetParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: fmt.Sprintf("invalid params: %v", err),
		}
	}

	if p.TaskID == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "taskId is required",
		}
	}

	pushConfigMu.RLock()
	cfg, ok := s.pushConfigs[p.TaskID]
	pushConfigMu.RUnlock()

	if !ok {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeTaskNotFound,
			Message: fmt.Sprintf("no push notification config for task %q", p.TaskID),
		}
	}

	return &cfg, nil
}
