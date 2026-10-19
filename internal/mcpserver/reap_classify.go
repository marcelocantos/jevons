// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

// probeSeatName is the synthetic work seat used when the caller supplies no
// name. It is never durable and never registered on the live fleet — it only
// exists so ShouldAutoReapDoneWorkAgent (the same function maybeReapDoneWorkAgent
// calls) can classify a bare report string without a seat narrating itself into
// an ask class (🎯T764).
const probeSeatName = "t764-reap-probe"

// ReapClassifyView is the structured answer from jevons_reap_classify.
// Decision and Reason are exactly what ShouldAutoReapDoneWorkAgent returns;
// the rest is the same diagnostic detail the lifecycle log carries on a real
// reap/skip (reapDecisionFields), so a live probe is readable the same way.
type ReapClassifyView struct {
	// Reap is true when the classifier would stop+Remove the seat.
	Reap bool `json:"reap"`
	// Reason is the classifier's reason string (finished_work, bare_done_completion,
	// awaits_overseer_*, not_finished_work_report, durable_role, …).
	Reason string `json:"reason"`
	// Seat is the name the decision was evaluated against (supplied, or the
	// synthetic probe seat).
	Seat string `json:"seat"`
	// Synthetic is true when no live seat was named and a temporary registry
	// entry was used. The live fleet is never touched.
	Synthetic bool `json:"synthetic"`
	// AskClass is set when ClassifyReportAskDetail found an ask.
	AskClass string `json:"ask_class,omitempty"`
	// AskMarker / ClaimMarker name the phrase the classifier keyed on.
	AskMarker   string `json:"ask_marker,omitempty"`
	ClaimMarker string `json:"claim_marker,omitempty"`
	// ReportSpan is the short window of report text around the deciding phrase.
	ReportSpan   string `json:"report_span,omitempty"`
	ReportOffset int    `json:"report_offset,omitempty"`
	// FalseGreenFlags lists FalseGreenFlagsForWorker kinds when that veto fired.
	FalseGreenFlags []string `json:"false_green_flags,omitempty"`
	// LooksFinished / LooksOwnWorkComplete are the pure report predicates the
	// decision path consults — exposed so a probe can see which half declined.
	LooksFinished        bool `json:"looks_finished"`
	LooksOwnWorkComplete bool `json:"looks_own_work_complete"`
	// Path names the production function that produced Decision/Reason, so a
	// reader does not have to guess whether a parallel classifier was used.
	Path string `json:"path"`
}

// jevons_reap_classify is the live diagnostic for the finished-work reap
// classifier (🎯T764). A seat that wants to check how the running daemon would
// read a report cannot emit that report as its own turn prose — the narration
// of the experiment becomes part of the classified text and steers the decision
// into an ask class. This tool classifies a *supplied* string through the same
// ShouldAutoReapDoneWorkAgent path maybeReapDoneWorkAgent uses, and returns the
// decision without reaping anyone.
func (s *Server) registerReapClassifyTool() {
	s.addTool(
		mcp.NewTool("jevons_reap_classify",
			mcp.WithDescription("Classify a supplied report text through the same path maybeReapDoneWorkAgent uses (ShouldAutoReapDoneWorkAgent) and return the decision and reason — without reaping anyone (🎯T764). Use this to live-probe the running daemon's reap classifier: pass the report text as an argument instead of emitting it as your own turn prose (which would make the experiment's narration part of the classified text and steer the decision into an ask class)."),
			mcp.WithString("report", mcp.Required(), mcp.Description("Report text to classify. This is the whole string the classifier sees — not a seat's live turn.")),
			mcp.WithString("name", mcp.Description("Optional live seat name. When set, seat-side checks (durable role, descendants, registered workdir for false-green / outstanding-scope) use that seat. When omitted, a synthetic non-durable work seat is used so the report-side decision is still the production path; the live fleet is never touched.")),
			mcp.WithString("workdir", mcp.Description("Optional workdir for the synthetic probe seat (false-green / outstanding-scope). Ignored when name is set — the live seat's WorkDir is used. Defaults to the daemon workerWD.")),
		),
		s.handleReapClassify,
	)
}

