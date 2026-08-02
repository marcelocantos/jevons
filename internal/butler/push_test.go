// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package butler_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/butler"
)

func TestFormatEventPush(t *testing.T) {
	got := butler.FormatEventPush("ci", "green on master")
	if got != "[event: ci] green on master" {
		t.Fatalf("FormatEventPush = %q", got)
	}
	got = butler.FormatEventPush("  ", "  hello  ")
	if got != "[event: unknown] hello" {
		t.Fatalf("empty source default = %q", got)
	}
}

// TestPushEventRehydratesAndDelivers is the 🎯T34 e2e oracle: event fires
// → target (possibly stopped) receives the push after rehydrate.
func TestPushEventRehydratesAndDelivers(t *testing.T) {
	f := newFakeFleet()
	f.mintID = "eeeeeeee-ffff-4000-8000-000000000001"
	b := newLifecycleButler(t, t.TempDir(), f)

	if _, err := b.Spawn(butler.SpawnArgs{ID: "po", WorkDir: "/work/x", Description: "PO"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	f.Stop("po") // idle / GC'd

	reply, err := b.PushEvent("po", "worker-finished", "slice A landed")
	if err != nil {
		t.Fatalf("PushEvent: %v", err)
	}
	if f.launches < 2 {
		t.Fatalf("expected rehydrate launch, launches=%d", f.launches)
	}
	if !strings.Contains(reply, "[event: worker-finished]") || !strings.Contains(reply, "slice A landed") {
		t.Fatalf("reply missing event payload: %q", reply)
	}
}

func TestPushEventUnreachableTypedError(t *testing.T) {
	f := newFakeFleet()
	f.mintID = "eeeeeeee-ffff-4000-8000-000000000002"
	b := newLifecycleButler(t, t.TempDir(), f)
	if _, err := b.Spawn(butler.SpawnArgs{ID: "w", WorkDir: "/work/x"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	f.Stop("w")
	f.launchErr = errFakeUnreachable

	_, err := b.PushEvent("w", "timer", "tick")
	if err == nil {
		t.Fatal("want typed error for unreachable agent")
	}
	if !strings.Contains(err.Error(), "push") || !strings.Contains(err.Error(), "rehydrate") {
		// Direct wraps rehydrate failures; PushEvent prefixes push.
		if !strings.Contains(err.Error(), "could not rehydrate") && !strings.Contains(err.Error(), "push") {
			t.Fatalf("unexpected error shape: %v", err)
		}
	}
}

func TestPushEventMissingTarget(t *testing.T) {
	f := newFakeFleet()
	b := newLifecycleButler(t, t.TempDir(), f)
	_, err := b.PushEvent("nope", "ci", "green")
	if err == nil {
		t.Fatal("want error")
	}
	// Unified deliver vocabulary (🎯T111.2 / 🎯T114): no silent thread-only wording
	// when the target is simply unknown.
	if !strings.Contains(err.Error(), "no participant") && !strings.Contains(err.Error(), "no thread") {
		t.Fatalf("want missing-target error, got %v", err)
	}
}

// 🎯T111.2: agent-only participant (no threads.json row) receives push.
func TestPushEventAgentOnlyParticipant(t *testing.T) {
	f := newFakeFleet()
	b := newLifecycleButler(t, t.TempDir(), f)
	p := &fakeParticipants{
		exists: map[string]bool{"jevons-po": true},
		reply:  "ack",
	}
	b.SetParticipants(p)

	reply, err := b.PushEvent("jevons-po", "worker-finished", "slice A landed")
	if err != nil {
		t.Fatalf("PushEvent agent: %v", err)
	}
	if !strings.Contains(p.lastText, "[event: worker-finished]") || !strings.Contains(p.lastText, "slice A landed") {
		t.Fatalf("agent did not receive event payload: %q", p.lastText)
	}
	if reply != "ack" {
		t.Fatalf("reply=%q", reply)
	}
	// Must not look like the old thread-only failure.
	if strings.Contains(reply, "no thread") {
		t.Fatal("unexpected no-thread in success path")
	}
}

// 🎯T111.3: Spawn records Parent on the durable thread for fleet Launch.
func TestSpawnRecordsParent(t *testing.T) {
	f := newFakeFleet()
	f.mintID = "eeeeeeee-ffff-4000-8000-000000000098"
	b := newLifecycleButler(t, t.TempDir(), f)
	th, err := b.Spawn(butler.SpawnArgs{ID: "po", WorkDir: "/work/x", Parent: "jevons"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if th.Parent != "jevons" {
		t.Fatalf("Parent=%q want jevons", th.Parent)
	}
}

// 🎯T114: thread spawn defaults purpose=aside (side-chat participant).
func TestSpawnDefaultsPurposeAside(t *testing.T) {
	f := newFakeFleet()
	f.mintID = "eeeeeeee-ffff-4000-8000-000000000097"
	b := newLifecycleButler(t, t.TempDir(), f)
	th, err := b.Spawn(butler.SpawnArgs{ID: "aside-1", WorkDir: "/work/x", Parent: "jevons"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if th.Purpose != "aside" {
		t.Fatalf("Purpose=%q want aside", th.Purpose)
	}
}

// 🎯T114: send-to-aside (thread) and send-to-worker (agent) share Deliver.
func TestDeliverSameAPIThreadAndAgent(t *testing.T) {
	f := newFakeFleet()
	f.mintID = "eeeeeeee-ffff-4000-8000-000000000096"
	b := newLifecycleButler(t, t.TempDir(), f)
	p := &fakeParticipants{exists: map[string]bool{"worker-a": true}}
	b.SetParticipants(p)

	if _, err := b.Spawn(butler.SpawnArgs{ID: "aside-1", WorkDir: "/work/x", Description: "nit"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	r1, err := b.Deliver("aside-1", "hello aside")
	if err != nil {
		t.Fatalf("Deliver thread: %v", err)
	}
	if !strings.Contains(r1, "hello aside") {
		t.Fatalf("thread reply: %q", r1)
	}
	r2, err := b.Deliver("worker-a", "hello worker")
	if err != nil {
		t.Fatalf("Deliver agent: %v", err)
	}
	if !strings.Contains(r2, "hello worker") && r2 != "ack" {
		// default fakeParticipants reply embeds the text
		if p.lastText != "hello worker" {
			t.Fatalf("agent path lastText=%q reply=%q", p.lastText, r2)
		}
	}
	if p.lastID != "worker-a" {
		t.Fatalf("lastID=%q want worker-a", p.lastID)
	}
}

// errFakeUnreachable is a stable error so rehydrate failure is observable.
var errFakeUnreachable = errString("process unreachable")

type errString string

func (e errString) Error() string { return string(e) }

// fakeParticipants implements butler.Participants for agent-only push tests.
type fakeParticipants struct {
	exists   map[string]bool
	reply    string
	err      error
	lastID   string
	lastText string
}

func (p *fakeParticipants) Exists(id string) bool { return p.exists[id] }

func (p *fakeParticipants) Deliver(id, text string) (string, error) {
	p.lastID = id
	p.lastText = text
	if p.err != nil {
		return "", p.err
	}
	if p.reply == "" {
		return "reply to: " + text, nil
	}
	return p.reply, nil
}

// ensure interface satisfaction at compile time.
var _ butler.Participants = (*fakeParticipants)(nil)

// silence unused if Deliver signature drifts in future refactors
var _ = fmt.Sprintf
