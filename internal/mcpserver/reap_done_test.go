// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
)

// declaredFinishReport is a valid finish-report. Unenveloped prose is not
// a done claim (🎯T766.4).
func declaredFinishReport(target, payload string) string {
	return envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       target,
		SHA:          "abcdef0123456",
		Verdict:      envelope.VerdictGreen,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      payload,
	})
}

// declaredInProgressReport is a finish-report whose status slot says the
// work is not done. Payload prose is not that claim (🎯T766.4).
func declaredInProgressReport(target, payload string) string {
	return envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       target,
		Status:       envelope.ProgressInProgress,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      payload,
	})
}

// declaredRiskFinishReport is a valid finish-report with accepted-risk and
// no SHA or gate id. The uncited-claim veto (🎯T946) only runs after a
// declared finish, and a SHA slot would cite an oracle id.
func declaredRiskFinishReport(target, payload string) string {
	return envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       target,
		Risk:         envelope.RiskAccepted,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      payload,
	})
}

// TestT7664ReapFieldsFollowTheEnvelopeNotTheSentence drives the reap log.
// A completion sentence, including one that says a turn is in flight, is
// not a done class and is not a log field. A declared finish is.
func TestT7664ReapFieldsFollowTheEnvelopeNotTheSentence(t *testing.T) {
	prose := "Done. reading done; implementing now. A turn is in flight; the quoted marker is not a claim."
	fields := reapDecisionFields("worker", "unknown_report", prose)
	if fields["done_class"] != "unknown" {
		t.Fatalf("prose done_class=%v", fields["done_class"])
	}
	for _, key := range []string{"claim_marker", "ask_class", "ask_marker", "report_span", "cited_marker"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("prose field %s=%v", key, fields[key])
		}
	}

	finish := declaredFinishReport("T766.4", prose)
	fields = reapDecisionFields("worker", "finished_work", finish)
	if fields["done_class"] != envelope.DoneYes.String() {
		t.Fatalf("finish done_class=%v", fields["done_class"])
	}
	if fields["envelope_kind"] != string(envelope.KindFinishReport) {
		t.Fatalf("envelope_kind=%v", fields["envelope_kind"])
	}
	if _, ok := fields["claim_marker"]; ok {
		t.Fatalf("finish still names a prose claim: %v", fields)
	}

	inProgress := declaredInProgressReport("T766.4", prose)
	fields = reapDecisionFields("worker", "outstanding_scope_in_progress", inProgress)
	if fields["done_class"] != envelope.DoneNo.String() || fields["envelope_status"] != string(envelope.ProgressInProgress) {
		t.Fatalf("in-progress fields=%v", fields)
	}

	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	reg := t439Registry(t, "jv-t7664-prose")
	s := &Server{registry: reg}
	s.maybeReapDoneWorkAgent("jv-t7664-prose", prose)
	if reg.Def("jv-t7664-prose") == nil {
		t.Fatal("prose completion reaped the seat")
	}
	for _, rec := range cap.snapshot() {
		m := attrsMap(rec)
		switch m["reason"] {
		case "local_done_clause", "quoted_completion_claim":
			t.Fatalf("reap log reclassified prose: %v", m)
		}
		if m["claim_marker"] != nil || m["ask_class"] != nil || m["cited_marker"] != nil {
			t.Fatalf("reap log carried a prose span: %v", m)
		}
	}

	cap.reset()
	reg2 := t439Registry(t, "jv-t7664-finish")
	s2 := &Server{registry: reg2}
	s2.maybeReapDoneWorkAgent("jv-t7664-finish", finish)
	if reg2.Def("jv-t7664-finish") != nil {
		t.Fatal("declared finish stayed registered")
	}
	got := findLifecycle(cap.snapshot(), compAgentLifecycle, "reap_done")
	if got == nil {
		t.Fatal("declared finish wrote no reap_done log")
	}
	if got["outcome"] != "ok" || got["reason"] != "finished_work" || got["done_class"] != "yes" {
		t.Fatalf("reap log=%v", got)
	}
	if got["claim_marker"] != nil || got["envelope_kind"] != string(envelope.KindFinishReport) {
		t.Fatalf("reap log=%v", got)
	}
}

