// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
)

// ReapClassifyView is the structured answer for jevons_reap_classify (🎯T764).
// It runs the same classifier maybeReapDoneWorkAgent consults before reaping;
// it does not stop or remove the seat.
type ReapClassifyView struct {
	WouldReap       bool           `json:"would_reap"`
	Reason          string         `json:"reason"`
	IdleDisposition string         `json:"idle_disposition"`
	Envelope        map[string]any `json:"envelope"`
}

// ClassifyReapForSeat is the pure decision path shared by the MCP tool and
// hermetic tests. reg and name supply workdir, durable role, and descendants
// facts; report is arbitrary text (typically a finish-report envelope only,
// without the seat's surrounding turn narration).
func ClassifyReapForSeat(reg *claudia.Registry, name, report string, isDurable func(string) bool) ReapClassifyView {
	wouldReap, reason := ShouldAutoReapDoneWorkAgent(reg, name, report, isDurable)
	idle := ClassifyWorkerIdleDisposition(report)
	fields := reapDecisionFields(name, reason, report)
	return ReapClassifyView{
		WouldReap:       wouldReap,
		Reason:          reason,
		IdleDisposition: string(idle),
		Envelope:        fields,
	}
}

// jevons_reap_classify classifies supplied report text through the running
// daemon's reap path without requiring that text to be a seat's whole turn
// (🎯T764). Supervisors and workers use it to probe finish shapes before send.
func (s *Server) registerReapClassifyTool() {
	s.addTool(
		mcp.NewTool("jevons_reap_classify",
			mcp.WithDescription("Classify supplied report text through the same path maybeReapDoneWorkAgent uses (🎯T764). Returns would_reap, reason, idle_disposition, and envelope fields. Does not reap or notify. Pass only the finish-report envelope to avoid turn narration confounding the classifier."),
			mcp.WithString("agent", mcp.Required(), mcp.Description("Registered seat name whose workdir, role, and descendants are applied (same as a live terminal turn on that seat).")),
			mcp.WithString("report", mcp.Required(), mcp.Description("Report text to classify — usually a jevons finish-report fence without surrounding turn prose.")),
		),
		s.handleReapClassify,
	)
}

func (s *Server) handleReapClassify(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := strings.TrimSpace(str(req.GetArguments()["agent"]))
	report := strings.TrimSpace(str(req.GetArguments()["report"]))
	if name == "" {
		return mcp.NewToolResultError("agent is required"), nil
	}
	if report == "" {
		return mcp.NewToolResultError("report is required"), nil
	}
	if s == nil || s.registry == nil {
		return mcp.NewToolResultError("registry unavailable"), nil
	}
	view := ClassifyReapForSeat(s.registry, name, report, s.durableSeat)
	blob, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	summary := fmt.Sprintf("would_reap=%t reason=%s idle_disposition=%s",
		view.WouldReap, view.Reason, view.IdleDisposition)
	return mcp.NewToolResultText(summary + "\n" + string(blob)), nil
}
