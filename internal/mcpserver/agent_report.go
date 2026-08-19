// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/agentreport"
)

// SetAgentReportDir wires the durable agent-report store (🎯T388) under
// state_dir. Empty path leaves the store off and the retrieval tool
// unregistered — in which case notify still delivers, and an over-bound report
// is still marked as cut, just without a retrieval handle. That degradation is
// deliberate: a visible cut with no handle is far better than the silent cut
// it replaces, so a misconfigured state dir must not restore the old failure.
func (s *Server) SetAgentReportDir(stateDir string) {
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
	s.mu.Lock()
	s.agentReportDir = stateDir
	s.mu.Unlock()
	s.registerAgentReportTools()
}

// agentReportStateDir reads the configured store root ("" when unwired).
func (s *Server) agentReportStateDir() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentReportDir
}

// storeAgentReport persists a report before it is delivered and before
// 🎯T165/T195 auto-deregistration can remove the agent, and returns the handle
// the delivery marker should name.
//
// Storage failure is logged and returns a zero handle rather than dropping the
// delivery: the report reaching the overseer partially beats it not reaching
// the overseer at all.
func (s *Server) storeAgentReport(agentName, text string) agentreport.Handle {
	dir := s.agentReportStateDir()
	if dir == "" {
		return agentreport.Handle{}
	}
	rec, err := agentreport.Save(dir, agentName, text, time.Now())
	if err != nil {
		slog.Error("agent report store failed",
			"agent", agentName, "len", len(text), "err", err)
		return agentreport.Handle{}
	}
	return rec.Handle()
}

func (s *Server) registerAgentReportTools() {
	// The store is wired independently of the MCP transport so tests (and any
	// degraded boot without a tool server) still get durability; only the
	// tool registration needs a live mcpSrv.
	if s.mcpSrv == nil {
		return
	}
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_agent_report_read",
			mcp.WithDescription("Read an agent's stored turn report in full (🎯T388). Every report an agent delivers is stored before delivery, so this works AFTER the agent auto-deregisters on its terminal report. Compose with 🎯T401: jevons_agent_send to a reaped agent reports reaped-with-reason and holds gate feedback in sendq rather than answering bare \"not running\"; this tool is the read path for the stored report itself. Use it when a delivered report carries the [⚠️ REPORT TRUNCATED] marker, or when you need one part of a report again: the bytes come from the store, so the agent never re-derives (and so cannot rewrite) what it already said. Omit report_id for the latest report; omit section for the whole text; pass list=true to see what is stored."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Agent name, e.g. jv-t372-auto")),
			mcp.WithString("report_id", mcp.Description("Report id from the truncation marker; default latest")),
			mcp.WithString("section", mcp.Description("Return only the matching section (case-insensitive substring of a markdown heading, e.g. \"asks\")")),
			mcp.WithBoolean("list", mcp.Description("List stored report ids and sizes for this agent instead of returning a body")),
		),
		s.handleAgentReportRead,
	)
}

func (s *Server) handleAgentReportRead(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dir := s.agentReportStateDir()
	if dir == "" {
		return mcp.NewToolResultError("agent report store not configured (state_dir)"), nil
	}
	args := req.GetArguments()
	name := strings.TrimSpace(str(args["name"]))
	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}
	reportID := strings.TrimSpace(str(args["report_id"]))
	section := strings.TrimSpace(str(args["section"]))
	list, _ := args["list"].(bool)

	if list {
		recs, err := agentreport.List(dir, name)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list reports for %q: %v", name, err)), nil
		}
		if len(recs) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("No stored reports for %q.", name)), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d stored report(s) for %s (newest last):\n", len(recs), name)
		for _, r := range recs {
			fmt.Fprintf(&b, "  %s  %s  %d bytes\n", r.ID, r.At.Format(time.RFC3339), r.Bytes)
		}
		return mcp.NewToolResultText(b.String()), nil
	}

	var (
		rec agentreport.Record
		err error
	)
	if reportID == "" {
		rec, err = agentreport.Latest(dir, name)
	} else {
		rec, err = agentreport.Load(dir, name, reportID)
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("read report for %q: %v", name, err)), nil
	}

	if section != "" {
		sec, err := agentreport.FindSection(rec.Text, section)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("report %s: %v", rec.ID, err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf(
			"[%s report %s — section %q, %d bytes, verbatim from the store]\n\n%s",
			name, rec.ID, sec.Heading, len(sec.Text), sec.Text)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf(
		"[%s report %s — %d bytes, full text from the store]\n\n%s",
		name, rec.ID, rec.Bytes, rec.Text)), nil
}