// TestT7664FalseGreenSkipLogsTheTypedReason drives maybeReapDoneWorkAgent.
// The skip log repeats the false-green reason the typed checker returned.
// It does not name a completion sentence inside that report.
func TestT7664FalseGreenSkipLogsTheTypedReason(t *testing.T) {
	report := declaredFinishReport("T470", "Done. Mission complete. SHA abcdef0123456. GATE go-test exit=1 RED id=2817ca36.")
	reg := t439Registry(t, "jv-t7664-false-green")
	ok, want := ShouldAutoReapDoneWorkAgent(reg, "jv-t7664-false-green", report, nil)
	if ok || !strings.HasPrefix(want, "false_green_") {
		t.Fatalf("reap=%v reason=%s, want a false-green veto", ok, want)
	}

	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := &Server{registry: reg}
	s.maybeReapDoneWorkAgent("jv-t7664-false-green", report)
	if reg.Def("jv-t7664-false-green") == nil {
		t.Fatal("false-green report reaped the seat")
	}
	got := findLifecycle(cap.snapshot(), compAgentLifecycle, "reap_done")
	if got == nil {
		t.Fatal("false-green skip wrote no reap_done log")
	}
	if got["outcome"] != "skipped" || got["reason"] != want || got["done_class"] != "yes" {
		t.Fatalf("reap log=%v, want reason %s", got, want)
	}
	if got["claim_marker"] != nil || got["ask_class"] != nil {
		t.Fatalf("false-green log named prose: %v", got)
	}
}

func TestLooksLikeFinishedWorkReport(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"still reading the tree", false},
		{"Done. Mission complete.", false},
		{"go test ./... PASS", false},
		{"SHA abcdef0123456 landed", false},
		{"Done. SHA abcdef0123456. go test ./internal/mcpserver -run T165 PASS", false},
		{"finished — make test green", false},
		{"complete with accepted risk: owner smoke residual (class-3)", false},
		{"All finished, ready for review. hermetic TestReapDone green", false},
		{declaredFinishReport("T165", "Landed."), true},
	}
	for _, tc := range cases {
		if got := LooksLikeFinishedWorkReport(tc.in); got != tc.want {
			t.Errorf("LooksLikeFinishedWorkReport(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestDurableFleetAgent(t *testing.T) {
	isO := func(n string) bool { return n == "jevons" }
	if !DurableFleetAgent("jevons", claudia.PurposeOverseer, isO) {
		t.Fatal("overseer must be durable")
	}
	if DurableFleetAgent("jevons-po", claudia.PurposeWork, isO) {
		t.Fatal("a -po name with purpose work is not durable")
	}
	if !DurableFleetAgent("jevons-po", purposeProductOwner, isO) {
		t.Fatal("purpose product-owner must be durable")
	}
	if DurableFleetAgent("po", claudia.PurposeWork, isO) {
		t.Fatal("the name po is not a purpose")
	}
	if !DurableFleetAgent("att-billing", claudia.PurposeAside, isO) {
		t.Fatal("aside must be durable")
	}
	if DurableFleetAgent("jv-t165-reap-done", claudia.PurposeWork, isO) {
		t.Fatal("work leaf must not be durable")
	}
}

// 🎯T165 hermetic: spawn work fixture → done/achieve path → agent_list omits name.
func TestT165ReapDoneWorkAgentHermetic(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer, Materialized: true, Provider: "grok"},
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: purposeProductOwner, Parent: "jevons", Materialized: true, Provider: "grok"},
		{Name: "jv-t165-reap-done", WorkDir: dir, SessionID: "s-w", Purpose: claudia.PurposeWork, Parent: "jevons-po", Materialized: true, Provider: "grok"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{registry: reg}
	isO := s.isOverseerAgent

	// Residual: stop without kill keeps registration.
	stopReq := mcp.CallToolRequest{}
	stopReq.Params.Arguments = map[string]any{"name": "jv-t165-reap-done"}
	if _, err := s.handleAgentStop(context.Background(), stopReq); err != nil {
		t.Fatal(err)
	}
	if reg.Def("jv-t165-reap-done") == nil {
		t.Fatal("stop must not deregister (T165 residual)")
	}

	// Done path: a declared finish-report → stop+Remove.
	report := declaredFinishReport("T165", "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T165 PASS")
	reaped, err := ReapDoneWorkAgent(reg, nil, "jv-t165-reap-done", report, isO)
	if err != nil {
		t.Fatal(err)
	}
	if !reaped {
		t.Fatal("expected work agent reaped on done path")
	}
	if reg.Def("jv-t165-reap-done") != nil {
		t.Fatal("worker still registered after done reap")
	}

	// Live registry omits the finished worker; PO + overseer stay.
	// 🎯T401 may still name the reaped seat under Finished-and-reaped
	// (recoverable address) — that is not a live row.
	if s.registry.Def("jv-t165-reap-done") != nil {
		t.Fatal("reaped worker still in live registry")
	}
	listReq := mcp.CallToolRequest{}
	listRes, err := observedAgentList(s, context.Background(), listReq)
	if err != nil {
		t.Fatal(err)
	}
	out := toolText(listRes)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "jv-t165-reap-done") &&
			!strings.Contains(out, "Finished-and-reaped") {
			t.Fatalf("agent_list still lists reaped worker as a live seat:\n%s", out)
		}
	}
	if !strings.Contains(out, "jevons-po") || !strings.Contains(out, "jevons") {
		t.Fatalf("PO/overseer must remain in agent_list:\n%s", out)
	}

	// PO finished-style report must not auto-reap durable PO.
	reapedPO, err := ReapDoneWorkAgent(reg, nil, "jevons-po", report, isO)
	if err != nil {
		t.Fatal(err)
	}
	if reapedPO {
		t.Fatal("PO must not auto-reap")
	}
	if reg.Def("jevons-po") == nil {
		t.Fatal("PO was removed")
	}
}

