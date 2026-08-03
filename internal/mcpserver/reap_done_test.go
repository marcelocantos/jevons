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
)

func TestLooksLikeFinishedWorkReport(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"still reading the tree", false},
		{"Done. Mission complete.", false}, // bare done — T31 residual
		{"go test ./... PASS", false},      // evidence without claim
		{"SHA abcdef0123456 landed", false},
		{"Done. SHA abcdef0123456. go test ./internal/mcpserver -run T165 PASS", true},
		{"finished — make test green", true},
		{"complete with accepted risk: owner smoke residual (class-3)", true},
		{"All finished, ready for review. hermetic TestReapDone green", true},
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
	report := "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T165 PASS"
	reaped, err := ReapDoneWorkAgent(reg, "jv-t165-reap-done", report, isO)
	if err != nil {
		t.Fatal(err)
	}
	if !reaped {
		t.Fatal("expected work agent reaped on done path")
	}
	if reg.Def("jv-t165-reap-done") != nil {
		t.Fatal("worker still registered after done reap")
	}

	// agent_list omits the finished worker; PO + overseer stay.
	listReq := mcp.CallToolRequest{}
	listRes, err := s.handleAgentList(context.Background(), listReq)
	if err != nil {
		t.Fatal(err)
	}
	out := toolText(listRes)
	if strings.Contains(out, "jv-t165-reap-done") {
		t.Fatalf("agent_list still lists reaped worker:\n%s", out)
	}
	if !strings.Contains(out, "jevons-po") || !strings.Contains(out, "jevons") {
		t.Fatalf("PO/overseer must remain in agent_list:\n%s", out)
	}

	// PO finished-style report must not auto-reap durable PO.
	reapedPO, err := ReapDoneWorkAgent(reg, "jevons-po", report, isO)
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

func TestT165BareDoneDoesNotReap(t *testing.T) {
	reg := regWithTree(t)
	// "worker" from fixture is purpose-empty (defaults to work) and not durable.
	reaped, err := ReapDoneWorkAgent(reg, "worker", "Done. All finished.", func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if reaped {
		t.Fatal("bare done must not reap")
	}
	if reg.Def("worker") == nil {
		t.Fatal("worker removed on bare done")
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
	report := "Done. SHA abcdef0123456. go test ./... PASS"
	reaped, err := ReapDoneWorkAgent(reg, "boss-slice", report, func(n string) bool { return n == "jevons" })
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
		Text:       "Done. SHA abcdef0123456. hermetic TestT165 PASS",
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
