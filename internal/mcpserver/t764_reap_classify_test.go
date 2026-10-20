// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T764: jevons_reap_classify classifies a supplied report through the same
// ShouldAutoReapDoneWorkAgent path maybeReapDoneWorkAgent uses, without
// reaping anyone and without the report having to be a seat's own turn prose.

func TestT764ClassifyBareDoneReapsSynthetic(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"report": "Done. Mission complete.",
	}
	res, err := s.handleReapClassify(context.Background(), req)
	if err != nil {
		t.Fatalf("handleReapClassify: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("errored: %+v", res)
	}
	view := decodeReapClassify(t, res)
	if !view.Reap {
		t.Fatalf("bare done must reap; got keep reason=%s view=%+v", view.Reason, view)
	}
	if view.Reason != "bare_done_completion" && view.Reason != "finished_work" {
		t.Fatalf("reason=%q, want bare_done_completion or finished_work", view.Reason)
	}
	if !view.Synthetic || view.Seat != probeSeatName {
		t.Fatalf("synthetic probe seat expected; seat=%q synthetic=%v", view.Seat, view.Synthetic)
	}
	if view.Path != "ShouldAutoReapDoneWorkAgent" {
		t.Fatalf("path=%q, want ShouldAutoReapDoneWorkAgent — a parallel classifier is the defect this target forbids", view.Path)
	}
	if !view.LooksFinished {
		t.Fatal("looks_finished must be true for bare done")
	}
	// The live fleet is empty — nothing was registered, nothing was reaped.
	if s.registry != nil && len(s.registry.List()) != 0 {
		t.Fatalf("live registry mutated: %d seats", len(s.registry.List()))
	}
}

func TestT764ClassifyAskKeepsAndNamesClass(t *testing.T) {
	// Specimen shape from 🎯T395 / T764 context: a report that would look like
	// a finish if the narration of the experiment were stripped, but carries
	// an explicit overseer ask — the exact failure mode of self-provocation.
	report := "Done with the experiment setup. Should I proceed with the reap, or hold?"
	s := New(t.TempDir(), nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"report": report}
	res, err := s.handleReapClassify(context.Background(), req)
	if err != nil {
		t.Fatalf("handleReapClassify: %v", err)
	}
	view := decodeReapClassify(t, res)
	if view.Reap {
		t.Fatalf("ask must keep; got reap reason=%s", view.Reason)
	}
	if !strings.HasPrefix(view.Reason, "awaits_overseer_") && view.Reason != "not_finished_work_report" {
		// Either the ask class veto or the finish-shape veto is acceptable;
		// what must not happen is a silent finished_work reap.
		t.Fatalf("reason=%q does not keep as an ask/non-finish", view.Reason)
	}
}