// 🎯T766.4: unenveloped bare done declares nothing, so it is unknown and
// the seat stays. A declared finish-report still reaps the work leaf.
func TestT195BareDoneReapsWorkAgent(t *testing.T) {
	reg := regWithTree(t)
	isWork := func(string) bool { return false }
	reaped, err := ReapDoneWorkAgent(reg, nil, "worker", "Done. All finished.", isWork)
	if err != nil {
		t.Fatal(err)
	}
	if reaped {
		t.Fatal("unenveloped bare done must not reap")
	}
	if reg.Def("worker") == nil {
		t.Fatal("worker removed on unenveloped bare done")
	}
	reaped, err = ReapDoneWorkAgent(reg, nil, "worker", declaredFinishReport("T195", "Done. All finished."), isWork)
	if err != nil {
		t.Fatal(err)
	}
	if !reaped {
		t.Fatal("declared finish-report must reap work implementer")
	}
	if reg.Def("worker") != nil {
		t.Fatal("worker still registered after declared finish")
	}
}

// 🎯T195 hermetic: implementer path with imperfect report → agent_list omits name.
func TestT195ImperfectDoneReportHermetic(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer, Materialized: true, Provider: "grok"},
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: purposeProductOwner, Parent: "jevons", Materialized: true, Provider: "grok"},
		{Name: "jv-t195-reap-imperfect", WorkDir: dir, SessionID: "s-w", Purpose: claudia.PurposeWork, Parent: "jevons-po", Materialized: true, Provider: "grok", TargetID: "T195"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{registry: reg}
	isO := s.isOverseerAgent

	// Residual: stop without kill keeps registration.
	stopReq := mcp.CallToolRequest{}
	stopReq.Params.Arguments = map[string]any{"name": "jv-t195-reap-imperfect"}
	if _, err := s.handleAgentStop(context.Background(), stopReq); err != nil {
		t.Fatal(err)
	}
	if reg.Def("jv-t195-reap-imperfect") == nil {
		t.Fatal("stop must not deregister (T165/T195 residual)")
	}

	// Unenveloped prose stays. A declared finish-report stop+Removes.
	if reaped, err := ReapDoneWorkAgent(reg, nil, "jv-t195-reap-imperfect", "Done. Mission complete. Target achieved.", isO); err != nil || reaped {
		t.Fatalf("unenveloped prose reaped=%v err=%v", reaped, err)
	}
	report := declaredFinishReport("T195", "Done. Mission complete. Target achieved.")
	reaped, err := ReapDoneWorkAgent(reg, nil, "jv-t195-reap-imperfect", report, isO)
	if err != nil {
		t.Fatal(err)
	}
	if !reaped {
		t.Fatal("expected work agent reaped on imperfect done path")
	}
	if reg.Def("jv-t195-reap-imperfect") != nil {
		t.Fatal("worker still registered after imperfect-done reap")
	}

	listReq := mcp.CallToolRequest{}
	listRes, err := observedAgentList(s, context.Background(), listReq)
	if err != nil {
		t.Fatal(err)
	}
	out := toolText(listRes)
	if strings.Contains(out, "jv-t195-reap-imperfect") {
		t.Fatalf("agent_list still lists reaped worker:\n%s", out)
	}
	if !strings.Contains(out, "jevons-po") || !strings.Contains(out, "jevons") {
		t.Fatalf("PO/overseer must remain in agent_list:\n%s", out)
	}

	// PO imperfect report must not auto-reap durable PO.
	reapedPO, err := ReapDoneWorkAgent(reg, nil, "jevons-po", report, isO)
	if err != nil {
		t.Fatal(err)
	}
	if reapedPO {
		t.Fatal("PO must not auto-reap")
	}
}

