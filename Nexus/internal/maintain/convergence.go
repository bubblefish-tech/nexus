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

package maintain

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/maintain/registry"
)

// ReconcileResult records the outcome of one reconciliation attempt for a tool.
type ReconcileResult struct {
	Tool    string
	IssueID string // "" when skipped
	Steps   int    // number of recipe steps executed
	Err     error  // nil on success or skip
	Skipped bool   // true when no issue applies or tool is already converged
}

// Reconciler drives the convergence loop: for each tracked tool, it identifies
// the active issue (drift or liveness failure), selects the matching fix recipe
// from the connector registry, and executes it as an ACID transaction.
//
// This is the Kubernetes-style declarative reconciliation loop applied to the
// local AI tool ecosystem. The reconciler is purely additive — it never deletes
// config keys that exist in the actual state but are absent from the desired state.
type Reconciler struct {
	twin     *EnvironmentTwin
	registry *registry.Registry
}

// NewReconciler wires a Reconciler to the given twin and registry.
func NewReconciler(twin *EnvironmentTwin, reg *registry.Registry) *Reconciler {
	return &Reconciler{twin: twin, registry: reg}
}

// Reconcile performs one convergence pass over every tracked tool.
// Tools already in desired state are skipped. The call is synchronous and
// sequential — one tool at a time so journal files stay readable.
func (r *Reconciler) Reconcile(ctx context.Context) []ReconcileResult {
	tools := r.twin.AllTools()
	results := make([]ReconcileResult, 0, len(tools))
	for _, ts := range tools {
		results = append(results, r.reconcileTool(ctx, ts))
	}
	return results
}

// Run calls Reconcile on the given interval until ctx is cancelled.
// Per-tool errors are logged but do not terminate the loop — the reconciler
// retries on the next tick (immune memory pattern: learn from failures,
// keep trying until convergence).
func (r *Reconciler) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			for _, res := range r.Reconcile(ctx) {
				if res.Err != nil {
					slog.WarnContext(ctx, "maintain: reconcile error",
						"tool", res.Tool, "issue", res.IssueID, "err", res.Err)
				}
			}
		}
	}
}

// reconcileTool performs one convergence attempt for a single tool.
func (r *Reconciler) reconcileTool(ctx context.Context, ts *ToolState) ReconcileResult {
	res := ReconcileResult{Tool: ts.Name}

	conn, ok := r.registry.ConnectorFor(ts.Name)
	if !ok {
		res.Skipped = true
		return res
	}

	// Refresh desired state from the registry MCP template before evaluating drift.
	if desired := r.registry.MCPDesiredState(ts.Name); desired != nil {
		r.twin.ComputeDesiredState(ts, desired)
	}

	needsLiveness := ts.Status == "stopped" || ts.Status == "unknown"
	needsConfig := len(ts.Drift) > 0
	if !needsLiveness && !needsConfig {
		res.Skipped = true
		return res
	}

	issueID := selectIssue(ts, conn)
	if issueID == "" {
		res.Skipped = true
		return res
	}

	rawSteps := r.registry.RecipeFor(ts.Name, issueID)
	if len(rawSteps) == 0 {
		res.Skipped = true
		return res
	}

	steps := convertSteps(rawSteps, ts, conn)
	tx := NewTransaction(ts.Name, steps)
	res.IssueID = issueID
	res.Steps = len(steps)
	res.Err = tx.Execute(ctx)

	if res.Err == nil {
		slog.InfoContext(ctx, "maintain: converged",
			"tool", ts.Name, "issue", issueID, "steps", len(steps))
	}
	return res
}

// selectIssue picks the best-matching known issue for the current tool state.
// Liveness issues (recipe contains restart_process or wait_for_port) are matched
// when the tool is stopped/unknown. Config issues are matched when drift is present.
// Returns "" when no applicable issue is found.
func selectIssue(ts *ToolState, c *registry.Connector) string {
	needsLiveness := ts.Status == "stopped" || ts.Status == "unknown"
	needsConfig := len(ts.Drift) > 0
	for _, ki := range c.KnownIssues {
		kind := issueKind(ki)
		if kind == "liveness" && needsLiveness {
			return ki.ID
		}
		if kind == "config" && needsConfig {
			return ki.ID
		}
	}
	return ""
}

// issueKind classifies a known issue by inspecting its fix recipe.
// An issue is "liveness" if it contains a process-restart or port-wait step;
// otherwise it is "config".
func issueKind(ki registry.KnownIssue) string {
	for _, step := range ki.FixRecipe {
		switch step.Action {
		case string(ActionRestartProcess), string(ActionWaitForPort):
			return "liveness"
		}
	}
	return "config"
}

// convertSteps converts []registry.RawStep → []Step, applying template
// substitution to string param values (e.g. "{{config_path}}" → actual path).
func convertSteps(raw []registry.RawStep, ts *ToolState, c *registry.Connector) []Step {
	vars := templateVars(ts, c)
	steps := make([]Step, len(raw))
	for i, r := range raw {
		steps[i] = Step{
			Action: ActionType(r.Action),
			Params: substituteParams(r.Params, vars),
		}
	}
	return steps
}

// templateVars builds the variable substitution map for a tool's recipe.
func templateVars(ts *ToolState, c *registry.Connector) map[string]string {
	vars := map[string]string{
		"{{tool_name}}": ts.Name,
		"{{endpoint}}":  c.Detection.Endpoint,
	}
	if len(ts.ConfigPaths) > 0 {
		vars["{{config_path}}"] = ts.ConfigPaths[0]
	}
	return vars
}

// substituteParams returns a shallow copy of params with string values
// substituted according to vars. Non-string values are passed through unchanged.
func substituteParams(params map[string]any, vars map[string]string) map[string]any {
	if len(params) == 0 {
		return params
	}
	out := make(map[string]any, len(params))
	for k, v := range params {
		if s, ok := v.(string); ok {
			for tmpl, val := range vars {
				s = strings.ReplaceAll(s, tmpl, val)
			}
			out[k] = s
		} else {
			out[k] = v
		}
	}
	return out
}