func (s *Server) handleReapClassify(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	report := str(args["report"])
	// Empty report is allowed: the classifier has a real answer for it
	// (no_work_claim), and that answer is part of what a probe may want.
	name := strings.TrimSpace(str(args["name"]))
	workDir := strings.TrimSpace(str(args["workdir"]))

	view, err := s.classifyReapReport(name, report, workDir)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	blob, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(formatReapClassifySummary(view) + "\n" + string(blob)), nil
}

// classifyReapReport runs the production decision path and packages the view.
// It never calls maybeReapDoneWorkAgent or killSubtree.
func (s *Server) classifyReapReport(name, report, workDir string) (ReapClassifyView, error) {
	reg, seat, synthetic, cleanup, err := s.reapClassifyRegistry(name, workDir)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return ReapClassifyView{}, err
	}
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, seat, report, s.isOverseerAgent)
	view := ReapClassifyView{
		Reap:                 ok,
		Reason:               reason,
		Seat:                 seat,
		Synthetic:            synthetic,
		LooksFinished:        LooksLikeFinishedWorkReport(report),
		LooksOwnWorkComplete: LooksLikeOwnWorkCompleteReport(report),
		Path:                 "ShouldAutoReapDoneWorkAgent",
	}
	// Same diagnostic fields the lifecycle log carries (reapDecisionFields).
	fields := reapDecisionFields(seat, reason, report)
	if v, _ := fields["ask_class"].(string); v != "" {
		view.AskClass = v
	}
	if v, _ := fields["ask_marker"].(string); v != "" {
		view.AskMarker = v
	}
	if v, _ := fields["claim_marker"].(string); v != "" {
		view.ClaimMarker = v
	}
	if v, _ := fields["report_span"].(string); v != "" {
		view.ReportSpan = v
	}
	if v, ok := fields["report_offset"].(int); ok {
		view.ReportOffset = v
	}
	// When the false-green veto fired, name the kinds — same as
	// maybeReapDoneWorkAgent's skip arm.
	if strings.HasPrefix(reason, "false_green_") {
		wd := ""
		if def := reg.Def(seat); def != nil {
			wd = def.WorkDir
		}
		view.FalseGreenFlags = falseGreenKinds(FalseGreenFlagsForWorker(report, wd))
	}
	return view, nil
}

// reapClassifyRegistry picks the registry the decision runs against.
// A named live seat uses the daemon registry as-is. An unnamed probe gets a
// temporary registry holding one non-durable work seat, so the report-side
// half of ShouldAutoReapDoneWorkAgent is still the production function.
func (s *Server) reapClassifyRegistry(name, workDir string) (reg *claudia.Registry, seat string, synthetic bool, cleanup func(), err error) {
	if name != "" {
		if s == nil || s.registry == nil {
			return nil, "", false, nil, fmt.Errorf("agent registry unavailable")
		}
		return s.registry, name, false, nil, nil
	}

	// Synthetic probe seat: report-side classification without a live name.
	dir := workDir
	if dir == "" && s != nil {
		dir = s.workerWD
	}
	if dir == "" {
		dir = os.TempDir()
	}
	tmpRoot, err := os.MkdirTemp("", "jevons-reap-classify-*")
	if err != nil {
		return nil, "", false, nil, fmt.Errorf("probe dir: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(tmpRoot) }
	tmp, err := claudia.NewRegistry(filepath.Join(tmpRoot, "agents.json"))
	if err != nil {
		cleanup()
		return nil, "", false, nil, fmt.Errorf("probe registry: %w", err)
	}
	if err := tmp.Register(claudia.AgentDef{
		Name:         probeSeatName,
		WorkDir:      dir,
		SessionID:    "t764-probe",
		Purpose:      claudia.PurposeWork,
		Parent:       "jevons-po",
		Materialized: true,
		Provider:     "grok",
	}); err != nil {
		cleanup()
		return nil, "", false, nil, fmt.Errorf("probe register: %w", err)
	}
	return tmp, probeSeatName, true, cleanup, nil
}

func formatReapClassifySummary(v ReapClassifyView) string {
	decision := "keep"
	if v.Reap {
		decision = "reap"
	}
	seat := v.Seat
	if v.Synthetic {
		seat = seat + " (synthetic)"
	}
	return fmt.Sprintf("reap_classify decision=%s reason=%s seat=%s path=%s",
		decision, v.Reason, seat, v.Path)
}