func TestT165HasDescendantsDoesNotReap(t *testing.T) {
	reg := regWithTree(t)
	// Mark po as non-durable name... use a boss name without -po.
	dir := t.TempDir()
	if err := reg.Register(claudia.AgentDef{
		Name: "boss-slice", WorkDir: dir, SessionID: "s-b", Parent: "jevons",
		Purpose: claudia.PurposeWork, Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "leaf-a", WorkDir: dir, SessionID: "s-la", Parent: "boss-slice",
		Purpose: claudia.PurposeWork, Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	report := declaredFinishReport("T165", "Done. SHA abcdef0123456. go test ./... PASS")
	reaped, err := ReapDoneWorkAgent(reg, nil, "boss-slice", report, func(n string) bool { return n == "jevons" })
	if err != nil {
		t.Fatal(err)
	}
	if reaped {
		t.Fatal("boss with descendants must not auto-reap")
	}
	if reg.Def("boss-slice") == nil {
		t.Fatal("boss removed while children registered")
	}
}

func TestT165MaybeReapOnTerminalSink(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "leaf-w", WorkDir: dir, SessionID: "s-l", Parent: "jevons",
		Purpose: claudia.PurposeWork, Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: dir, SessionID: "s-o",
		Purpose: claudia.PurposeOverseer, Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	var notified []string
	s := &Server{
		registry: reg,
		notifyJevon: func(text string) {
			notified = append(notified, text)
		},
	}
	// Exercise sink path used on real terminal stops.
	sink := s.agentEventSink("leaf-w")
	sink(claudia.Event{
		Type:       "assistant",
		Text:       declaredFinishReport("T165", "Done. SHA abcdef0123456. hermetic TestT165 PASS"),
		StopReason: "end_turn",
	})
	if reg.Def("leaf-w") != nil {
		t.Fatal("terminal finished report must auto-reap work agent")
	}
	if len(notified) == 0 {
		t.Fatal("overseer must receive done report before reap")
	}
	if !strings.Contains(notified[0], "Done. SHA") {
		t.Fatalf("notify payload: %q", notified[0])
	}
}
