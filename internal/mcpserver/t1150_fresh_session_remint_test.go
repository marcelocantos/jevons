// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/jevons/internal/seatstop"
)

// 🎯T1150 — a dead auto remint must not reuse the prior grant's session,
// especially when T1148 re-places onto another provider.
func TestT1150DeadAutoRemintRotatesSessionAcrossProvider(t *testing.T) {
	t.Cleanup(planusage.SetUnsteerableForTest(map[string]string{}))
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const name = "jv-t1150-auto"
	const oldSession = "1873bdd5-claude-grant"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: oldSession, AutoStart: true,
		Provider: claudia.Provider("openai-codex"), Materialized: true,
		Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T1150",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	now := time.Now()
	okRem, okUsed := 94.0, 6.0
	resets := now.Add(48 * time.Hour)
	lim := planusage.DefaultWeeklyWindowSeconds
	cursorOK := planusage.Backend{Provider: "cursor", Status: planusage.StatusAvailable,
		Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &okUsed, RemainingPercent: &okRem, ResetsAt: &resets, LimitWindowSeconds: &lim}}}
	hot := func(p string) planusage.Backend {
		used, rem := 100.0, 0.0
		resets := now.Add(48 * time.Hour)
		lim := planusage.DefaultWeeklyWindowSeconds
		return planusage.Backend{Provider: p, Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used, RemainingPercent: &rem, ResetsAt: &resets, LimitWindowSeconds: &lim}}}
	}
	aheadRem, aheadUsed := 41.0, 59.0
	claudeAhead := planusage.Backend{Provider: "claude", Status: planusage.StatusAvailable,
		Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &aheadUsed, RemainingPercent: &aheadRem, ResetsAt: &resets, LimitWindowSeconds: &lim}}}
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{hot("codex"), claudeAhead, cursorOK, hot("grok")}}
	})

	def, existed, _, err := s.stitchAgentStart(name, dir, "", "", "", "jevons-po", claudia.PurposeWork, "T1150", "brief")
	if err != nil {
		t.Fatalf("stitch: %v", err)
	}
	if !existed {
		t.Fatal("want preserved reaped row")
	}
	if seatprovider.Resolve(def.Provider).Plan != seatprovider.Cursor {
		t.Fatalf("provider = %q want cursor", def.Provider)
	}
	if def.SessionID == "" || def.SessionID == oldSession {
		t.Fatalf("session = %q want fresh id, not %q", def.SessionID, oldSession)
	}
	if def.Materialized {
		t.Fatal("remint must clear Materialized so launch mints")
	}
}

// Auto-start seats released as unbriefed retire even when the registry row
// predates this start (reaped-name remint shape).
func TestT1150UnbriefedAutoRemintRetiresPreservedRow(t *testing.T) {
	s, reg, _, _ := startPromptFixture(t, "jv-t1150-unbriefed", claudia.ProviderGrok)
	if d := reg.Def("jv-t1150-unbriefed"); d != nil {
		d.AutoStart = true
		if err := reg.Register(*d); err != nil {
			t.Fatal(err)
		}
	}
	if !s.releaseUnbriefedSeat("jv-t1150-unbriefed", true, nil) {
		t.Fatal("auto-start unbriefed remint must retire the row")
	}
	if reg.Def("jv-t1150-unbriefed") != nil {
		t.Fatal("row still registered")
	}
}

// T1139 reattach must not adopt a seat the opening-brief path released.
func TestT1150ReattachSkipsUnbriefedRelease(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const name = "jv-t1150-reattach"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s1", AutoStart: true,
		Provider: claudia.ProviderGrok, Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.noteSeatStop(name, seatstop.SourceUnbriefed, "opening brief proven undelivered; seat released (🎯T387)", "daemon", "")

	blocked := func(n string) bool { return s.reattachBlocked(n) }
	got := fleet.ReattachRunningSeats(reg, fleetintent.Snapshot{}, time.Now(), nil, blocked).Back
	if len(got) != 0 {
		t.Fatalf("reattach = %v want none for unbriefed release", got)
	}
}
