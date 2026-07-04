// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/butler"
)

// SetButler attaches the butler orchestrator and registers the thread
// management tools. Threads are the durable spine: adopt registers an
// existing session observe-only; list and status report the full set.
func (s *Server) SetButler(b *butler.Butler) {
	s.butler = b

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_thread_adopt",
			mcp.WithDescription("Adopt an existing Claude Code session as a durable thread, observe-only. Non-invasive: the session's process is never taken over — its transcript is tailed for status. Use this to grandfather sessions the owner already has running."),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("Claude Code session UUID to adopt")),
			mcp.WithString("description", mcp.Description("The owner's work-language label, e.g. 'the multimaze2 rebuild'")),
			mcp.WithString("id", mcp.Description("Optional stable handle for the thread (defaults to the session id)")),
			mcp.WithString("workdir", mcp.Description("Optional working directory override (defaults to the session's own cwd)")),
		),
		s.handleThreadAdopt,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_thread_list",
			mcp.WithDescription("List all threads (adopted and spawned) with their derived status: active/working/blocked/done/idle plus a recent-activity summary."),
		),
		s.handleThreadList,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_thread_status",
			mcp.WithDescription("Get the current status and recent-activity summary of a single thread, derived on demand from its transcript."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Thread ID")),
		),
		s.handleThreadStatus,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_thread_spawn",
			mcp.WithDescription("Spawn a new thread the butler owns end-to-end and start its Claude Code process. The thread is durable — it survives restarts and its idle process is stopped resumably and rehydrated on demand."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Unique thread handle (e.g. 'tern-po', 'maze-rebuild')")),
			mcp.WithString("workdir", mcp.Required(), mcp.Description("Working directory (e.g. '~/work/github.com/marcelocantos/tern')")),
			mcp.WithString("description", mcp.Description("The owner's work-language label")),
			mcp.WithString("model", mcp.Description("Model override (e.g. 'opus', 'sonnet')")),
		),
		s.handleThreadSpawn,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_thread_direct",
			mcp.WithDescription("Direct a thread: deliver a message and return its reply. If the thread's process was stopped or aged out, it is transparently rehydrated first; if it cannot be reached, a distinct error is returned (never a silent timeout). Observe-only adopted threads must be taken over before they can be directed."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Thread ID")),
			mcp.WithString("text", mcp.Required(), mcp.Description("Message to deliver")),
		),
		s.handleThreadDirect,
	)
}

func (s *Server) handleThreadAdopt(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}

	th, err := s.butler.Adopt(butler.AdoptArgs{
		SessionID:   sessionID,
		ID:          str(args["id"]),
		WorkDir:     str(args["workdir"]),
		Description: str(args["description"]),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"Adopted thread %q (observe-only) — session %s in %s.",
		th.ID, th.SessionID[:8], th.WorkDir)), nil
}

func (s *Server) handleThreadList(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	threads := s.butler.List()
	if len(threads) == 0 {
		return mcp.NewToolResultText("No threads. Adopt a session with jevons_thread_adopt or spawn one."), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%-20s  %-8s  %-8s  %s\n", "THREAD", "KIND", "STATE", "SUMMARY")
	for _, ts := range threads {
		fmt.Fprintf(&b, "%-20s  %-8s  %-8s  %s\n",
			ts.Thread.ID, ts.Thread.Kind, ts.Status.State, ts.Status.Summary)
	}
	return mcp.NewToolResultText(b.String()), nil
}

func (s *Server) handleThreadStatus(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := req.GetArguments()["id"].(string)
	if id == "" {
		return mcp.NewToolResultError("id is required"), nil
	}

	ts, err := s.butler.Status(id)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Thread:      %s (%s)\n", ts.Thread.ID, ts.Thread.Kind)
	fmt.Fprintf(&b, "WorkDir:     %s\n", ts.Thread.WorkDir)
	fmt.Fprintf(&b, "Session:     %s\n", ts.Thread.SessionID)
	fmt.Fprintf(&b, "State:       %s\n", ts.Status.State)
	fmt.Fprintf(&b, "Summary:     %s\n", ts.Status.Summary)
	if ts.Thread.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", ts.Thread.Description)
	}
	return mcp.NewToolResultText(b.String()), nil
}

func (s *Server) handleThreadSpawn(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	id := str(args["id"])
	workdir := str(args["workdir"])
	if id == "" || workdir == "" {
		return mcp.NewToolResultError("id and workdir are required"), nil
	}
	if strings.HasPrefix(workdir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			workdir = home + workdir[1:]
		}
	}

	th, err := s.butler.Spawn(butler.SpawnArgs{
		ID:          id,
		WorkDir:     workdir,
		Description: str(args["description"]),
		Model:       str(args["model"]),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"Spawned thread %q (session %s) in %s.", th.ID, short(th.SessionID), th.WorkDir)), nil
}

func (s *Server) handleThreadDirect(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	id := str(args["id"])
	text := str(args["text"])
	if id == "" || text == "" {
		return mcp.NewToolResultError("id and text are required"), nil
	}

	reply, err := s.butler.Direct(id, text)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(reply), nil
}

// str is a nil-safe cast of an MCP argument to string.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// short renders the first 8 characters of a session id for display.
func short(sessionID string) string {
	if len(sessionID) > 8 {
		return sessionID[:8]
	}
	return sessionID
}
