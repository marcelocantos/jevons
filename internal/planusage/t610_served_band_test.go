// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"encoding/json"
	"testing"
	"time"
)

func pctOf(v float64) *float64 { return &v }

func bandWindow(name string, used, remaining float64, now time.Time, remTimeFrac float64) Window {
	lim := DefaultWeeklyWindowSeconds
	resets := now.Add(time.Duration(remTimeFrac * float64(lim) * float64(time.Second)))
	return Window{
		Name: name, UsedPercent: pctOf(used), RemainingPercent: pctOf(remaining),
		ResetsAt: &resets, LimitWindowSeconds: &lim,
	}
}

// 🎯T610. The cockpit painted a window red that the daemon did not consider
// hot, because the payload carried numbers and no verdict, so the browser
// classified for itself with a model a release behind. The fix is not a
// better copy of the rule in TypeScript — it is that there is nothing left
// to copy.
//
// The specimen is the one the owner hit: claude weekly, 36% used at ~18%
// elapsed. Ratio 2.0, which the superseded model called hot.
func TestServedBandIsTheDaemonsVerdict(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	th := DefaultThresholds()
	snap := Snapshot{Backends: []Backend{{
		Provider: "claude", Status: StatusAvailable,
		Windows: []Window{bandWindow(WindowWeekly, 36, 64, now, 0.82)},
	}}}

	got := WithBands(snap, now, th)
	band := got.Backends[0].Windows[0].Band
	// 🎯T641: the same specimen is ok under the owner-tuned vertices
	// (pressure 0.41 < amber 0.49). The T610 claim is that we serve the
	// daemon verdict, not that this window stays amber forever.
	if band != string(BandOK) {
		t.Fatalf("36%% used at ~18%% elapsed served as %q, want %q", band, BandOK)
	}
	// The control that makes the case mean something: the old ratio model
	// really would have called this hot, so a browser computing 36/18 = 2.0
	// against a 1.5 hot ratio disagrees with what we now serve.
	if ratio := 36.0 / 18.0; ratio <= 1.5 {
		t.Fatalf("fixture no longer exercises the disagreement: ratio %.2f", ratio)
	}
	// And it is the same answer the backend-level classifier gives, because
	// both go through BandOfWindow.
	if direct := WeeklyBandOf(snap.Backends[0], now, th); string(direct) != band {
		t.Fatalf("served band %q != WeeklyBandOf %q — two rules again", band, direct)
	}
}

// The snapshot is shared with every other reader. Decorating it in place
// would be a write on a struct other goroutines hold.
func TestWithBandsDoesNotMutateTheSharedSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	snap := Snapshot{Backends: []Backend{{
		Provider: "claude", Status: StatusAvailable,
		Windows: []Window{bandWindow(WindowWeekly, 80, 20, now, 0.5)},
	}}}

	_ = WithBands(snap, now, DefaultThresholds())
	if b := snap.Backends[0].Windows[0].Band; b != "" {
		t.Fatalf("source snapshot was mutated: band=%q", b)
	}
}

// The verdict is time-dependent: pressure divides by the time left, so the
// same numbers classify differently later in the window. A band computed
// when the snapshot was fetched would age; this one is as of the request.
func TestTheBandIsComputedAtRequestTime(t *testing.T) {
	th := DefaultThresholds()
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	early := WithBands(Snapshot{Backends: []Backend{{
		Provider: "claude", Status: StatusAvailable,
		Windows: []Window{bandWindow(WindowWeekly, 40, 60, start, 0.7)},
	}}}, start, th).Backends[0].Windows[0].Band
	late := WithBands(Snapshot{Backends: []Backend{{
		Provider: "claude", Status: StatusAvailable,
		Windows: []Window{bandWindow(WindowWeekly, 40, 60, start, 0.05)},
	}}}, start, th).Backends[0].Windows[0].Band
	if early == late {
		t.Fatalf("same numbers classified identically at 30%% and 95%% elapsed (%q) — the clock is not reaching the verdict", early)
	}
}

// "We cannot see this provider" and "this provider is fine" must not paint
// the same, so an unavailable backend publishes no verdict at all.
func TestUnavailableBackendServesNoBand(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	snap := Snapshot{Backends: []Backend{{
		Provider: "codex", Status: StatusUnavailable, Reason: "not signed in",
		Windows: []Window{bandWindow(WindowWeekly, 10, 90, now, 0.5)},
	}}}
	if b := WithBands(snap, now, DefaultThresholds()).Backends[0].Windows[0].Band; b != "" {
		t.Fatalf("unavailable backend served band %q, want empty", b)
	}
}

// Every window gets a verdict, not only the backend's primary one — the
// ticker paints them all.
func TestEveryWindowCarriesItsOwnVerdict(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	snap := Snapshot{Backends: []Backend{{
		Provider: "claude", Status: StatusAvailable,
		Windows: []Window{
			bandWindow(WindowWeekly, 80, 20, now, 0.5),
			bandWindow("session", 5, 95, now, 0.9),
		},
	}}}
	out := WithBands(snap, now, DefaultThresholds())
	for i, w := range out.Backends[0].Windows {
		if w.Band == "" {
			t.Fatalf("window %d (%s) served without a band", i, w.Name)
		}
	}
	if out.Backends[0].Windows[0].Band == out.Backends[0].Windows[1].Band {
		t.Fatalf("a spent week and an untouched session classified the same: %q",
			out.Backends[0].Windows[0].Band)
	}
}

// The field has to survive the wire, or none of the above reaches a browser.
func TestBandSurvivesJSON(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	snap := WithBands(Snapshot{Backends: []Backend{{
		Provider: "claude", Status: StatusAvailable,
		Windows: []Window{bandWindow(WindowWeekly, 80, 20, now, 0.5)},
	}}}, now, DefaultThresholds())

	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var back Snapshot
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatal(err)
	}
	if got := back.Backends[0].Windows[0].Band; got != string(BandHot) {
		t.Fatalf("band after a round trip = %q, want %q\n%s", got, BandHot, body)
	}
}
