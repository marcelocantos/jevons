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
			mcp.WithDescription("Adopt an existing Grok Build session in ONE call: auto-names the thread after the session's repo, registers it, and TAKES IT OVER by default. Pass session_id only. If still open in another terminal, take-over is refused. Pass observe_only:true to watch without taking over."),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("Grok session id to adopt")),
			mcp.WithBoolean("observe_only", mcp.Description("Only observe (tail the transcript); do NOT take over. Default false — adoption takes over.")),
			mcp.WithString("id", mcp.Description("Optional handle override (defaults to the repo/dir name)")),
			mcp.WithString("description", mcp.Description("Optional label override (defaults to the repo/dir name)")),
			mcp.WithString("workdir", mcp.Description("Optional working directory override (defaults to the session's own cwd)")),
		),
		s.handleThreadAdopt,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_thread_remove",
			mcp.WithDescription("Remove a thread: stop and deregister any owned process (the underlying Grok session on disk is left intact) and drop the record."),
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
			mcp.WithDescription("Spawn a new Grok thread the butler owns end-to-end. Durable across restarts; idle process is stopped and rehydrated on demand."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Unique thread handle (e.g. 'tern-po', 'maze-rebuild')")),
			mcp.WithString("workdir", mcp.Required(), mcp.Description("Working directory (absolute or ~-relative repo path)")),
			mcp.WithString("description", mcp.Description("The owner's work-language label")),
			mcp.WithString("model", mcp.Description("Model override (e.g. 'grok-4'; empty = Grok default)")),
		),
		s.handleThreadSpawn,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_thread_takeover",
			mcp.WithDescription("Take over an adopted (observe-only) thread so Jevons owns and can direct it. Refuses if another process still holds the session. The session id is preserved."),
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
		if n := len(ts.Status.Integrity); n > 0 {
			fmt.Fprintf(&b, "  ⚠ integrity issues: %d\n", n)
		}
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
	if n := len(ts.Status.Integrity); n > 0 {
		fmt.Fprintf(&b, "Integrity:   %d issue(s) — %s\n", n, ts.Status.Integrity[0].Kind)
		for _, iss := range ts.Status.Integrity {
			fmt.Fprintf(&b, "  - turn %d: %s (%s)\n", iss.TurnIndex, iss.Kind, iss.Detail)
		}
	}
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
