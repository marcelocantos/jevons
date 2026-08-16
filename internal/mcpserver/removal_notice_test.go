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
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// t435Fleet is the 2026-08-10 shape: an overseer, the PO that adjudicated the
// finish reports, and the worker the PO's own achieve then reaped.
func t435Fleet(t *testing.T) (*Server, *claudia.Registry) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer, Materialized: true, Provider: "grok"},
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
		{Name: "jv-t420-recovery-oracle", WorkDir: dir, SessionID: "s-w", Purpose: claudia.PurposeWork,
			Parent: "jevons-po", TargetID: "T420", Materialized: true, Provider: "grok"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	return &Server{registry: reg}, reg
}

func t435AgentList(t *testing.T, s *Server) string {
	t.Helper()
	res, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return toolText(res)
}

// TestT435AgentListNamesTheReapThatEmptiedTheRow is clause 2: the reap is
// legible to the parent PO on the fleet surface it already watches, not only
// in a log it would have to think to read. The PO that achieved 🎯T420 sees
// its worker gone from agent_list; without this it sees a shorter list and no
// cause, which is precisely the reading that manufactured a phantom orphaning
// incident on 2026-08-10.
func TestT435AgentListNamesTheReapThatEmptiedTheRow(t *testing.T) {
	s, reg := t435Fleet(t)

	if _, err := s.RemovalAccount().RemoveSubtree(reg, "jv-t420-recovery-oracle", fleetlog.Removal{
		Reason: fleetlog.ReasonReapAchieve,
		Detail: "reaped on achieve of 🎯T420",
		Fields: map[string]any{"achieved_target_id": "T420"},
	}); err != nil {
		t.Fatal(err)
	}

	out := t435AgentList(t, s)
	if strings.Contains(out, "\njv-t420-recovery-oracle") {
		t.Fatalf("reaped worker must not still be listed as running:\n%s", out)
	}
	for _, want := range []string{
		"jv-t420-recovery-oracle left the fleet",
		fleetlog.ReasonReapAchieve,
		"reaped on achieve of 🎯T420",
		"parent jevons-po",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("agent_list must account for the removal (missing %q):\n%s", want, out)
		}
	}
	// Clause 3: a designed teardown reads as one. The surface must not offer
	// the orphaning vocabulary for a row that left by decision.
	for _, never := range []string{"silent removal", "orphan"} {
		if strings.Contains(strings.ToLower(out), never) {
			t.Fatalf("designed teardown must not read as %q:\n%s", never, out)
		}
	}
}

// TestT435AgentListIsSilentAboutAnUnaccountedRemoval is the control, and the
// red half: it removes the same row the pre-fix way — straight through the
// registry — and shows the surface then has nothing to say. The notice comes
// from the accounted path, not from noticing a shorter list.
func TestT435AgentListIsSilentAboutAnUnaccountedRemoval(t *testing.T) {
	s, reg := t435Fleet(t)

	if err := reg.Remove("jv-t420-recovery-oracle"); err != nil {
		t.Fatal(err)
	}

	out := t435AgentList(t, s)
	if strings.Contains(out, "left the fleet") {
		t.Fatalf("a removal that bypassed the chokepoint cannot be accounted for:\n%s", out)
	}
	if !strings.Contains(out, "jevons-po") {
		t.Fatalf("PO must still be listed:\n%s", out)
	}
}

// TestT435AgentListWithNoRemovalsIsUnchanged keeps the surface quiet in the
// ordinary case: the block appears because something left, never as chrome.
func TestT435AgentListWithNoRemovalsIsUnchanged(t *testing.T) {
	s, _ := t435Fleet(t)
	out := t435AgentList(t, s)
	if strings.Contains(out, "Recent fleet removals") {
		t.Fatalf("no removals happened; surface must not announce any:\n%s", out)
	}
	if !strings.Contains(out, "jv-t420-recovery-oracle") {
		t.Fatalf("live worker missing from agent_list:\n%s", out)
	}
}