func TestT764ClassifyMatchesMaybeReapDecision(t *testing.T) {
	// The acceptance criterion: same path as maybeReapDoneWorkAgent. Drive both
	// with the same report against the same registered seat; the tool's reason
	// must equal ShouldAutoReapDoneWorkAgent's, and maybeReap must either reap
	// or keep in lockstep — never diverge.
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "jv-t764-sink"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-t764",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg, workerWD: dir}
	s.registerReapClassifyTool()

	cases := []struct {
		name   string
		report string
	}{
		{"bare done", "Done. Mission complete."},
		{"oracle finish", "Done. SHA abcdef0123456789. go test ./internal/mcpserver -run T764 PASS"},
		{"empty", ""},
		{"mid chatter", "still reading the tree, no claim yet"},
		{"checkpoint", "Checkpoint: reading done; next I will implement the fix."},
		// The T750 specimen: quoted/negated completion must not reap.
		{"t750 quoted", t745BusyPaneReport},
		{"t750 asserted", t745FinishedReport},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Re-register if a prior case reaped the seat.
			if reg.Def(name) == nil {
				if err := reg.Register(claudia.AgentDef{
					Name: name, WorkDir: dir, SessionID: "s-t764",
					Purpose: claudia.PurposeWork, Parent: "jevons-po",
					Materialized: true, Provider: "grok",
				}); err != nil {
					t.Fatal(err)
				}
			}
			wantOK, wantReason := ShouldAutoReapDoneWorkAgent(reg, name, tc.report, s.isOverseerAgent)

			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]any{"report": tc.report, "name": name}
			res, err := s.handleReapClassify(context.Background(), req)
			if err != nil {
				t.Fatalf("handleReapClassify: %v", err)
			}
			if res == nil || res.IsError {
				t.Fatalf("errored: %+v text=%s", res, resultText(t, res))
			}
			view := decodeReapClassify(t, res)
			if view.Reap != wantOK || view.Reason != wantReason {
				t.Fatalf("tool diverged from ShouldAutoReapDoneWorkAgent: tool=(reap=%v reason=%q) want=(reap=%v reason=%q)",
					view.Reap, view.Reason, wantOK, wantReason)
			}
			if view.Synthetic {
				t.Fatal("named seat must not be synthetic")
			}
			if view.Path != "ShouldAutoReapDoneWorkAgent" {
				t.Fatalf("path=%q", view.Path)
			}

			// maybeReapDoneWorkAgent must agree: reap removes the seat, keep leaves it.
			before := reg.Def(name) != nil
			s.maybeReapDoneWorkAgent(name, tc.report)
			after := reg.Def(name) != nil
			if wantOK {
				if after {
					t.Fatalf("maybeReap should have removed %s for reason %s", name, wantReason)
				}
			} else if !after && before {
				t.Fatalf("maybeReap removed %s but classifier said keep (%s)", name, wantReason)
			}
		})
	}
}

func TestT764ClassifyDurableRoleKeeps(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: dir, SessionID: "s-po",
		Purpose: claudia.PurposeWork, Parent: "jevons",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"report": "Done. Mission complete.",
		"name":   "jevons-po",
	}
	res, err := s.handleReapClassify(context.Background(), req)
	if err != nil {
		t.Fatalf("handleReapClassify: %v", err)
	}
	view := decodeReapClassify(t, res)
	if view.Reap {
		t.Fatalf("durable PO must keep; got reap reason=%s", view.Reason)
	}
	if view.Reason != "durable_role" {
		t.Fatalf("reason=%q, want durable_role", view.Reason)
	}
	// And the seat is still registered — the tool never reaps.
	if reg.Def("jevons-po") == nil {
		t.Fatal("tool reaped the PO")
	}
}

func TestT764ClassifyUnknownNameIsNotRegistered(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"report": "Done. Mission complete.",
		"name":   "jv-t764-never-existed",
	}
	res, err := s.handleReapClassify(context.Background(), req)
	if err != nil {
		t.Fatalf("handleReapClassify: %v", err)
	}
	view := decodeReapClassify(t, res)
	// Report-side says finish, seat-side says not registered — same order as
	// the production path (false-green first, then not_registered).
	if view.Reap {
		t.Fatalf("unknown name must keep; got reap reason=%s", view.Reason)
	}
	if view.Reason != "not_registered" && !strings.HasPrefix(view.Reason, "false_green_") {
		t.Fatalf("reason=%q, want not_registered (or false_green_* if the tree vetoed first)", view.Reason)
	}
}

func TestT764ToolIsRegisteredOnNew(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"report": "x"}
	res, err := s.handleReapClassify(context.Background(), req)
	if err != nil {
		t.Fatalf("handleReapClassify: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	// Summary line proves the handler New wires is the production one.
	if text := resultText(t, res); !strings.Contains(text, "reap_classify decision=") {
		t.Fatalf("unexpected result: %q", text)
	}
}

func decodeReapClassify(t *testing.T, res *mcp.CallToolResult) ReapClassifyView {
	t.Helper()
	text := resultText(t, res)
	idx := strings.Index(text, "{")
	if idx < 0 {
		t.Fatalf("no JSON view in %q", text)
	}
	var view ReapClassifyView
	if err := json.Unmarshal([]byte(text[idx:]), &view); err != nil {
		t.Fatalf("decode view: %v\n%s", err, text[idx:])
	}
	if !strings.Contains(text, "reap_classify decision=") {
		t.Fatalf("missing summary line in %q", text)
	}
	return view
}
