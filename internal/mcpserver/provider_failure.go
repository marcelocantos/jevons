// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// Provider failure classification on the MCP direct/deliver paths (🎯T283).
//
// The owner chat path has classified provider failures since 🎯T237
// (server/chat_wire.go): a bare "Internal error" becomes actionable copy
// carrying a class code. Callers of jevons_thread_direct, jevons_event_push,
// jevons_agent_send and jwork got the raw string instead, so an outage was
// indistinguishable from a product defect — an agent told "no marker file, no
// token in reply" blames the shell tool when the backend was simply down.
//
// These helpers give those paths the same treatment and the same slog schema
// (component/failure_class/transient/surface/raw) so 🎯T236 recovery consumers
// read one shape across every surface.

// logProviderFailure emits the shared structured failure line.
func logProviderFailure(surface, target string, class agenterr.Class, raw string) {
	slog.Warn("provider_failure",
		"component", "provider_failure",
		"failure_class", class.String(),
		"transient", class.IsTransient(),
		"surface", surface,
		"target", target,
		"raw", truncate(strings.TrimSpace(raw), 400),
	)
}

// toolFailure converts an error from a direct/deliver path into an MCP error
// result. Classified provider failures get owner-visible copy naming the class;
// everything else (missing thread, bad arguments) passes through unchanged so
// existing callers and their tests keep the wording they match on.
func toolFailure(surface, target string, err error) *mcp.CallToolResult {
	if err == nil {
		return nil
	}
	class, ownerMsg := agenterr.ClassifyAndFormat(err)
	if !class.IsFailure() {
		return mcp.NewToolResultError(err.Error())
	}
	logProviderFailure(surface, target, class, err.Error())
	return mcp.NewToolResultError(ownerMsg)
}

// jworkFailure decides whether a finished jwork run is a provider outage
// rather than a worker result (🎯T283). errClass/errRaw are the strongest
// failure seen on the task's error events; result is the worker's output.
//
// A worker that produced nothing but a provider failure reports the outage, so
// the caller cannot read a backend being down as "the tool did not run". An
// error event followed by real output is a recovered run, not an outage: the
// result stands and the class rides in structuredContent instead.
func jworkFailure(errClass agenterr.Class, errRaw, result string) (agenterr.Class, string) {
	if class, ownerMsg, ok := agenterr.ReplyFailure(result); ok {
		return class, ownerMsg
	}
	if errClass.IsFailure() && strings.TrimSpace(result) == "" {
		return errClass, agenterr.OwnerCopy(errClass, errRaw)
	}
	return agenterr.ClassNone, ""
}

// replyFailure converts a reply that is nothing but a provider failure signal
// into an MCP error result, or returns nil when the reply is real work.
// This is the arm that matters for Grok ACP, which delivers "Internal error"
// as the assistant turn rather than as a transport error.
func replyFailure(surface, target, reply string) *mcp.CallToolResult {
	class, ownerMsg, ok := agenterr.ReplyFailure(reply)
	if !ok {
		return nil
	}
	logProviderFailure(surface, target, class, reply)
	return mcp.NewToolResultError(ownerMsg)
}
