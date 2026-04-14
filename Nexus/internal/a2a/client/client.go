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

// Package client implements the NA2A JSON-RPC client for communicating with
// remote A2A agents over any supported transport.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
	"github.com/BubbleFish-Nexus/internal/a2a/transport"
)

// reqCounter is a global atomic counter for generating unique request IDs.
var reqCounter atomic.Int64

// nextID returns a new unique JSON-RPC request ID.
func nextID() jsonrpc.ID {
	return jsonrpc.NumberID(reqCounter.Add(1))
}

// SendConfig configures the behavior of a SendMessage call.
type SendConfig struct {
	Blocking            bool     `json:"blocking,omitempty"`
	TimeoutMs           int64    `json:"timeoutMs,omitempty"`
	AcceptedOutputModes []string `json:"acceptedOutputModes,omitempty"`
}

// Client is a JSON-RPC client that communicates with a single remote A2A agent.
type Client struct {
	conn    transport.Conn
	agentID string
	logger  *slog.Logger
}

// NewClient wraps an existing transport connection as an NA2A Client.
func NewClient(conn transport.Conn, agentID string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		conn:    conn,
		agentID: agentID,
		logger:  logger,
	}
}

// AgentID returns the remote agent's identifier.
func (c *Client) AgentID() string {
	return c.agentID
}

// Close closes the underlying transport connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// sendMessageParams is the JSON-RPC params for message/send.
type sendMessageParams struct {
	Message             *a2a.Message `json:"message"`
	Skill               string       `json:"skill,omitempty"`
	Blocking            bool         `json:"blocking,omitempty"`
	TimeoutMs           int64        `json:"timeoutMs,omitempty"`
	AcceptedOutputModes []string     `json:"acceptedOutputModes,omitempty"`
}

// SendMessage sends a message to the remote agent using the "message/send"
// JSON-RPC method and returns the resulting Task.
func (c *Client) SendMessage(ctx context.Context, msg *a2a.Message, skill string, config *SendConfig) (*a2a.Task, error) {
	params := sendMessageParams{
		Message: msg,
		Skill:   skill,
	}
	if config != nil {
		params.Blocking = config.Blocking
		params.TimeoutMs = config.TimeoutMs
		params.AcceptedOutputModes = config.AcceptedOutputModes
	}

	resp, err := c.call(ctx, "message/send", params)
	if err != nil {
		return nil, err
	}

	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		return nil, fmt.Errorf("client: unmarshal task: %w", err)
	}
	return &task, nil
}

// StreamMessage sends a message to the remote agent using the "message/stream"
// JSON-RPC method and returns a channel of streaming events.
func (c *Client) StreamMessage(ctx context.Context, msg *a2a.Message, skill string, config *SendConfig) (<-chan transport.Event, error) {
	params := sendMessageParams{
		Message: msg,
		Skill:   skill,
	}
	if config != nil {
		params.Blocking = config.Blocking
		params.TimeoutMs = config.TimeoutMs
		params.AcceptedOutputModes = config.AcceptedOutputModes
	}

	req, err := jsonrpc.NewRequest(nextID(), "message/stream", params)
	if err != nil {
		return nil, fmt.Errorf("client: build request: %w", err)
	}

	c.logger.Debug("client: stream", "method", "message/stream", "agent", c.agentID)
	return c.conn.Stream(ctx, req)
}

// getTaskParams is the JSON-RPC params for tasks/get.
type getTaskParams struct {
	TaskID         string `json:"taskId"`
	IncludeHistory bool   `json:"includeHistory,omitempty"`
}

// GetTask retrieves a task by ID from the remote agent.
func (c *Client) GetTask(ctx context.Context, taskID string, includeHistory bool) (*a2a.Task, error) {
	params := getTaskParams{
		TaskID:         taskID,
		IncludeHistory: includeHistory,
	}

	resp, err := c.call(ctx, "tasks/get", params)
	if err != nil {
		return nil, err
	}

	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		return nil, fmt.Errorf("client: unmarshal task: %w", err)
	}
	return &task, nil
}

// cancelTaskParams is the JSON-RPC params for tasks/cancel.
type cancelTaskParams struct {
	TaskID string `json:"taskId"`
	Reason string `json:"reason,omitempty"`
}

// CancelTask requests cancellation of a task on the remote agent.
func (c *Client) CancelTask(ctx context.Context, taskID string, reason string) (*a2a.Task, error) {
	params := cancelTaskParams{
		TaskID: taskID,
		Reason: reason,
	}

	resp, err := c.call(ctx, "tasks/cancel", params)
	if err != nil {
		return nil, err
	}

	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		return nil, fmt.Errorf("client: unmarshal task: %w", err)
	}
	return &task, nil
}

// GetAgentCard retrieves the remote agent's card.
func (c *Client) GetAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	resp, err := c.call(ctx, "agent/card", nil)
	if err != nil {
		return nil, err
	}

	var card a2a.AgentCard
	if err := json.Unmarshal(resp.Result, &card); err != nil {
		return nil, fmt.Errorf("client: unmarshal agent card: %w", err)
	}
	return &card, nil
}

// Ping checks connectivity with the remote agent.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.call(ctx, "agent/ping", nil)
	return err
}

// call builds a JSON-RPC request, sends it, and checks for errors.
func (c *Client) call(ctx context.Context, method string, params interface{}) (*jsonrpc.Response, error) {
	req, err := jsonrpc.NewRequest(nextID(), method, params)
	if err != nil {
		return nil, fmt.Errorf("client: build request: %w", err)
	}

	c.logger.Debug("client: call", "method", method, "agent", c.agentID)

	resp, err := c.conn.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("client: send %s: %w", method, err)
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	return resp, nil
}
