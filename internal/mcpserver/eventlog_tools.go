// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/eventlog"
)

// EventLogTailFunc returns newest-first events and the journal path.
type EventLogTailFunc func(opt eventlog.TailOptions) (events []eventlog.Event, path string, err error)

// SetEventLogTailer wires jevons_logs_tail (🎯T120 product introspection).
func (s *Server) SetEventLogTailer(fn EventLogTailFunc) {
	s.eventLogTail = fn
	if fn != nil {
		s.registerEventLogTools()
	}
}

func (s *Server) registerEventLogTools() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_logs_tail",
			mcp.WithDescription("Tail durable product event logs under state_dir/logs/events.jsonl (browser decisions + lifecycle). Newest first. Use component/decision filters (e.g. component=thread_route, decision=match). Logs first — not DevTools-only (🎯T120)."),
			mcp.WithNumber("limit", mcp.Description("Max events (default 100, max 2000)")),
			mcp.WithString("component", mcp.Description("Filter by component (thread_route, attention, send_queue, browser, …)")),
			mcp.WithString("decision", mcp.Description("Filter by decision enum (match, enqueue, send, …)")),
			mcp.WithString("source", mcp.Description("browser | server")),
			mcp.WithString("q", mcp.Description("Substring match on msg (case-insensitive)")),
		),
		s.handleLogsTail,
	)
}

func (s *Server) handleLogsTail(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.eventLogTail == nil {
		return mcp.NewToolResultError("event log not configured"), nil
	}
	args, _ := req.Params.Arguments.(map[string]any)
	limit := 100
	if n, ok := args["limit"].(float64); ok && n > 0 {
		limit = int(n)
	}
	comp, _ := args["component"].(string)
	dec, _ := args["decision"].(string)
	src, _ := args["source"].(string)
	q, _ := args["q"].(string)

	events, path, err := s.eventLogTail(eventlog.TailOptions{
		Limit:     limit,
		Component: comp,
		Decision:  dec,
		Source:    src,
		Contains:  q,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("logs tail: %v", err)), nil
	}
	if events == nil {
		events = []eventlog.Event{}
	}
	payload := map[string]any{
		"path":   path,
		"count":  len(events),
		"events": events,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}
