// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"strings"
	"testing"
)

// The 2026-09-20 refusal, from the live capacity API: claude at 12 of a
// published soft cap of 12, the session census blind at 0 of 20, and three
// Build spawns refused. The number every product owner was shown as the
// reason — "0 live sessions of 20" — was not the dimension that decided.
func saturatedProviderSnapshot() Snapshot {
	return Snapshot{
		Accounting:       "subscription",
		MaxSessions:      20,
		ActiveSessions:   0,
		ProviderLoad:     map[string]int{"claude": 12, "cursor": 1, "grok": 1},
		ProviderSoftCaps: map[string]int{"claude": 12, "codex": 6, "grok": 12},
	}
}

func TestT708SeatReasonNamesTheProviderThatBound(t *testing.T) {
	snap := saturatedProviderSnapshot()
	pol := DefaultPolicy()
	a := Assess(snap, pol)
	if !seatCountBlocks(a) {
		t.Fatalf("seat halt did not fire; seat headroom %.2f", a.SeatHeadroom)
	}
	reason := seatCountReason(snap, pol, a)
	if !strings.Contains(reason, "claude") {
		t.Errorf("reason does not name the provider that bound: %s", reason)
	}
	if !strings.Contains(reason, "12") {
		t.Errorf("reason does not carry the cap that bound: %s", reason)
	}
	// The exact sentence that sent two product owners after a session
	// counter that had refused nobody.
	if strings.Contains(reason, "0 live sessions of 20") || strings.Contains(reason, "0 live seats of 20") {
		t.Errorf("reason still reports the blind session census as the cause: %s", reason)
	}
}

func TestT708SeatDimensionPicksTheTightest(t *testing.T) {
	snap := saturatedProviderSnapshot()
	b := SeatDimension(snap, DefaultPolicy())
	if b.Provider != "claude" || b.Used != 12 || b.Limit != 12 {
		t.Fatalf("binding = %+v, want claude 12/12", b)
	}
	if b.Inferred {
		t.Error("claude publishes a cap; it must not be reported as assumed")
	}
}

// The control: when the session census really is the tightest dimension,
// the reason names sessions and nothing else.
func TestT708SeatReasonNamesSessionsWhenSessionsBind(t *testing.T) {
	snap := Snapshot{
		MaxSessions:      20,
		ActiveSessions:   20,
		ProviderLoad:     map[string]int{"claude": 1},
		ProviderSoftCaps: map[string]int{"claude": 12},
	}
	pol := DefaultPolicy()
	a := Assess(snap, pol)
	reason := seatCountReason(snap, pol, a)
	if !strings.Contains(reason, "20 live seats of 20") {
		t.Fatalf("reason does not name the session census: %s", reason)
	}
	if strings.Contains(reason, "provider") {
		t.Errorf("reason blames a provider that had eleven seats spare: %s", reason)
	}
}

// An assumed cap must say it is assumed: a denominator this package made up
// must never read as a published bound (🎯T463).
func TestT708SeatReasonMarksAnAssumedCap(t *testing.T) {
	snap := Snapshot{
		MaxSessions:      20,
		ActiveSessions:   2,
		ProviderLoad:     map[string]int{"claude": 40},
		ProviderSoftCaps: map[string]int{"claude": 0},
	}
	pol := DefaultPolicy()
	b := SeatDimension(snap, pol)
	if b.Provider != "claude" || !b.Inferred {
		t.Fatalf("binding = %+v, want an inferred claude cap", b)
	}
	if reason := seatCountReason(snap, pol, Assess(snap, pol)); !strings.Contains(reason, "assumed") {
		t.Errorf("reason presents a made-up cap as published: %s", reason)
	}
}
