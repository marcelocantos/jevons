// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatstate

import (
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/turnev"
)

// 🎯T766.2: the authority is only worth having if unknown survives and
// staleness is part of the answer. Everything else it does is bookkeeping.

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time       { return c.t }
func (c *clock) tick(d time.Duration) { c.t = c.t.Add(d) }

func newAt(t time.Time, stale time.Duration) (*Authority, *clock) {
	c := &clock{t: t}
	return New(Args{Now: c.now, Stale: stale}), c
}

// The zero value of a report means "I am not telling you", never "false".
// A struct nobody filled in must not be able to assert anything.
func TestZeroObservationAssertsNothing(t *testing.T) {
	a, _ := newAt(at("2026-09-21T00:00:00Z"), time.Minute)
	a.Observe(Observation{Name: "jv-1", Source: "test", Alive: Yes, InFlight: Yes})

	// A later report that knows only the model must not flip alive to false.
	a.Observe(Observation{Name: "jv-1", Source: "test", Model: "opus"})

	s, ok := a.Get("jv-1")
	if !ok {
		t.Fatal("seat vanished")
	}
	if s.Alive != Yes {
		t.Fatalf("Alive = %s after a report that said nothing about it, want yes", s.Alive)
	}
	if s.InFlight != Yes {
		t.Fatalf("InFlight = %s, want yes", s.InFlight)
	}
	if s.Model != "opus" {
		t.Fatalf("Model = %q, want opus", s.Model)
	}
}

// Not knowing is not the same as knowing it is false, and the type must not
// let a caller confuse them.
func TestUnknownIsNotFalse(t *testing.T) {
	if Unknown.Known() {
		t.Fatal("Unknown claims to carry information")
	}
	if !No.Known() || !Yes.Known() {
		t.Fatal("an observed value claims ignorance")
	}
	if Unknown == No {
		t.Fatal("Unknown and No are the same value — the distinction this package exists for")
	}
	var fresh State
	if fresh.Alive != Unknown || fresh.InFlight != Unknown {
		t.Fatal("the zero State asserts something")
	}
	if fresh.Phase != turnev.PhaseUnknown {
		t.Fatal("the zero State claims a phase")
	}
}

// The property that stops a blind supervisor reporting calm: once an
// observation is older than the window, its condition fields read unknown.
func TestStaleConditionsDecayButIdentityDoesNot(t *testing.T) {
	a, c := newAt(at("2026-09-21T00:00:00Z"), 2*time.Minute)
	a.Observe(Observation{
		Name: "jv-1", Source: "claudia.info", Provider: "claude", Model: "opus",
		Alive: Yes, InFlight: Yes, Phase: turnev.PhaseWorking, QueueDepth: 3,
	})

	c.tick(90 * time.Second)
	s, _ := a.Get("jv-1")
	if s.Alive != Yes || s.Phase != turnev.PhaseWorking || s.QueueDepth != 3 {
		t.Fatalf("a fresh reading decayed early: %+v", s)
	}

	c.tick(60 * time.Second) // now 2m30s old
	s, ok := a.Get("jv-1")
	if !ok {
		t.Fatal("a seat we have lost sight of must still be known to exist")
	}
	if s.Alive != Unknown || s.InFlight != Unknown || s.QueueDepth != QueueUnknown {
		t.Fatalf("stale conditions were served as current: %+v", s)
	}
	if s.Phase != turnev.PhaseUnknown {
		t.Fatalf("stale phase = %s, want unknown", s.Phase)
	}
	// Identity is not a condition and does not expire.
	if s.Provider != "claude" || s.Model != "opus" || s.Name != "jv-1" {
		t.Fatalf("identity decayed: %+v", s)
	}
	// And the caller can still see how old the evidence was.
	if s.Observed.IsZero() || s.Source != "claudia.info" {
		t.Fatalf("provenance lost: observed=%v source=%q", s.Observed, s.Source)
	}
}

// A seat nobody has reported and a seat we have lost sight of call for
// different actions, so they must be distinguishable (🎯T422 clause 5).
func TestNeverHeardOfIsNotTheSameAsLostSightOf(t *testing.T) {
	a, c := newAt(at("2026-09-21T00:00:00Z"), time.Minute)
	if _, ok := a.Get("ghost"); ok {
		t.Fatal("a seat nobody reported was answered for")
	}
	a.Observe(Observation{Name: "jv-1", Source: "test", Alive: Yes})
	c.tick(10 * time.Minute)
	s, ok := a.Get("jv-1")
	if !ok {
		t.Fatal("a seat we knew about became indistinguishable from one we never saw")
	}
	if s.Alive != Unknown {
		t.Fatalf("Alive = %s, want unknown", s.Alive)
	}
}

