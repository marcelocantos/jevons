// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// ToolProfileParam is the query parameter on the MCP URL that names the
// tool surface a seat is listed. Absent means every tool.
const ToolProfileParam = "tools"

// ToolProfileCore is the fleet-control surface: what a seat needs to see,
// steer and report on other seats, and nothing that only configures the
// daemon's ambient cycles.
//
// The whole surface was 55 tools and 58 KB of schema on 2026-09-22, carried
// in every seat's context on every turn; this profile is 21 tools and 28 KB.
// A profile changes what tools/list returns and nothing else; a tool that is
// not listed can still be called by name.
//
// No seat is given this profile yet. It was written on the theory that a
// Cursor overseer could not see jevonsmcp because the surface was too large,
// and that theory was wrong: with 21 tools listed it still could not. Three
// throwaway ACP sessions showed cursor-agent 2026.09.18 surfacing only a
// subset of ~/.cursor/mcp.json and nothing passed in session/new, over HTTP
// or stdio.
const ToolProfileCore = "core"

type toolProfileKey struct{}

// toolProfileFromRequest carries the URL's profile into the request context,
// where the tools/list filter can read it.
func toolProfileFromRequest(ctx context.Context, r *http.Request) context.Context {
	if p := strings.TrimSpace(r.URL.Query().Get(ToolProfileParam)); p != "" {
		return context.WithValue(ctx, toolProfileKey{}, p)
	}
	return ctx
}

// ambientToolPrefixes are the families outside the core surface: knobs and
// manual triggers for cycles the daemon runs on its own schedule, the
// pre-🎯T114 thread API, and the daemon's own self-test.
var ambientToolPrefixes = []string{
	"jevons_audit_", "jevons_research_", "jevons_rsi_", "jevons_idea_",
	"jevons_thread_", "self_test.",
}

// ambientTools are the single tools outside the core surface.
var ambientTools = map[string]bool{
	"jevons_staff_ops_cycle":   true,
	"jevons_sentinel_cycle":    true,
	"jevons_security_status":   true,
	"jevons_writ_exec":         true,
	"jevons_screenshot":        true,
	"jevons_cost":              true,
	"jevons_transcript_rewind": true,
	"jevons_mcp_reconnect":     true,
}

func inCoreProfile(name string) bool {
	if ambientTools[name] {
		return false
	}
	for _, p := range ambientToolPrefixes {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	return true
}

// filterToolsByProfile is the server's tools/list filter. An unknown profile
// lists everything: a seat that asks for a surface this daemon has not heard
// of must not end up with none.
func filterToolsByProfile(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	if p, _ := ctx.Value(toolProfileKey{}).(string); p != ToolProfileCore {
		return tools
	}
	out := make([]mcp.Tool, 0, len(tools))
	for _, t := range tools {
		if inCoreProfile(t.Name) {
			out = append(out, t)
		}
	}
	return out
}
