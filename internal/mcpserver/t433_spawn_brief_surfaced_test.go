// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

// 🎯T433 — a frontier leaf whose spawn brief never reaches the worker is
// surfaced to its product owner, not silently parked.
//
// The T383 instance: the frontier consumer logged park_spawn_failed at
// 10:40:34.565Z and moved on; the owning PO learned nothing and the leaf
// read as unattended-but-fine. These oracles drive the PRODUCTION sweep
// call site (frontierConsumeSweep, per the T419 rule) with a failing spawn
// and assert the PO receives a notification naming the target id and the
// delivery error verbatim — with a control proving a healthy spawn sends
// no such notice.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/relayroute"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// t433Specimens are the two real delivery-error modes observed on
// 2026-08-10 (both from the claudia paste path, 🎯T28 upstream).
var t433Specimens = []string{
	"turn not submitted: composer state=paste_chip after 8 Enter presses",
	"composer empty after paste (brief never reached pane)",
}

func t433Harness(t *testing.T) (*Server, *claudia.Registry, *fakeSender, string) {
	t.Helper()
	dir := t.TempDir()
	const ledger = `
targets:
  T900:
    name: Ready leaf that will lose its worker
    status: identified
`
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: dir, SessionID: "po1",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	po := &fakeSender{alive: true}
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		if name != "jevons-po" {
			t.Fatalf("unexpected fleet delivery to %q", name)
		}
		return po, false, nil
	})
	s.SetTurnWitness(witnessYielding(TurnEvidence{
		Observed: true, PayloadSeen: true,
		Detail: "transcript gained a user message carrying this payload",
	}))
	return s, reg, po, dir
}

// TestT433SpawnFailureSurfacesToPO forces a start-brief delivery failure
// through the production sweep and asserts the acceptance's state (ii): a
// notification addressed to the owning PO naming the leaf and the error
// verbatim, no briefless agent retained, and no silent return to the pool.
func TestT433SpawnFailureSurfacesToPO(t *testing.T) {
	s, reg, po, dir := t433Harness(t)
	ledger, err := OpenFrontierSpawnLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	verbatim := t433Specimens[0]
	loopArgs := FrontierConsumeLoopArgs{
		Server:   s,
		Workdir:  dir,
		ParentPO: "jevons-po",
		Spawn: func(leaf targetfile.FrontierLeaf, workerName, parent string) error {
			// The production spawnFrontierWorker has already torn the seat
			// down (releaseUnbriefedSeat) by the time it returns an error;
			// registering nothing models that end state.
			return &t433DeliveryError{verbatim}
		},
	}

	reps := s.frontierConsumeSweep(loopArgs, ledger)
	var got *FrontierConsumeReport
	for i := range reps {
		if reps[i].TargetID == "T900" {
			got = &reps[i]
		}
	}
	if got == nil || got.Action != FrontierConsumePark || got.Reason != FrontierReasonSpawnFailed {
		t.Fatalf("T900 report = %+v, want park/%s", got, FrontierReasonSpawnFailed)
	}

	// State (ii): the PO was told, naming the leaf and the error verbatim.
	if len(po.sent) != 1 {
		t.Fatalf("PO deliveries = %d, want 1: %v", len(po.sent), po.sent)
	}
	for _, want := range []string{"🎯T900", "jv-t900-auto", verbatim} {
		if !strings.Contains(po.sent[0], want) {
			t.Errorf("PO notice missing %q:\n%s", want, po.sent[0])
		}
	}

	// Never a third state: no briefless agent retained for the target.
	if def := reg.Def("jv-t900-auto"); def != nil {
		t.Fatalf("briefless worker still registered: %+v", def)
	}

	// A second sweep failing identically re-parks but does not drumbeat the
	// PO with a byte-identical notice. The PO's turn is marked ended first —
	// otherwise a second send would be QUEUED behind the in-flight turn and
	// po.sent would stay at 1 even with the dedupe deleted, proving nothing.
	po.inFlight = false
	s.noteTurnEnded("jevons-po")
	reps = s.frontierConsumeSweep(loopArgs, ledger)
	for _, r := range reps {
		if r.TargetID == "T900" && r.Reason != FrontierReasonSpawnFailed {
			t.Fatalf("second sweep T900 = %+v", r)
		}
	}
	if len(po.sent) != 1 {
		t.Fatalf("duplicate spawn-failure notice sent: %v", po.sent)
	}

	// A DIFFERENT failure is news again, not a suppressed repeat.
	verbatim = t433Specimens[1]
	po.inFlight = false
	s.noteTurnEnded("jevons-po")
	s.frontierConsumeSweep(loopArgs, ledger)
	if len(po.sent) != 2 || !strings.Contains(po.sent[1], t433Specimens[1]) {
		t.Fatalf("changed error not re-surfaced: %v", po.sent)
	}
}

// TestT433ControlHealthySpawnSendsNoFailureNotice is the control: the same
// harness with a spawn that succeeds delivers nothing to the PO, so the
// failure oracle above cannot be passing on ambient chatter.
func TestT433ControlHealthySpawnSendsNoFailureNotice(t *testing.T) {
	s, reg, po, dir := t433Harness(t)
	ledger, err := OpenFrontierSpawnLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reps := s.frontierConsumeSweep(FrontierConsumeLoopArgs{
		Server:   s,
		Workdir:  dir,
		ParentPO: "jevons-po",
		Spawn: func(leaf targetfile.FrontierLeaf, workerName, parent string) error {
			return reg.Register(claudia.AgentDef{
				Name: workerName, WorkDir: dir, SessionID: "spawned",
				Purpose: claudia.PurposeWork, Parent: parent, TargetID: leaf.ID,
			})
		},
	}, ledger)
	var spawned bool
	for _, r := range reps {
		if r.TargetID == "T900" && r.Action == FrontierConsumeSpawn {
			spawned = true
		}
	}
	if !spawned {
		t.Fatalf("control did not spawn: %+v", reps)
	}
	for _, msg := range po.sent {
		if strings.Contains(msg, "[spawn-failure") {
			t.Fatalf("healthy spawn produced a failure notice: %q", msg)
		}
	}
}

// TestT433NoticeStaysOnThePO pins the routing hazard: the notice travels to
// a PO with OriginAgent, and deliverByName bounces agent-origin PO mail to
// the overseer when relayroute classifies it RouteOverseer (🎯T392.7) —
// leaving the PO only a record line, which is the silence this target ends.
// Both real error specimens must classify RouteParent inside the notice.
func TestT433NoticeStaysOnThePO(t *testing.T) {
	t.Parallel()
	for _, errText := range t433Specimens {
		notice := FormatSpawnFailureNotice("T383", "jv-t383-auto", errText)
		if got := relayroute.Classify(notice); got != relayroute.RouteParent {
			t.Errorf("notice classifies %s (would skip the PO):\n%s", got, notice)
		}
		for _, want := range []string{"🎯T383", "jv-t383-auto", errText, FrontierReasonSpawnFailed} {
			if !strings.Contains(notice, want) {
				t.Errorf("notice missing %q:\n%s", want, notice)
			}
		}
	}
}

// t433DeliveryError carries the specimen text unchanged, so the verbatim
// assertion cannot be satisfied by a rewrapped paraphrase.
type t433DeliveryError struct{ msg string }

func (e *t433DeliveryError) Error() string { return e.msg }
