// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/supervisormem"
)

// registerSupervisorTools exposes unattended-supervisor memory and escalation
// without ~/.jevons filesystem reach or Bash blurter (🎯T704, 🎯T700).
func (s *Server) registerSupervisorTools() {
	s.addTool(
		mcp.NewTool("jevons_supervisor_notes",
			mcp.WithDescription("Read or append the unattended supervisor's durable notes at <state_dir>/supervisor/notes.md (🎯T704). action=read returns the full file; action=append_rolling requires a block starting with ### (dated ROLLING entry)."),
			mcp.WithString("action", mcp.Required(), mcp.Description("read | append_rolling")),
			mcp.WithString("block", mcp.Description("Full rolling block for append_rolling, including ### date header")),
		),
		s.handleSupervisorNotes,
	)
	s.addTool(
		mcp.NewTool("jevons_supervisor_escalate",
			mcp.WithDescription("Wake the owner via daemon blurter (jevons-supervisor app). Unattended passes MUST use this tool or POST /api/supervisor/escalate — never mcp__blurter__blurter_send or blurter through Bash (🎯T700). Severity is problem, ok, info, or warning."),
			mcp.WithString("subject", mcp.Required()),
			mcp.WithString("body", mcp.Required()),
			mcp.WithString("severity", mcp.Description("problem | ok | info | warning (default problem)")),
			mcp.WithString("key", mcp.Description("Optional blurter dedup key")),
		),
		s.handleSupervisorEscalate,
	)
}

func (s *Server) supervisorNotesStore() (*supervisormem.Store, error) {
	s.mu.Lock()
	dir := strings.TrimSpace(s.stateDir)
	s.mu.Unlock()
	if dir == "" {
		return nil, fmt.Errorf("supervisor notes: daemon state dir not configured")
	}
	return supervisormem.Open(dir)
}

func (s *Server) handleSupervisorNotes(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	store, err := s.supervisorNotesStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	action := strings.TrimSpace(str(req.GetArguments()["action"]))
	switch action {
	case "read":
		body, err := store.Read()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		lines, _ := supervisormem.RollingLineCount(body)
		out := map[string]any{"path": store.Path, "content": body, "rolling_lines": lines}
		blob, _ := json.MarshalIndent(out, "", "  ")
		return mcp.NewToolResultText(string(blob)), nil
	case "append_rolling":
		block := strings.TrimSpace(str(req.GetArguments()["block"]))
		if err := store.AppendRollingBlock(block); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		body, err := store.Read()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		lines, _ := supervisormem.RollingLineCount(body)
		return mcp.NewToolResultText(fmt.Sprintf("appended path=%s rolling_lines=%d", store.Path, lines)), nil
	default:
		return mcp.NewToolResultError("action must be read or append_rolling"), nil
	}
}

func (s *Server) handleSupervisorEscalate(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	subject := strings.TrimSpace(str(req.GetArguments()["subject"]))
	body := strings.TrimSpace(str(req.GetArguments()["body"]))
	severity := strings.TrimSpace(str(req.GetArguments()["severity"]))
	key := strings.TrimSpace(str(req.GetArguments()["key"]))
	spool, err := supervisormem.Escalate(subject, body, severity, key, nil)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("spooled: " + spool), nil
}
