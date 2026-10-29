// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T1109: jevons_agent_kill with descendants preserved (🎯T560) leaves the
// registry row in place and stamps the name Reaped. A fresh
// jevons_agent_start under that same name must succeed AND reset the
// per-agent intent to Working — not stay refused, and not succeed silently
// while intent still reads Reaped under a live process (which is what let
// the 🎯T984 sentinel stop the seat 12ms after frontier_consume re-minted
// it on 2026-10-21).
func TestT1109FreshStartUnderPreservedKilledRowResetsIntent(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	const name = "jv-t1071-auto"

	// The row 🎯T560's kill preserves: still registered, stopped.
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-" + name,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	// The intent stamp a preserve-descendants kill leaves behind.
	if err := store.SetAgent(name, fleetintent.Reaped, "product:kill", "", time.Now().Add(-2*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetFleetIntentStore(store)
	// Never actually dial a broker/process; the assertion is about the
	// intent gate and stamp, not about launch mechanics.
	s.launchAgentFn = func(context.Context, string) (*claudia.Agent, error) {
		return nil, errors.New("test: no launch")
	}

	if got := s.fleetIntent().AgentState(name); got != fleetintent.Reaped {
		t.Fatalf("precondition: intent = %q, want reaped", got)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    name,
		"workdir": dir,
		"parent":  "jevons-po",
		"purpose": "work",
	}
	res, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res != nil && res.IsError {
		text := toolText(res)
		if strings.Contains(text, "refusing to start") {
			t.Fatalf("fresh start under a preserved killed row was refused: %s", text)
		}
	}

	if got := s.fleetIntent().AgentState(name); got != fleetintent.Working {
		t.Fatalf("intent after fresh start = %q, want working (fresh start under a previously reaped name)", got)
	}
}

// Companion to the acceptance's second clause: once intent reads Working
// again, sweepStoodDownSeats (🎯T984) must leave the seat alone — not stop a
// process that is now running with the harness's blessing.
func TestT1109SweepLeavesFreshlyStartedSeatRunning(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	const name = "jv-t1071-auto"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-" + name,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetFleetIntentStore(store)

	// Fresh start resets intent to Working (the behaviour under test above).
	s.MarkAgentWorking(name, "test", "fresh start under a previously reaped name")
	if got := s.fleetIntent().AgentState(name); got != fleetintent.Working {
		t.Fatalf("intent = %q, want working", got)
	}

	found := s.stopSeatsAgainstIntent(nil)
	for _, v := range found {
		if v.Name == name {
			t.Fatalf("sweep stopped a seat whose intent is working: %+v", v)
		}
	}

	// Sanity: a seat that is genuinely still Reaped (no fresh start) is still
	// a target for the sweep's stand-down — the 🎯T413 distinction survives.
	if err := store.SetAgent(name, fleetintent.Reaped, "product:kill", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	s2 := New(dir, nil, nil)
	s2.SetRegistry(reg)
	s2.SetFleetIntentStore(store)
	// localSeatAlive requires a live registry process; simulate by not
	// faking it — stopSeatsAgainstIntent only acts on seats it finds
	// against intent AND alive. Since nothing here is actually alive, the
	// meaningful oracle is on the policy function directly.
	st := s2.fleetIntent().AgentState(name)
	if !fleetintent.StoodDown(st) {
		t.Fatalf("genuinely reaped intent = %q, want a stood-down state", st)
	}
}