// Sources know different things. The fold must combine them rather than let
// the last writer win — the ACP stream knows the phase, the queue knows its
// depth, and neither should erase the other.
func TestPartialSourcesCombine(t *testing.T) {
	a, _ := newAt(at("2026-09-21T00:00:00Z"), time.Minute)
	a.Observe(Observation{Name: "jv-1", Source: "claudia.info", Provider: "cursor", Alive: Yes, QueueDepth: QueueUnknown})
	a.Observe(Observation{Name: "jv-1", Source: "acp.event", Phase: turnev.PhaseWorking, QueueDepth: QueueUnknown})
	a.Observe(Observation{Name: "jv-1", Source: "sendq", QueueDepth: 12})

	s, _ := a.Get("jv-1")
	if s.Provider != "cursor" || s.Alive != Yes {
		t.Fatalf("claudia's report was lost: %+v", s)
	}
	if s.Phase != turnev.PhaseWorking {
		t.Fatalf("the event stream's report was lost: %+v", s)
	}
	if s.QueueDepth != 12 {
		t.Fatalf("QueueDepth = %d, want 12", s.QueueDepth)
	}
	if s.Source != "sendq" {
		t.Fatalf("Source = %q, want the most recent reporter", s.Source)
	}
}

// A late-arriving old report must not rewind what we know.
func TestAnOlderReportDoesNotRewindTheClock(t *testing.T) {
	a, _ := newAt(at("2026-09-21T00:02:00Z"), time.Hour)
	a.Observe(Observation{Name: "jv-1", Source: "new", Alive: Yes, At: at("2026-09-21T00:02:00Z")})
	a.Observe(Observation{Name: "jv-1", Source: "old", Alive: No, At: at("2026-09-21T00:00:00Z")})

	s, _ := a.Get("jv-1")
	if s.Observed != at("2026-09-21T00:02:00Z") {
		t.Fatalf("Observed = %v, want the newer time", s.Observed)
	}
	if s.Source != "new" {
		t.Fatalf("Source = %q, want the newer reporter to own provenance", s.Source)
	}
}

// An observation with no provenance is refused: an answer nobody can trace
// is how a wrong reading survives a post-mortem.
func TestUnattributedObservationsAreRefused(t *testing.T) {
	a, _ := newAt(at("2026-09-21T00:00:00Z"), time.Minute)
	a.Observe(Observation{Name: "jv-1", Alive: Yes})
	if _, ok := a.Get("jv-1"); ok {
		t.Fatal("an observation with no Source was accepted")
	}
	a.Observe(Observation{Source: "test", Alive: Yes})
	if len(a.Snapshot()) != 0 {
		t.Fatal("an observation with no Name was accepted")
	}
}

// An int field cannot express "no claim" through its zero value, so a seat
// nobody has reported a queue for must not read as having an empty one.
func TestAnUnreportedQueueIsUnknownNotEmpty(t *testing.T) {
	a, _ := newAt(at("2026-09-21T00:00:00Z"), time.Minute)
	a.Observe(Observation{Name: "jv-1", Source: "claudia.info", Alive: Yes, QueueDepth: QueueUnknown})

	s, _ := a.Get("jv-1")
	if s.QueueDepth != QueueUnknown {
		t.Fatalf("QueueDepth = %d for a seat nobody reported a queue for, want unknown", s.QueueDepth)
	}
	// And a real report of zero is a different, meaningful answer.
	a.Observe(Observation{Name: "jv-1", Source: "sendq", QueueDepth: 0})
	s, _ = a.Get("jv-1")
	if s.QueueDepth != 0 {
		t.Fatalf("QueueDepth = %d after a source reported an empty queue, want 0", s.QueueDepth)
	}
}

func TestForgetRemovesTheSeatEntirely(t *testing.T) {
	a, _ := newAt(at("2026-09-21T00:00:00Z"), time.Minute)
	a.Observe(Observation{Name: "jv-1", Source: "test", Alive: Yes})
	a.Forget("jv-1")
	if _, ok := a.Get("jv-1"); ok {
		t.Fatal("a forgotten seat still answers")
	}
}

func TestSnapshotIsOrderedAndDecayed(t *testing.T) {
	a, c := newAt(at("2026-09-21T00:00:00Z"), time.Minute)
	a.Observe(Observation{Name: "b", Source: "test", Alive: Yes})
	a.Observe(Observation{Name: "a", Source: "test", Alive: Yes})
	c.tick(2 * time.Minute)
	a.Observe(Observation{Name: "c", Source: "test", Alive: Yes})

	got := a.Snapshot()
	if len(got) != 3 || got[0].Name != "a" || got[1].Name != "b" || got[2].Name != "c" {
		t.Fatalf("snapshot not ordered by name: %+v", got)
	}
	if got[0].Alive != Unknown || got[1].Alive != Unknown {
		t.Fatal("stale seats were served as alive in a snapshot")
	}
	if got[2].Alive != Yes {
		t.Fatal("a fresh seat decayed in a snapshot")
	}
}
