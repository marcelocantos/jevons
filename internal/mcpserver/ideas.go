// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/idea"
)

// SetIdeaStateDir wires durable idea intake tools (🎯T325.3) against
// state_dir/ideas.json. Empty path leaves tools unregistered.
func (s *Server) SetIdeaStateDir(stateDir string) {
	if s == nil {
		return
	}
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return
	}
	if strings.HasPrefix(stateDir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			stateDir = filepath.Join(home, stateDir[2:])
		}
	}
	s.ideaStateDir = stateDir
	s.registerIdeaTools()
}

func (s *Server) registerIdeaTools() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_idea_capture",
			mcp.WithDescription("Capture an owner spark into the durable idea ledger (🎯T325.3) so it does not evaporate in scrollback. Returns the idea id and disposition (inbox/hold). Use after capture:/idea: or mid-chat ideation; triage later with jevons_idea_triage. List with jevons_idea_list."),
			mcp.WithString("text", mcp.Required(), mcp.Description("Spark body to retain")),
			mcp.WithString("source", mcp.Description("capture | idea | aside | chat | mcp (default mcp)")),
			mcp.WithString("domain", mcp.Description("Optional domain tag (health/finance/…); parked life domains land as hold)")),
			mcp.WithString("aside_id", mcp.Description("Optional dual-write aside id when capture: opened a fleet aside")),
		),
		s.handleIdeaCapture,
	)
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_idea_list",
			mcp.WithDescription("List durable captured ideas (🎯T325.3 listable surface). Optional disposition filter: inbox|file|park|hold|drop."),
			mcp.WithString("disposition", mcp.Description("Optional filter (inbox|file|park|hold|drop)")),
		),
		s.handleIdeaList,
	)
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_idea_triage",
			mcp.WithDescription("Triage a captured idea (🎯T325.3): product-shaped→file (+ then jevons_target_file + T193 if Build); needs-owner/design→park; life-domain parked→hold; rare noise→drop. Does not auto-file bullseye — file disposition is a ceremony marker; call jevons_target_file yourself for product leaves."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Idea id from capture/list")),
			mcp.WithString("disposition", mcp.Required(), mcp.Description("inbox | file | park | hold | drop")),
			mcp.WithString("note", mcp.Description("Triage reason / opportunity-cost note")),
			mcp.WithString("domain", mcp.Description("Optional domain tag update")),
			mcp.WithString("target_id", mcp.Description("When filing, the 🎯 id after jevons_target_file")),
		),
		s.handleIdeaTriage,
	)
}

func (s *Server) ideaStore() (*idea.Store, error) {
	if s == nil || strings.TrimSpace(s.ideaStateDir) == "" {
		return nil, fmt.Errorf("idea ledger not configured (state_dir)")
	}
	return idea.Open(s.ideaStateDir)
}

func (s *Server) handleIdeaCapture(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	text := strings.TrimSpace(str(args["text"]))
	if text == "" {
		return mcp.NewToolResultError("text is required"), nil
	}
	st, err := s.ideaStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	src := idea.SourceMCP
	if v := strings.TrimSpace(str(args["source"])); v != "" {
		src = idea.NormalizeSource(v)
	}
	rec, err := st.Capture(idea.CaptureArgs{
		Text:    text,
		Source:  src,
		Domain:  str(args["domain"]),
		AsideID: str(args["aside_id"]),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"Captured idea %s disposition=%s source=%s\nTitle: %s\nCeremony: %s\nText: %s\n",
		rec.ID, rec.Disposition, rec.Source, rec.Title, idea.NextCeremony(rec.Disposition), rec.Text,
	)), nil
}

func (s *Server) handleIdeaList(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	st, err := s.ideaStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	disp := str(req.GetArguments()["disposition"])
	list, err := st.List(disp)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Ideas (%d):\n", len(list)))
	if len(list) == 0 {
		b.WriteString("  (empty)\n")
		return mcp.NewToolResultText(b.String()), nil
	}
	for _, r := range list {
		b.WriteString(fmt.Sprintf("  %s [%s] %s", r.ID, r.Disposition, r.Title))
		if r.Domain != "" {
			b.WriteString(fmt.Sprintf(" domain=%s", r.Domain))
		}
		if r.TargetID != "" {
			b.WriteString(fmt.Sprintf(" 🎯%s", r.TargetID))
		}
		b.WriteByte('\n')
	}
	return mcp.NewToolResultText(b.String()), nil
}

func (s *Server) handleIdeaTriage(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	id := strings.TrimSpace(str(args["id"]))
	disp := strings.TrimSpace(str(args["disposition"]))
	if id == "" || disp == "" {
		return mcp.NewToolResultError("id and disposition are required"), nil
	}
	st, err := s.ideaStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	rec, err := st.Triage(idea.TriageArgs{
		ID:          id,
		Disposition: idea.Disposition(disp),
		Note:        str(args["note"]),
		Domain:      str(args["domain"]),
		TargetID:    str(args["target_id"]),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	msg := fmt.Sprintf(
		"Triaged %s → %s\nNext: %s\n",
		rec.ID, rec.Disposition, idea.NextCeremony(rec.Disposition),
	)
	if rec.Disposition == idea.File && rec.TargetID == "" {
		msg += "Reminder: disposition=file is a ceremony marker — call jevons_target_file to allocate 🎯, then jevons_idea_triage again with target_id (and T193 spawn if Build).\n"
	}
	return mcp.NewToolResultText(msg), nil
}
