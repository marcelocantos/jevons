// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
)


// t7664Finish builds a valid finish-report for reap hermetics (🎯T766.4:
// bare prose "Done." no longer reaps — the finish-report kind is the claim).
func t7664Finish(target, sha, payload string) string {
	if target == "" {
		target = "T766.4"
	}
	if sha == "" {
		sha = "abcdef0123456"
	}
	return envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       target,
		SHA:          sha,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      payload,
	})
}

func TestLooksLikeFinishedWorkReport(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"prose mid-work", "still reading the tree", false},
		{"bare done prose (T766.4: unknown, not finished)", "Done. Mission complete.", false},
		{"evidence without envelope", "go test ./... PASS", false},
		{"sha prose without envelope", "SHA abcdef0123456 landed", false},
		{"done+sha prose without envelope", "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T165 PASS", false},
		{"finish-report envelope", t7664Finish("T165", "abcdef0123456", "Done. Mission complete."), true},
		{"finish-report with oracle payload", t7664Finish("T165", "abcdef0123456", "go test ./internal/mcpserver -run T165 PASS"), true},
		{"accepted-risk envelope", envelope.Format(&envelope.Message{
			Kind: envelope.KindFinishReport, Target: "T31.1", Risk: envelope.RiskClass3,
			SilentLedger: envelope.SilentLedgerEmpty, Payload: "owner smoke residual",
		}), true},
	}
	for _, tc := range cases {
		if got := LooksLikeFinishedWorkReport(tc.in); got != tc.want {
			t.Errorf("%s: LooksLikeFinishedWorkReport=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestDurableFleetAgent(t *testing.T) {
	isO := func(n string) bool { return n == "jevons" }
	if !DurableFleetAgent("jevons", claudia.PurposeOverseer, isO) {
		t.Fatal("overseer must be durable")
	}
	if !DurableFleetAgent("jevons-po", claudia.PurposeWork, isO) {
		t.Fatal("jevons-po must be durable")
	}
	if !DurableFleetAgent("po", claudia.PurposeWork, isO) {
		t.Fatal("po fixture must be durable")
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
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
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

	// Done path: finished report with oracle evidence → stop+Remove.
	report := t7664Finish("T165", "abcdef0123456", "go test ./internal/mcpserver -run T165 PASS")
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

// 🎯T195 / 🎯T766.4: a finish-report envelope (even with a bare payload claim)
// reaps work leaves. Unenveloped "Done." no longer reaps — the envelope is the
// claim. Overseer acceptance still requires oracle/risk under T31.1.
func TestT195BareDoneReapsWorkAgent(t *testing.T) {
	reg := regWithTree(t)
	// "worker" from fixture is purpose-empty (defaults to work) and not durable.
	reaped, err := ReapDoneWorkAgent(reg, nil, "worker", t7664Finish("T195", "abcdef0123456", "Done. All finished."), func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if !reaped {
		t.Fatal("finish-report envelope must reap work implementer (T195/T766.4)")
	}
	if reg.Def("worker") != nil {
		t.Fatal("worker still registered after bare-done reap")
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
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
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

	// Finish-report with oracle field (payload may still say "Done.") → stop+Remove.
	report := t7664Finish("T195", "abcdef0123456", "Done. Mission complete. Target achieved.")
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
	report := t7664Finish("T165", "abcdef0123456", "go test ./... PASS")
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
		Text:       t7664Finish("T165", "abcdef0123456", "hermetic TestT165 PASS"),
		StopReason: "end_turn",
	})
	if reg.Def("leaf-w") != nil {
		t.Fatal("terminal finished report must auto-reap work agent")
	}
	if len(notified) == 0 {
		t.Fatal("overseer must receive done report before reap")
	}
	if !strings.Contains(notified[0], "finish-report") && !strings.Contains(notified[0], "abcdef0123456") {
		t.Fatalf("notify payload: %q", notified[0])
	}
}
