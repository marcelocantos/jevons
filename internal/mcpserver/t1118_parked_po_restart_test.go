// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// TestNotifyDaemonRestartedLeavesParkedCoordinatorDown is the 🎯T1118
// acceptance-1 hermetic for the daemon-restart resume call site (the "PO
// raise" / post-restart resume sweep the ledger names). It reproduces the
// exact shape of the 2026-10-21 20:01:44 incident: a durable PO row carrying
// per-agent intent `parked`, with a LIVE registered work child (so the
// pre-fix workChildren>0 guard would have refused to skip it) — and asserts
// the real call site (NotifyDaemonRestarted, exercising
// SkipSleepingCoordinatorRestart exactly as the restart sweep does) still
// never sends to, and therefore never rehydrates, the parked PO.
//
// RED on the pre-fix code: SkipSleepingCoordinatorRestart had no per-agent
// intent parameter at all, so a parked PO with a live work child sailed
// past every one of its guards (processAlive=false, openIntent=false,
// workChildren>0 → skip=false) and NotifyDaemonRestarted sent to it,
// rehydrating a seat the owner had parked three minutes after restart
// reattach had correctly left it down.
func TestNotifyDaemonRestartedLeavesParkedCoordinatorDown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const overseer = "jevons"
	const poName = "multimaze2-po"
	const workerName = "multimaze2-worker"

	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: overseer, Purpose: claudia.PurposeOverseer},
		{Name: poName, Parent: overseer, Purpose: claudia.PurposeWork, Role: "product-owner"},
		{Name: workerName, Parent: poName, Purpose: claudia.PurposeWork, TargetID: "T1"},
	} {
		d.WorkDir = dir
		d.SessionID = "s-" + d.Name
		d.Materialized = true
		d.Provider = "claude"
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}

	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAgent(poName, fleetintent.Parked, "jevons", "nothing left for this worker", time.Now()); err != nil {
		t.Fatal(err)
	}

	inbox := newT452Inbox()
	s := &Server{registry: reg, bootAt: time.Now()}
	s.SetFleetIntentStore(store)
	setObservedSenderResolver(s, func(name string) (agentSender, bool, error) {
		return t452Seat{dest: name, inbox: inbox}, false, nil
	})
	s.SetOverseerDeliver(func(text string, _ SendOrigin) error {
		inbox.put(overseer, text)
		return nil
	})
	s.SetTurnWitness(func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, PayloadSeen: true}
		}
	})

	var logbuf bytes.Buffer
	prevLog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logbuf, nil)))
	defer slog.SetDefault(prevLog)

	// defaultPO = poName so DaemonRestartEventTargets' empty-byParent
	// fallback puts the parked PO itself on the target list without
	// needing a live claudia process handle (reg.Get would be nil for a
	// row this test never Launch-ed; CountWorkChildren, unlike the
	// running-child grouping, counts the registered row regardless).
	// The registered-but-unlaunched work child still gives
	// restartNotifyWorkChildren > 0 — the exact pre-fix workChildren>0
	// guard that let a parked PO with "work to hand off" sail through.
	s.NotifyDaemonRestarted(overseer, poName, dir)

	got := inbox.snapshot()
	if msgs := got[poName]; len(msgs) != 0 {
		t.Fatalf("parked PO %q must never receive a daemon-restart resume send, got: %v", poName, msgs)
	}

	log := logbuf.String()
	if !strings.Contains(log, `msg="daemon restart skip sleeping coordinator"`) ||
		!strings.Contains(log, "target="+poName) || !strings.Contains(log, "reason=agent_intent") {
		t.Fatalf("expected a skip log naming %q with reason=agent_intent; got:\n%s", poName, log)
	}
	// 🎯T1118: the intent store's own promise for a park is "nothing
	// revives it — not a delivery, not a restart, not the idle sweep". A
	// delivery attempt reaching the parked PO's own agent_send machinery
	// at all is exactly the shape that rehydrated multimaze2-po on
	// 2026-10-21: the skip above must happen before any send is tried,
	// not merely before the send succeeds.
	if strings.Contains(log, "msg=agent_send") && strings.Contains(log, "name="+poName) {
		t.Fatalf("parked PO %q must never reach the agent_send path at all; got:\n%s", poName, log)
	}
}
