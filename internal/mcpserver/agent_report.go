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
	"github.com/marcelocantos/jevons/internal/notice"
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
// 🎯T747: identical bodies are NOT collapsed here or in agentreport.Save, and
// that is a decision, not an omission. Two reports with the same text, minutes
// apart, are two events: seatActivity (🎯T597) and recoverMissedTurns (🎯T744)
// read Latest().At as "this seat reported since X", and a store that handed
// back the first record for the second report would make a genuine repeat look
// stale. What the PO pays for is redelivery, which is handled where it is
// paid: prepareParentReport refuses an identical body already offered to that
// parent (reportContentKey), and bare acks — the bulk of identical bodies —
// are not stored at all (notify).
//
// Storage failure is logged and returns a zero handle rather than dropping the
// delivery: the report reaching the overseer partially beats it not reaching
// the overseer at all.
func (s *Server) storeAgentReport(agentName, text string) agentreport.Handle {
	dir := s.agentReportStateDir()
	if dir == "" {
		return agentreport.Handle{}
	}
	now := time.Now()
	rec, err := agentreport.Save(dir, agentName, text, now)
	if err != nil {
		slog.Error("agent report store failed",
			"agent", agentName, "len", len(text), "err", err)
		return agentreport.Handle{}
	}
	s.recordTerminalNotice(dir, agentName, text, now)
	return rec.Handle()
}

// recordTerminalNotice extracts a structured 🎯T254.4 terminal-outcome notice
// from a stored report and appends it to the reporting agent's parent's
// durable inbox. Best-effort: a non-terminal report (status-ping, ack, plain
// prose) yields ok=false and nothing is written — free text remains the only
// surface for those, unchanged from before this existed.
func (s *Server) recordTerminalNotice(dir, agentName, text string, at time.Time) {
	parent := s.agentParent(agentName)
	n, ok := notice.FromReport(agentName, parent, text, at)
	if !ok {
		return
	}
	if err := notice.Append(dir, n); err != nil {
		slog.Error("terminal notice append failed",
			"agent", agentName, "parent", parent, "err", err)
	}
}

// agentParent reads the registry's Parent for name, empty when unknown or
// the registry is unwired.
func (s *Server) agentParent(name string) string {
	if s == nil || s.registry == nil {
		return ""
	}
	def := s.registry.Def(name)
	if def == nil {
		return ""
	}
	return def.Parent
}

func (s *Server) registerAgentReportTools() {
	// The store is wired independently of the MCP transport so tests (and any
	// degraded boot without a tool server) still get durability; only the
	// tool registration needs a live mcpSrv.
	if s.mcpSrv == nil {
		return
	}
	s.addTool(
		mcp.NewTool("jevons_inbox_list",
			mcp.WithDescription("List durable structured terminal-outcome notices (🎯T254.4) — one small record per worker finish-report/scout-report/escalation (agent, parent, kind, outcome done|blocked|needs-design|other, target, sha, gate id, verdict, oracle/risk flags, a summary line), oldest first. Pass parent for a PO's own inbox; omit parent for the overseer's fleet-wide view (every parent, including notices filed as \"unowned\" because the reporter's parent was unknown). done means a finish-report with oracle or accepted-risk and no failing verdict; a RED verdict is blocked. Workers may state the outcome explicitly with a \"jevons: outcome done|blocked|needs-design\" slot. This does not replace the full free-text report (still read via jevons_agent_report_read); it is structure added on top. HTTP: GET /api/inbox?parent=&outcome=&limit=."),
			mcp.WithString("parent", mcp.Description("Parent agent name whose inbox to read, e.g. jevons-po. Omit for the fleet-wide overseer view.")),
			mcp.WithString("outcome", mcp.Description("Optional filter: done | blocked | needs-design | other")),
			mcp.WithNumber("limit", mcp.Description("Keep only the most recent N notices (default 50; 0 = all)")),
		),
		s.handleInboxList,
	)
	s.addTool(
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

// defaultInboxLimit bounds an unfiltered listing so a long-lived inbox stays
// scannable; limit=0 asks for everything.
const defaultInboxLimit = 50

// handleInboxList implements jevons_inbox_list: the 🎯T254.4 durable
// structured-notice surface for a parent PO, or fleet-wide for the overseer.
func (s *Server) handleInboxList(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dir := s.agentReportStateDir()
	if dir == "" {
		return mcp.NewToolResultError("agent report store not configured (state_dir)"), nil
	}
	args := req.GetArguments()
	q := notice.Query{Parent: strings.TrimSpace(str(args["parent"])), Limit: defaultInboxLimit}
	if raw := strings.TrimSpace(str(args["outcome"])); raw != "" {
		o, ok := notice.ParseOutcome(raw)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("unknown outcome %q (want done|blocked|needs-design|other)", raw)), nil
		}
		q.Outcome = o
	}
	if v, ok := args["limit"].(float64); ok && v >= 0 {
		q.Limit = int(v)
	}
	scope := "parent " + q.Parent
	if q.Parent == "" {
		scope = "the whole fleet (overseer view)"
	}
	notices, err := notice.Select(dir, q)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("list notices for %s: %v", scope, err)), nil
	}
	if len(notices) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No structured terminal notices for %s.", scope)), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d structured terminal notice(s) for %s (oldest first):\n", len(notices), scope)
	for _, n := range notices {
		fmt.Fprintf(&b, "  %s  agent=%s parent=%s kind=%s outcome=%s target=%s sha=%s gate=%s verdict=%s oracle=%v risk=%v — %s\n",
			n.Time.Format(time.RFC3339), n.Agent, n.Parent, n.Kind, n.Outcome, n.Target, n.SHA, n.GateID, n.Verdict, n.HasOracle, n.HasRisk, n.Summary)
	}
	return mcp.NewToolResultText(b.String()), nil
}
