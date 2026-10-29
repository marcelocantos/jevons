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
	"github.com/marcelocantos/jevons/internal/sendq"
)

func t1229ParkedDaemon(t *testing.T, name string, st fleetintent.State) (*Server, *recordingSender) {
	t.Helper()
	dir := t.TempDir()
	s, sender, _ := t418Daemon(t, dir)
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	if err := s.SetAgentIntent(name, st, "codex-owner-session", "informational notices causing model turns"); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-" + name,
		Purpose: claudia.PurposeWork, Role: "product-owner", Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	s.registry = reg
	return s, sender
}

// 🎯T1229 acceptance 3: hermetic through the real repo-watch Deliverer.
// A parked seat with a live fake process gets zero model calls and one
// queued item; lifting intent to working drains it.
func TestT1229RepoWatchDelivererHoldsParkedLiveSeat(t *testing.T) {
	const name = "jevons-po"
	const payload = "PR #75 opened: fix the leak"

	s, sender := t1229ParkedDaemon(t, name, fleetintent.Parked)
	d := s.NewPODeliverer()
	if err := d.Deliver(name, payload); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got := sender.delivered(); len(got) != 0 {
		t.Fatalf("model turn began on a parked live seat: %v", got)
	}
	entries, err := s.sendQueue().Snapshot(name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Pending {
		t.Fatalf("want one queued item: %+v %v", entries, err)
	}
	if !strings.Contains(entries[0].Text, payload) {
		t.Fatalf("queued text %q does not carry the brief", entries[0].Text)
	}

	if err := s.SetAgentIntent(name, fleetintent.Working, "owner", "resume"); err != nil {
		t.Fatal(err)
	}
	s.drainAgentSendQueue(name)
	got := sender.delivered()
	if len(got) != 1 {
		t.Fatalf("item did not drain after state=working: %v", got)
	}
	if !strings.Contains(got[0], payload) {
		t.Fatalf("drained %q, want payload", got[0])
	}
}

func TestT1229RepoWatchDelivererHoldsBlockedProviderLiveSeat(t *testing.T) {
	const name = "jevons-po"
	s, sender := t1229ParkedDaemon(t, name, fleetintent.BlockedProvider)
	if err := s.NewPODeliverer().Deliver(name, "issue #1 updated"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got := sender.delivered(); len(got) != 0 {
		t.Fatalf("model turn began under blocked_provider: %v", got)
	}
	entries, err := s.sendQueue().Snapshot(name)
	if err != nil || len(entries) != 1 {
		t.Fatalf("want one queued item: %+v %v", entries, err)
	}
}

// Owner-origin send to a parked live seat keeps current behaviour (🎯T1066
// may hold the idle submit) and the result says the seat is parked.
func TestT1229OwnerSendToParkedLiveSeatMentionsParked(t *testing.T) {
	const name = "jevons-po"
	s, sender := t1229ParkedDaemon(t, name, fleetintent.Parked)
	s.SetTurnWitness(func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, PayloadSeen: true}
		}
	})
	res, err := s.deliverByName(name, "owner: status of the park?", OriginOwner, false)
	if err != nil {
		t.Fatalf("owner send: %v", err)
	}
	if !strings.Contains(strings.ToLower(res.Message), "parked") {
		t.Fatalf("owner send result does not say parked: %q", res.Message)
	}
	// Current behaviour for an idle parked live seat is T1066 hold (queued,
	// not a model turn). That is the parked-seat answer the owner asked to keep.
	if res.Status == "queued" {
		if got := sender.delivered(); len(got) != 0 {
			t.Fatalf("queued owner send still reached the process: %v", got)
		}
		return
	}
	if got := sender.delivered(); len(got) != 1 {
		t.Fatalf("owner send status=%q delivered=%v", res.Status, got)
	}
}

func TestT1229OverseerDirectSendToParkedLiveSeatMentionsParked(t *testing.T) {
	const name = "jevons-po"
	s, sender := t1229ParkedDaemon(t, name, fleetintent.Parked)
	s.SetTurnWitness(func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, PayloadSeen: true}
		}
	})
	res, err := s.deliverByNameAs("jevons", name, "overseer: look at this", OriginAgent, false)
	if err != nil {
		t.Fatalf("overseer-direct send: %v", err)
	}
	if !strings.Contains(strings.ToLower(res.Message), "parked") {
		t.Fatalf("overseer-direct result does not say parked: %q", res.Message)
	}
	if res.Status == "queued" {
		if got := sender.delivered(); len(got) != 0 {
			t.Fatalf("queued overseer-direct send still reached the process: %v", got)
		}
	}
}

func TestT1229HoldAmbientInsteadOfPushDoesNotCallButler(t *testing.T) {
	const name = "jevons-po"
	s, sender := t1229ParkedDaemon(t, name, fleetintent.Parked)
	held, err := s.holdAmbientInsteadOfPush(name, "sentinel: leftover")
	if err != nil || !held {
		t.Fatalf("hold: held=%v err=%v", held, err)
	}
	if got := sender.delivered(); len(got) != 0 {
		t.Fatalf("push-guard started a model turn: %v", got)
	}
	entries, err := s.sendQueue().Snapshot(name)
	if err != nil || len(entries) != 1 {
		t.Fatalf("want queued: %+v %v", entries, err)
	}
}

func TestT1229WorkingIntentDoesNotHoldAmbient(t *testing.T) {
	const name = "jevons-po"
	s, sender := t1229ParkedDaemon(t, name, fleetintent.Working)
	d := s.NewPODeliverer()
	if err := d.Deliver(name, "PR #1"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	// Working intent: the live fake process is offered the brief.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(sender.delivered()) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := sender.delivered(); len(got) == 0 {
		t.Fatal("working-intent repo-watch brief never reached the live process")
	}
}
