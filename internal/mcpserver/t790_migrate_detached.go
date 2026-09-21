// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T790 — a jevons_agent_migrate move belongs to the daemon, not to the
// caller's request. A live claudia Migrate takes minutes; the 30s
// spawn-class deadline (🎯T254.5.1) answered "timed out" while the move
// went on, and the caller could not tell whether the agent had moved. The
// move now runs through the 🎯T792 detached-flight mechanism, and its
// outcome is retained so a retry (or a late reader) reads what happened
// instead of starting a second move.

const migrateOutcomeTTL = 15 * time.Minute

type migrateOutcomes struct {
	mu sync.Mutex
	m  map[string]migrateOutcome
}

type migrateOutcome struct {
	res *mcp.CallToolResult
	at  time.Time
}

func migrateKey(req mcp.CallToolRequest) string {
	return strings.TrimSpace(argString(req, "name")) + "→" + strings.TrimSpace(argString(req, "provider"))
}

func migratePendingText(key string) string {
	name, to, _ := strings.Cut(key, "→")
	return fmt.Sprintf(
		"migrate of %q → %s is still running past this call's deadline and CONTINUES in the daemon (🎯T790): "+
			"it is not cancelled. Read the outcome by repeating this exact call (it returns the recorded "+
			"result, or joins the move if still running — it never starts a second move), or watch "+
			"jevons_agent_list for the row's provider and model.", name, to)
}

// detachMigrate wraps the jevons_agent_migrate handler.
func (s *Server) detachMigrate(h toolHandler) toolHandler {
	inner := s.detachFlight(migrateKey, migratePendingText, func(ctx2 context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := h(ctx2, req)
		if err == nil && res != nil {
			o := &s.migrateOutcomes
			o.mu.Lock()
			if o.m == nil {
				o.m = map[string]migrateOutcome{}
			}
			o.m[migrateKey(req)] = migrateOutcome{res: res, at: time.Now()}
			o.mu.Unlock()
		}
		return res, err
	})
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		key := migrateKey(req)
		o := &s.migrateOutcomes
		o.mu.Lock()
		got, ok := o.m[key]
		if ok {
			delete(o.m, key) // read once
		}
		o.mu.Unlock()
		if ok && time.Since(got.at) < migrateOutcomeTTL {
			return got.res, nil
		}
		res, err := inner(ctx, req)
		if res != nil && !(res.IsError && firstText(res) == migratePendingText(key)) {
			// The caller got the real outcome; nothing is left to read later.
			o.mu.Lock()
			delete(o.m, key)
			o.mu.Unlock()
		}
		return res, err
	}
}

func firstText(r *mcp.CallToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	if tc, ok := r.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}
