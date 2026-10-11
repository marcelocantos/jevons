// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	// maxWorkerDepth is the maximum nesting depth for jwork calls.
	// Workers at this depth cannot spawn further workers.
	maxWorkerDepth = 3

	// headingThrottleInterval is the minimum time between progress
	// heartbeats for headings at the same depth.
	headingThrottleInterval = 2 * time.Second
)

// headingPattern matches markdown headings (# through ######).
var headingPattern = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)

// registerJwork adds the jwork MCP tool to the server.
func (s *Server) registerJwork() {
	s.addTool(
		mcp.NewTool("jwork",
			mcp.WithDescription("Temporarily unavailable: Task CLI transport was removed; claudia/T190 tracks the OMP Task replacement. Calls fail without starting a worker."),
			mcp.WithString("text", mcp.Required(), mcp.Description("Task description. Must be self-contained — the worker has no prior context.")),
			mcp.WithString("cwd", mcp.Description("Working directory for the worker (defaults to the coordinator's default)")),
			mcp.WithString("model", mcp.Description("Model override (e.g. 'grok-4'; empty = provider default)")),
			mcp.WithString("provider", mcp.Description("Reserved for the OMP Task replacement (claudia/T190). Currently unavailable.")),
			mcp.WithNumber("depth", mcp.Description("Current call depth (0 = top-level). Workers increment this when calling jwork themselves. Do not set manually.")),
		),
		s.handleJwork,
	)
}

// ErrTaskCLITransportRemoved is the explicit temporary jwork outage while
// claudia/T190 builds an OMP Task replacement. Do not fall back to a vendor
// CLI: the owner accepted temporary Task loss as part of the full CLI purge.
var ErrTaskCLITransportRemoved = errors.New("task CLI transport removed; no OMP Task replacement yet (claudia/T190): jwork is temporarily unavailable")

func (s *Server) handleJwork(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if blocked := s.checkSpawnAllowed(); blocked != nil {
		return blocked, nil
	}
	args := req.GetArguments()
	text, _ := args["text"].(string)
	if strings.TrimSpace(text) == "" {
		return mcp.NewToolResultError("missing required parameter: text"), nil
	}
	depthF, _ := args["depth"].(float64)
	if int(depthF) >= maxWorkerDepth {
		return mcp.NewToolResultError(fmt.Sprintf("maximum worker depth (%d) reached — cannot spawn further workers", maxWorkerDepth)), nil
	}
	// A named refusal precedes worker tracking and provider selection: no
	// Task.Run and therefore no vendor CLI can be reached from this tool.
	return mcp.NewToolResultError(ErrTaskCLITransportRemoved.Error()), nil
}

// extractHeading parses a markdown heading line, returning the text
// and heading depth (1-6). Returns ("", 0) if not a heading.
func extractHeading(line string) (string, int) {
	m := headingPattern.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return "", 0
	}
	return m[2], len(m[1])
}

// heartbeatTracker throttles progress heartbeats by heading depth.
type heartbeatTracker struct {
	mu        sync.Mutex
	lastByLvl map[int]time.Time
}

func (h *heartbeatTracker) shouldEmit(level int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.lastByLvl == nil {
		h.lastByLvl = make(map[int]time.Time)
	}

	now := time.Now()
	if last, ok := h.lastByLvl[level]; ok && now.Sub(last) < headingThrottleInterval {
		return false
	}
	h.lastByLvl[level] = now
	return true
}

// broadcastWorkerProgress sends a progress heartbeat to the web UI
// and notifies the Jevon overseer.
func (s *Server) broadcastWorkerProgress(workerID string, depth int, heading string) {
	s.mu.Lock()
	fn := s.notifyJevon
	s.mu.Unlock()

	if fn != nil {
		fn(fmt.Sprintf("[jwork %s d%d] %s", workerID, depth, heading))
	}
}

const delegationGuidance = `IMPORTANT: You are at the maximum delegation depth. ` +
	`You MUST complete this task directly — do not attempt to delegate to sub-workers ` +
	`or call the jwork tool. If the task is too large, do what you can and clearly ` +
	`state what remains unfinished.`
