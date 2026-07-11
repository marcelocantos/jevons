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
	"github.com/marcelocantos/jevons/internal/thread"
)

// SetButler attaches the butler orchestrator and registers the thread
// management tools. Threads are the durable spine: adopt registers an
// existing session observe-only; list and status report the full set.
func (s *Server) SetButler(b *butler.Butler) {
	s.butler = b

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_thread_adopt",
			mcp.WithDescription("Adopt an existing Claude Code session in ONE call: it auto-names the thread after the session's repo, registers it, and TAKES IT OVER by default so it's immediately directable and shows in the agent panel. Just pass session_id — do not ask the owner for a name (rename later if needed). If the session is still open in its own terminal, take-over is refused with an actionable error (stop driving it first). Pass observe_only:true to only watch it without taking over."),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("Claude Code session UUID to adopt")),
			mcp.WithBoolean("observe_only", mcp.Description("Only observe (tail the transcript); do NOT take over. Default false — adoption takes over.")),
			mcp.WithString("id", mcp.Description("Optional handle override (defaults to the repo/dir name)")),
			mcp.WithString("description", mcp.Description("Optional label override (defaults to the repo/dir name)")),
			mcp.WithString("workdir", mcp.Description("Optional working directory override (defaults to the session's own cwd)")),
		),
		s.handleThreadAdopt,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_thread_remove",
			mcp.WithDescription("Remove a thread: stop and deregister any owned process (the underlying Claude session on disk is left intact) and drop the record. Use to clean up duplicate or unwanted threads."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Thread ID to remove")),
		),
		s.handleThreadRemove,
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
			mcp.WithDescription("Spawn a new thread the butler owns end-to-end and start its agent process (Grok by default). The thread is durable — it survives restarts and its idle process is stopped resumably and rehydrated on demand."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Unique thread handle (e.g. 'tern-po', 'maze-rebuild')")),
			mcp.WithString("workdir", mcp.Required(), mcp.Description("Working directory (e.g. '~/work/github.com/marcelocantos/tern')")),
			mcp.WithString("description", mcp.Description("The owner's work-language label")),
			mcp.WithString("model", mcp.Description("Model override (e.g. 'grok-4', 'opus', 'sonnet')")),
			mcp.WithString("provider", mcp.Description("Agent harness: grok (default), claude, or codex")),
		),
		s.handleThreadSpawn,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_thread_takeover",
			mcp.WithDescription("Take over an adopted (observe-only) thread so Jevons owns and can direct it: launches Jevons's own process resuming the session. Refuses if the session is still being driven in another terminal — the owner must stop driving it first (two claude processes on one session corrupt it). The session is preserved, so a taken-over thread can later be handed back."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Thread ID (an adopted thread)")),
		),
		s.handleThreadTakeover,
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
	sessionID := str(args["session_id"])
	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}
	observeOnly, _ := args["observe_only"].(bool)

	th, err := s.butler.Adopt(butler.AdoptArgs{
		SessionID:   sessionID,
		ObserveOnly: observeOnly,
		ID:          str(args["id"]),
		WorkDir:     str(args["workdir"]),
		Description: str(args["description"]),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	verb := "took over (owned, directable)"
	if th.Kind == thread.KindAdopted {
		verb = "adopted observe-only"
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"Thread %q %s — session %s in %s.",
		th.ID, verb, short(th.SessionID), th.WorkDir)), nil
}

func (s *Server) handleThreadRemove(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := str(req.GetArguments()["id"])
	if id == "" {
		return mcp.NewToolResultError("id is required"), nil
	}
	if err := s.butler.Remove(id); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Removed thread %q.", id)), nil
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
		Provider:    str(args["provider"]),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"Spawned thread %q (session %s) in %s.", th.ID, short(th.SessionID), th.WorkDir)), nil
}

func (s *Server) handleThreadTakeover(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := str(req.GetArguments()["id"])
	if id == "" {
		return mcp.NewToolResultError("id is required"), nil
	}
	th, err := s.butler.TakeOver(id)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"Took over thread %q — now owned and directable (session %s in %s).",
		th.ID, short(th.SessionID), th.WorkDir)), nil
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
