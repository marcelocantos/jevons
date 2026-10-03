// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/wakebatch"
)

// 🎯T730: a worker-idle notice names a seat that exists and is actually idle.
//
// Specimen 2026-09-21: a coalesced batch named five seats under jevons-po;
// GET /api/agents at that moment showed four ABSENT (reaped) and one
// PRESENT with phase=working. T451 already dropped absent names in a
// helper with a fake present map; flushWakeBatches — the product assembly
// path — was never called from a test, and never re-checked phase.
//
// Hermetic: a batch assembled over a window in which two of five seats are
// reaped, and one of the survivors is working, emits only for the genuinely
// idle survivors. A mutation that restores emission for reaped names goes RED.

func TestIdleNoticeStillCurrent(t *testing.T) {
	t.Parallel()
	def := &claudia.AgentDef{Name: "jv-alive"}
	if IdleNoticeStillCurrent(nil, "idle") {
		t.Fatal("reaped/absent seat (nil def) must not be current, even with stale idle phase")
	}
	if IdleNoticeStillCurrent(def, "working") {
		t.Fatal("registered working seat must not be named idle")
	}
	if IdleNoticeStillCurrent(def, "WORKING") {
		t.Fatal("phase compare is case-insensitive")
	}
	if !IdleNoticeStillCurrent(def, "idle") {
		t.Fatal("registered idle seat is current")
	}
	if !IdleNoticeStillCurrent(def, "") {
		t.Fatal("registered seat with unknown phase is kept — empty is not working")
	}
}

func TestT730FlushDropsReapedAndWorking(t *testing.T) {
	t.Parallel()
	s, fx := t730Batch(t)

	var got []string
	n := s.flushWakeBatches(func(recipient, text string) error {
		if recipient != "jevons-po" {
			t.Errorf("digest recipient %q, want jevons-po", recipient)
		}
		got = append(got, text)
		return nil
	})
	if n != 1 {
		t.Fatalf("woke %d recipients, want 1; digest=%v", n, got)
	}
	if len(got) != 1 {
		t.Fatalf("sent %d digests, want 1: %v", len(got), got)
	}
	text := got[0]
	for _, name := range fx.idle {
		if !strings.Contains(text, name) {
			t.Errorf("digest dropped genuinely-idle survivor %s:\n%s", name, text)
		}
	}
	for _, name := range fx.reaped {
		if strings.Contains(text, name) {
			t.Errorf("digest named reaped seat %s:\n%s", name, text)
		}
	}
	if strings.Contains(text, fx.working) {
		t.Errorf("digest named working seat %s:\n%s", fx.working, text)
	}
}

func TestT730MutationRestoringReapedNamesGoesRed(t *testing.T) {
	t.Parallel()
	s, fx := t730Batch(t)

	evs := t730QueuedEvents(fx)
	mutant := FilterIdleEventsForLiveAgents(evs, func(string) bool { return true })
	mutantText := wakebatch.FormatDigest(mutant)
	for _, name := range fx.reaped {
		if !strings.Contains(mutantText, name) {
			t.Fatalf("mutant setup: restoring emission must still name reaped seat %s", name)
		}
	}

	live := FilterIdleEventsForLiveAgents(evs, s.idleNoticeLive)
	liveText := wakebatch.FormatDigest(live)
	for _, name := range fx.reaped {
		if strings.Contains(liveText, name) {
			t.Fatalf("live lookup still emits reaped name %s — restore-emission mutant is live:\n%s", name, liveText)
		}
	}

	var flushed string
	s.flushWakeBatches(func(_, text string) error {
		flushed = text
		return nil
	})
	for _, name := range fx.reaped {
		if strings.Contains(flushed, name) {
			t.Fatalf("flushWakeBatches named reaped seat %s — skipping the live filter in flush is live:\n%s", name, flushed)
		}
	}
	if strings.Contains(flushed, fx.working) {
		t.Fatalf("flushWakeBatches named working seat %s:\n%s", fx.working, flushed)
	}
	for _, name := range fx.idle {
		if !strings.Contains(flushed, name) {
			t.Fatalf("flushWakeBatches dropped idle survivor %s:\n%s", name, flushed)
		}
	}
}

type t730Fix struct {
	reaped  []string
	working string
	idle    []string
}

func t730Batch(t *testing.T) (*Server, t730Fix) {
	t.Helper()
	fx := t730Fix{
		reaped:  []string{"jv-t730-reaped-a", "jv-t730-reaped-b"},
		working: "jv-t730-working",
		idle:    []string{"jv-t730-idle-a", "jv-t730-idle-b"},
	}
	all := append(append([]string{}, fx.reaped...), fx.working)
	all = append(all, fx.idle...)

	defs := []claudia.AgentDef{{
		Name: "jevons-po", Purpose: claudia.PurposeWork, SessionID: "po",
	}}
	for i, name := range all {
		defs = append(defs, claudia.AgentDef{
			Name: name, Purpose: claudia.PurposeWork, Parent: "jevons-po",
			TargetID: "T730", SessionID: "w" + string(rune('1'+i)),
		})
	}
	s, _ := t451Server(t, t.TempDir(), defs...)
	activity := NewIdleActivityTracker()
	s.idleActivity = activity
	activity.SetAuthority(s.Seats())
	s.SetWakeBatchWindow(time.Nanosecond)

	for _, name := range all {
		activity.SeedRunning(name)
		s.emitWorkerIdleToParent(name, "working", "idle")
	}

	for _, name := range fx.reaped {
		if err := s.registry.Remove(name); err != nil {
			t.Fatalf("Remove %s: %v", name, err)
		}
		if s.registry.Def(name) != nil {
			t.Fatalf("Remove %s left Def non-nil", name)
		}
	}
	activity.Observe(fx.working, claudia.Event{Type: "assistant", Text: "mid-turn"})
	if got := activity.Get(fx.working).Phase; got != "working" {
		t.Fatalf("working seat phase=%q, want working", got)
	}
	return s, fx
}

func t730QueuedEvents(fx t730Fix) []wakebatch.Event {
	var evs []wakebatch.Event
	add := func(name string) {
		evs = append(evs, wakebatch.Event{
			Recipient: "jevons-po", Kind: eventWorkerIdle, Subject: name,
			Detail: "Worker: " + name, At: time.Time{},
		})
	}
	for _, name := range fx.reaped {
		add(name)
	}
	add(fx.working)
	for _, name := range fx.idle {
		add(name)
	}
	return evs
}
