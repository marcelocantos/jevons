// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/butler"
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
		s.logLifecycle(compEventPush, "push", "error", map[string]any{
			"err": "butler not configured",
		})
		return mcp.NewToolResultError("event push: butler not configured"), nil
	}
	args := req.GetArguments()
	target := strings.TrimSpace(str(args["target"]))
	event := strings.TrimSpace(str(args["event"]))
	text := strings.TrimSpace(str(args["text"]))
	life := map[string]any{"target": target, "event": event}
	if target == "" || text == "" {
		s.logLifecycle(compEventPush, "push", "error", map[string]any{
			"target": target, "err": "target and text are required",
		})
		return mcp.NewToolResultError("target and text are required"), nil
	}
	if event == "" {
		event = "unknown"
		life["event"] = event
	}
	wire := butler.FormatEventPush(event, text)

	// 🎯T428. This tool does not pass through deliverToOverseer, so the
	// guard is applied here against the same ledger and the same wire
	// bytes. Non-overseer targets are unaffected.
	var ticket notifyReplayTicket
	if s.isOverseerAgent(target) {
		var dec notifyReplayDecision
		ticket, dec = s.notifyReplays().Offer(wire)
		if !dec.Admit {
			life["suppressed_replay"] = dec.Reason
			life["offers"] = dec.Offers
			s.logLifecycle(compEventPush, "push", "ok", life)
			return mcp.NewToolResultText(
				describeReplaySuppression(target, dec, s.notifyReplays().Now())), nil
		}
	}

	reply, err := s.butler.PushEvent(target, event, text)
	if err != nil {
		ticket.Abandon()
		life["err"] = err.Error()
		life["failure_class"] = agenterr.Classify(err).String()
		s.logLifecycle(compEventPush, "push", "error", life)
		// 🎯T283: same classification the owner chat path gets.
		return toolFailure("event_push", target, err), nil
	}
	ticket.Settle(true)
	if class, ownerMsg, ok := agenterr.ReplyFailure(reply); ok {
		life["failure_class"] = class.String()
		s.logLifecycle(compEventPush, "push", "error", life)
		logProviderFailure("event_push", target, class, reply)
		return mcp.NewToolResultError(ownerMsg), nil
	}
	s.logLifecycle(compEventPush, "push", "ok", life)
	return mcp.NewToolResultText(fmt.Sprintf("Pushed event %q to %q.\n\n%s", event, target, reply)), nil
}
