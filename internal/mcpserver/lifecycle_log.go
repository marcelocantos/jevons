// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

// Component names for fleet MCP lifecycle slog + eventlog (🎯T128.1).
// Stable for rg / GET /api/logs?component=… / jevons_logs_tail filters.
const (
	compAgentLifecycle = "agent_lifecycle"
	compThread         = "thread"
	compEventPush      = "event_push"
)

// logLifecycle emits structured component + outcome attrs via the shared
// dual-write path (s.LogEvent → eventlog.Log when T128.4 is wired).
// decision is the action (start|stop|kill|spawn|push); outcome is ok|error.
// Always logs on success and failure so fleet lifecycle is greppable.
func (s *Server) logLifecycle(component, decision, outcome string, fields map[string]any) {
	out := make(map[string]any, len(fields)+3)
	for k, v := range fields {
		out[k] = v
	}
	out["outcome"] = outcome
	if outcome == "error" {
		out["level"] = "warn"
	}
	// Stable msg for journal/slog: "start" etc. is decision; override for clarity.
	if _, ok := out["msg"]; !ok && component != "" && decision != "" {
		out["msg"] = component + "." + decision
	}
	if s == nil {
		return
	}
	s.LogEvent(component, decision, out)
}
