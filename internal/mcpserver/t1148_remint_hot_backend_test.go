// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/seatprovider"
)

// 🎯T1148: a frontier remint must re-place a dead seat whose broker/registry
// row still names a hot backend, instead of inheriting that dest and refusing.
func TestT1148DeadSeatHotCodexRemintsOnGreenDest(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	const name = "jv-t1148-auto"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-dead-codex",
		Provider: claudia.Provider("openai-codex"),
		Purpose:  claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAgent(name, fleetintent.Reaped, "product:kill", "", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetFleetIntentStore(store)
	now := time.Now()
	hot := func(p string) planusage.Backend {
		used, rem := 100.0, 0.0
		resets := now.Add(48 * time.Hour)
		lim := planusage.DefaultWeeklyWindowSeconds
		return planusage.Backend{Provider: p, Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used, RemainingPercent: &rem, ResetsAt: &resets, LimitWindowSeconds: &lim}}}
	}
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			hot("codex"), hot("claude"), hot("cursor"), t583Weekly("grok", 87, now),
		}}
	})

	def, existed, _, err := s.stitchAgentStart(name, dir, "", "", "", "jevons-po", claudia.PurposeWork, "T1148", "")
	if err != nil {
		t.Fatalf("remint stitch on hot codex + ok grok: %v", err)
	}
	if !existed {
		t.Fatal("expected preserved registry row (🎯T560 kill shape)")
	}
	if def == nil {
		t.Fatal("no registry row after remint")
	}
	if seatprovider.Resolve(def.Provider).Plan != seatprovider.Grok {
		t.Fatalf("remint provider = %q, want grok (re-placed off hot codex)", def.Provider)
	}
}


// Acceptance-shaped fixture from the 2026-10-22 🎯T1036 incident (codex hot,
// cursor ok, claude ahead). Default auto-mint excludes unsteerable cursor
// (🎯T791); clearing the synthetic table proves re-placement lands on
// cursor instead of inheriting openai-codex. spawnFrontierWorker registers
// through the same stitchAgentStart path this exercises.
func TestT1148AcceptanceRemintsOnCursorNotHotCodex(t *testing.T) {
	t.Cleanup(planusage.SetUnsteerableForTest(map[string]string{}))
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const name = "jv-t1036-auto"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-dead-codex",
		Provider: claudia.Provider("openai-codex"),
		Purpose:  claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	now := time.Now()
	hot := func(p string) planusage.Backend {
		used, rem := 100.0, 0.0
		resets := now.Add(48 * time.Hour)
		lim := planusage.DefaultWeeklyWindowSeconds
		return planusage.Backend{Provider: p, Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used, RemainingPercent: &rem, ResetsAt: &resets, LimitWindowSeconds: &lim}}}
	}
	okRem, okUsed := 94.0, 6.0
	aheadRem, aheadUsed := 41.0, 59.0
	resets := now.Add(48 * time.Hour)
	lim := planusage.DefaultWeeklyWindowSeconds
	cursorOK := planusage.Backend{Provider: "cursor", Status: planusage.StatusAvailable,
		Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &okUsed, RemainingPercent: &okRem, ResetsAt: &resets, LimitWindowSeconds: &lim}}}
	claudeAhead := planusage.Backend{Provider: "claude", Status: planusage.StatusAvailable,
		Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &aheadUsed, RemainingPercent: &aheadRem, ResetsAt: &resets, LimitWindowSeconds: &lim}}}
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{hot("codex"), claudeAhead, cursorOK, hot("grok")}}
	})

	def, existed, _, err := s.stitchAgentStart(name, dir, "", "", "", "jevons-po", claudia.PurposeWork, "T1036", "")
	if err != nil {
		t.Fatalf("remint on hot codex + ok cursor: %v", err)
	}
	if !existed {
		t.Fatal("expected preserved registry row with broker grant shape")
	}
	if seatprovider.Resolve(def.Provider).Plan != seatprovider.Cursor {
		t.Fatalf("remint provider = %q, want cursor (re-placed off hot codex)", def.Provider)
	}
}

func TestT1148PlanDestEmptyDistinguishesPinnedProvider(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const name = "jv-t1148-pinned-msg"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-dead-codex",
		Provider: claudia.Provider("openai-codex"),
		Purpose:  claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	now := time.Now()
	hot := func(p string) planusage.Backend {
		used, rem := 100.0, 0.0
		resets := now.Add(48 * time.Hour)
		lim := planusage.DefaultWeeklyWindowSeconds
		return planusage.Backend{Provider: p, Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used, RemainingPercent: &rem, ResetsAt: &resets, LimitWindowSeconds: &lim}}}
	}
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{hot("codex"), hot("cursor"), hot("claude"), hot("grok")}}
	})

	err = s.spawnFrontierWorker(name, dir, "jevons-po", "T1148", "brief")
	if err == nil || !strings.Contains(err.Error(), "plan dest empty") {
		t.Fatalf("spawn = %v, want plan dest empty", err)
	}
	if !strings.Contains(err.Error(), "prior seat pinned codex") {
		t.Fatalf("error = %q, want pinned-provider wording distinct from all-providers-fail", err.Error())
	}
	notice := FormatSpawnFailureNotice("T1148", name, err.Error())
	if !strings.Contains(notice, "prior seat pinned codex") {
		t.Fatalf("spawn-failure notice = %q, want pinned provider quoted", notice)
	}
}
