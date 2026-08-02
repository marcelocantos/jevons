// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerEventPushTools exposes 🎯T34 event-triggered push via MCP so
// generic event sources (scripts, CI hooks, other agents) do not
// hard-code delivery paths.
func (s *Server) registerEventPushTools() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_event_push",
			mcp.WithDescription("Push an event-driven message into a target participant (butler thread or fleet agent) so it acts next (🎯T34/T114/T111.2). One deliver path by name/id: rehydrates a stopped process; returns a typed error if undeliverable. Never fails with 'no thread' when a registered agent exists. Use when a dependency landed, CI went green, a worker finished, a timer elapsed, etc. — not for owner chat (use jevons_thread_direct)."),
			mcp.WithString("target", mcp.Required(), mcp.Description("Thread ID or fleet agent name to push into")),
			mcp.WithString("event", mcp.Required(), mcp.Description("Event source/kind, e.g. ci, worker-finished, timer, dependency")),
			mcp.WithString("text", mcp.Required(), mcp.Description("What the agent should do next / what happened")),
		),
		s.handleEventPush,
	)
}

func (s *Server) handleEventPush(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.butler == nil {
		return mcp.NewToolResultError("event push: butler not configured"), nil
	}
	args := req.GetArguments()
	target := strings.TrimSpace(str(args["target"]))
	event := strings.TrimSpace(str(args["event"]))
	text := strings.TrimSpace(str(args["text"]))
	if target == "" || text == "" {
		return mcp.NewToolResultError("target and text are required"), nil
	}
	if event == "" {
		event = "unknown"
	}
	reply, err := s.butler.PushEvent(target, event, text)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Pushed event %q to %q.\n\n%s", event, target, reply)), nil
}
