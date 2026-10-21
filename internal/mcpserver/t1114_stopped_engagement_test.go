// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T1114: the engagement gate counts live seats only. A registry row that
// seatstate has observed as stopped (no process, any fleet intent) must not
// block a PO from spawning a fresh implementer on that target; a row
// observed running still refuses, and the refusal names the seat and the
// status it counted.
func TestAgentStartAdmitsSpawnBesideStoppedRow(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t1114-auto", WorkDir: dir, SessionID: "s1",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok", TargetID: "T1114",
	}); err != nil {
		t.Fatal(err)
	}

	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	// Observe the row as stopped (🎯T414 fleet intent is a separate axis —
	// "any intent" per acceptance — this exercises the seatstate read).
	auth := seatstate.RegistryAuthority(reg)
	auth.BindRegistry(reg)
	seatstate.ObserveStopped(reg, "jv-t1114-auto")

	if msg := s.refuseEngagedOrClosedTarget("jv-t1114-dup", dir, "T1114", false); msg != "" {
		t.Fatalf("stopped row must not block a fresh implementer: %q", msg)
	}

	// Now observe a second seat on the same target as running: it must
	// refuse, and the refusal names the seat and its status.
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t1114-running", WorkDir: dir, SessionID: "s2",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok", TargetID: "T1114",
	}); err != nil {
		t.Fatal(err)
	}
	auth.Observe(seatstate.Observation{Name: "jv-t1114-running", Alive: seatstate.Yes, LocalAlive: seatstate.Yes, QueueDepth: seatstate.QueueUnknown, Source: "test"})

	msg := s.refuseEngagedOrClosedTarget("jv-t1114-dup2", dir, "T1114", false)
	if msg == "" || !strings.Contains(msg, "jv-t1114-running") {
		t.Fatalf("running row must still refuse, got %q", msg)
	}
	if !strings.Contains(msg, "engaged_by=jv-t1114-running status=running") {
		t.Fatalf("refusal must name the seat and its live status, got %q", msg)
	}
}
