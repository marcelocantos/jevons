// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/wakebatch"
)

// 🎯T451 clause 3. The four fixtures the target names: absent agent,
// owner-parked live agent, open-mission control, cleared-owner control.

const t451Ledger = `
targets:
  T70:
    name: owner-parked converging
    status: converging
    owned_by:
      owner: owner
  T71:
    name: genuinely open
    status: identified
  T72:
    name: owner cleared
    status: identified
`

func t451Dir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(t451Ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func t451Server(t *testing.T, dir string, defs ...claudia.AgentDef) (*Server, *recordingSender) {
	t.Helper()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range defs {
		if d.WorkDir == "" {
			d.WorkDir = dir
		}
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetIdlePressureHooks(IdlePressureHooks{MissionOpen: NewLedgerMissionOpen(reg.List)})
	sender := &recordingSender{}
	s.SetSenderResolver(func(string) (agentSender, bool, error) { return sender, false, nil })
	return s, sender
}

func t451IdleText(got []string, agent string) bool {
	for _, line := range got {
		if strings.Contains(line, eventWorkerIdle) && strings.Contains(line, agent) {
			return true
		}
	}
	return false
}

// (a) An idle candidate absent from the registry is dropped at digest
// delivery — the window between emit and flush is how the four phantom
// events reached bullseye-po.
func TestT451AbsentAgentIdleEventIsDropped(t *testing.T) {
	t.Parallel()
	evs := []wakebatch.Event{
		{Recipient: "bullseye-po", Kind: eventWorkerIdle, Subject: "bs-t70-release-probe-ci"},
		{Recipient: "bullseye-po", Kind: eventWorkerIdle, Subject: "bs-t69-version-provenance"},
		{Recipient: "bullseye-po", Kind: eventWorkerIdle, Subject: "still-here"},
		{Recipient: "jevons", Kind: eventDaemonRestarted, Subject: "gone"},
	}
	present := map[string]bool{"still-here": true}
	got := FilterIdleEventsForLiveAgents(evs, func(name string) bool { return present[name] })
	if len(got) != 2 {
		t.Fatalf("kept %d events, want 2 (live idle + non-idle kind): %+v", len(got), got)
	}
	if got[0].Subject != "still-here" || got[1].Kind != eventDaemonRestarted {
		t.Fatalf("kept %+v; want still-here idle and the restart", got)
	}
}

// emitWorkerIdleToParent on a name the registry no longer holds is a
// no-op — the emit-time half of (a).
func TestT451EmitSkipsADeregisteredAgent(t *testing.T) {
	t.Parallel()
	s, sender := t451Server(t, t.TempDir(),
		claudia.AgentDef{Name: "jevons-po", Purpose: claudia.PurposeWork, SessionID: "po"},
	)
	s.emitWorkerIdleToParent("bs-t70-release-probe-ci", "working", "idle")
	if got := sender.delivered(); len(got) != 0 {
		t.Fatalf("emitted for an absent agent: %v", got)
	}
}

// (b) A live agent bound to an owner-assigned target emits nothing.
func TestT451OwnerParkedLiveAgentEmitsNothing(t *testing.T) {
	t.Parallel()
	dir := t451Dir(t)
	s, sender := t451Server(t, dir,
		claudia.AgentDef{Name: "jevons-po", Purpose: claudia.PurposeWork, SessionID: "po"},
		claudia.AgentDef{
			Name: "jv-t70", Purpose: claudia.PurposeWork, Parent: "jevons-po",
			TargetID: "T70", SessionID: "w1",
		},
	)
	s.emitWorkerIdleToParent("jv-t70", "working", "idle")
	if got := sender.delivered(); t451IdleText(got, "jv-t70") {
		t.Fatalf("owner-parked worker still woke the PO: %v", got)
	}
}

// (c) CONTROL — a live agent on a genuinely open unassigned mission still emits.
func TestT451OpenUnassignedMissionStillEmits(t *testing.T) {
	t.Parallel()
	dir := t451Dir(t)
	s, sender := t451Server(t, dir,
		claudia.AgentDef{Name: "jevons-po", Purpose: claudia.PurposeWork, SessionID: "po"},
		claudia.AgentDef{
			Name: "jv-t71", Purpose: claudia.PurposeWork, Parent: "jevons-po",
			TargetID: "T71", SessionID: "w2",
		},
	)
	s.emitWorkerIdleToParent("jv-t71", "working", "idle")
	if got := sender.delivered(); !t451IdleText(got, "jv-t71") {
		t.Fatalf("open-mission worker was silenced: %v", got)
	}
}

// (d) CONTROL — clearing the owner assignment restores emission.
func TestT451ClearedOwnerReturnsToEmission(t *testing.T) {
	t.Parallel()
	dir := t451Dir(t)
	s, sender := t451Server(t, dir,
		claudia.AgentDef{Name: "jevons-po", Purpose: claudia.PurposeWork, SessionID: "po"},
		claudia.AgentDef{
			Name: "jv-t72", Purpose: claudia.PurposeWork, Parent: "jevons-po",
			TargetID: "T72", SessionID: "w3",
		},
	)
	s.emitWorkerIdleToParent("jv-t72", "working", "idle")
	if got := sender.delivered(); !t451IdleText(got, "jv-t72") {
		t.Fatalf("cleared-owner worker was silenced: %v", got)
	}
}

// NewLedgerMissionOpen is the shipped classifier, not a copy of the rule.
func TestT451LedgerMissionOpenReadsOwnedBy(t *testing.T) {
	t.Parallel()
	dir := t451Dir(t)
	list := func() []claudia.AgentDef {
		return []claudia.AgentDef{
			{TargetID: "T70", WorkDir: dir},
			{TargetID: "T71", WorkDir: dir},
		}
	}
	open := NewLedgerMissionOpen(list)
	if open("T70") {
		t.Fatal("T70 is owner-parked; MissionOpen must be false")
	}
	if !open("T71") {
		t.Fatal("T71 is unassigned identified; MissionOpen must stay true")
	}
	if !open("T99") {
		t.Fatal("unknown target stays open — silence is the dangerous answer")
	}
}
