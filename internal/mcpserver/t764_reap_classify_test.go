// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T764: classify finish-report text without the seat's surrounding narration.
func TestT764ReapClassifyEnvelopeOnlyMatchesProductionPath(t *testing.T) {
	reg := t439Registry(t, "jv-t764-probe")
	finishOnly := declaredFinishReport("T764", "Landed with oracle evidence.")
	view := ClassifyReapForSeat(reg, "jv-t764-probe", finishOnly, nil)
	if !view.WouldReap {
		t.Fatalf("envelope-only finish should reap, got reason=%s", view.Reason)
	}
	if view.IdleDisposition != string(IdleActionReap) {
		t.Fatalf("idle_disposition=%s", view.IdleDisposition)
	}
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t764-probe", finishOnly, nil)
	if ok != view.WouldReap || reason != view.Reason {
		t.Fatalf("ClassifyReapForSeat diverged from ShouldAutoReapDoneWorkAgent: (%v,%q) vs (%v,%q)",
			view.WouldReap, view.Reason, ok, reason)
	}

	inProgress := declaredInProgressReport("T764", "Still open gaps.")
	view2 := ClassifyReapForSeat(reg, "jv-t764-probe", inProgress, nil)
	if view2.WouldReap {
		t.Fatalf("in-progress finish should not reap, reason=%s", view2.Reason)
	}
	if view2.IdleDisposition != string(IdleActionKeep) {
		t.Fatalf("idle_disposition=%s", view2.IdleDisposition)
	}
}

// A seat can probe the envelope it intends to send while keeping experiment
// prose out of the classified string.
func TestT764ReapClassifyIgnoresNarrationNotInReportArg(t *testing.T) {
	reg := t439Registry(t, "jv-t764-self")
	finish := declaredFinishReport("T764", "Done.")
	narratedTurn := "I am live-testing the reap classifier on myself.\n\n" + finish + "\n\nThis paragraph is not part of the report argument."
	viewEnv := ClassifyReapForSeat(reg, "jv-t764-self", finish, nil)
	viewTurn := ClassifyReapForSeat(reg, "jv-t764-self", narratedTurn, nil)
	if !viewEnv.WouldReap {
		t.Fatalf("clean envelope should reap")
	}
	if viewTurn.WouldReap {
		t.Fatalf("narrated turn must not be what the tool is for; got reason=%s", viewTurn.Reason)
	}
}

func TestT764ReapClassifyMCPRoundTrip(t *testing.T) {
	reg := t439Registry(t, "jv-t764-mcp")
	s := &Server{registry: reg}
	report := declaredFinishReport("T764", "Shipped.")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"agent":  "jv-t764-mcp",
		"report": report,
	}
	res, err := s.handleReapClassify(context.Background(), req)
	if err != nil {
		t.Fatalf("handleReapClassify: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("classify errored: %+v", res)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "would_reap=true") {
		t.Fatalf("summary %q", text)
	}
	idx := strings.Index(text, "{")
	if idx < 0 {
		t.Fatalf("no JSON in %q", text)
	}
	var view ReapClassifyView
	if err := json.Unmarshal([]byte(text[idx:]), &view); err != nil {
		t.Fatalf("decode: %v\n%s", err, text[idx:])
	}
	if !view.WouldReap || view.Reason == "" {
		t.Fatalf("view=%+v", view)
	}
}
