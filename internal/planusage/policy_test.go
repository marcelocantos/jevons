// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"
)

func TestWeeklyBandTable(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour) // 50% of 7d remaining
	lim := DefaultWeeklyWindowSeconds

	pct := func(v float64) *float64 { return &v }
	// Same shape as weekly, with the remaining-time fraction spelled out
	// so a specimen can sit anywhere in the window rather than only at the
	// halfway mark.
	weeklyAtElapsed := func(rem, used, remTimePct float64) Backend {
		resets := now.Add(time.Duration(remTimePct / 100 * float64(lim)) * time.Second)
		return Backend{
			Provider: "grok",
			Status:   StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(rem), UsedPercent: pct(used),
				ResetsAt: &resets, LimitWindowSeconds: &lim,
			}},
		}
	}
	weekly := func(rem, used float64) Backend {
		return Backend{
			Provider: "grok",
			Status:   StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(rem), UsedPercent: pct(used),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		}
	}

	// 🎯T596 moved this vertex deliberately. Burn 1.1 — 55% used at 50%
	// elapsed — is a 10% overspend with half the window still to run, and
	// the owner's instruction was explicit: "Dipping microscopically below
	// 1 shouldn't trigger orange." Under the pressure model it reads 0.17,
	// inside the 0.25 amber vertex, because a deviation that small this
	// early demands no correction worth a colour.
	if got := WeeklyBandOf(weekly(45, 55), now, th); got != BandOK {
		t.Fatalf("burn 1.1 at mid-window → ok, got %s", got)
	}
	// Ahead still exists, and is still reached — by a window that actually
	// demands a correction: 70% used at 50% elapsed reads 0.76.
	if got := WeeklyBandOf(weekly(30, 70), now, th); got != BandAhead {
		t.Fatalf("burn 1.4 → ahead, got %s", got)
	}
	// These ride the ahead band, so they move to the specimen that is now
	// in it. The claims are unchanged: ahead stops new seats being minted
	// but does not evict the ones already there.
	if MintIneligible(weekly(30, 70), now, th) != true {
		t.Fatal("ahead is mint-ineligible")
	}
	if MigrateOff(weekly(30, 70), now, th) {
		t.Fatal("ahead is not migrate-off")
	}
	// And the specimen that fell back to ok is now freely mintable — the
	// point of giving a 10% overspend no colour is that it costs nothing.
	if MintIneligible(weekly(45, 55), now, th) {
		t.Fatal("burn 1.1 is ok, and ok mints")
	}

	if got := WeeklyBandOf(weekly(20, 80), now, th); got != BandHot {
		t.Fatalf("burn 1.6 → hot, got %s", got)
	}
	if !MigrateOff(weekly(20, 80), now, th) {
		t.Fatal("hot migrates")
	}

	if got := WeeklyBandOf(weekly(0, 100), now, th); got != BandExhausted {
		t.Fatalf("0 remaining → exhausted, got %s", got)
	}

	if got := WeeklyBandOf(Backend{
		Provider: "claude", Status: StatusUnavailable,
		Reason: "Claude usage HTTP 429: rate_limit_error",
	}, now, th); got != BandExhausted {
		t.Fatalf("429 → exhausted, got %s", got)
	}

	// 🎯T596 widened the waste vertex deliberately, for the same reason it
	// widened the panic one: 42% used at 50% elapsed is 16% behind pace
	// and needs no correction worth a colour. It reads -0.32, inside the
	// -0.60 vertex.
	if got := WeeklyBandOf(weekly(58, 42), now, th); got != BandOK {
		t.Fatalf("16%% behind pace mid-window → ok, got %s", got)
	}
	// Waste still speaks when the runway makes it real: 40% used with 30%
	// of the week left reads -1.15, and that allowance will not be spent.
	if got := WeeklyBandOf(weeklyAtElapsed(60, 40, 30), now, th); got != BandUnder {
		t.Fatalf("40%% used with 30%% left → under, got %s", got)
	}
}

// TestT390_1_6_1EarlyWindowDamping: burn is damped (used+λ)/(elapsed+λ)
// so a barely-started week is not painted hot. Claude's week-start
// specimen (9% used, 5.6% elapsed, 91% remaining) had raw burn 1.6 —
// just past the 5% warmup — and drove migrate-off from an almost-full
// backend. A genuinely spent mid-week (80% used, 50% elapsed) stays hot.
func TestT390_1_6_1EarlyWindowDamping(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	weeklyAt := func(rem, used, remTimePct float64) Backend {
		resets := now.Add(time.Duration(remTimePct/100*float64(lim)) * time.Second)
		return Backend{
			Provider: "claude",
			Status:   StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(rem), UsedPercent: pct(used),
				ResetsAt: &resets, LimitWindowSeconds: &lim,
			}},
		}
	}

	weekStart := weeklyAt(91, 9, 94.4)
	if got := WeeklyBandOf(weekStart, now, th); got != BandOK && got != BandAhead {
		t.Fatalf("week start must be ok or ahead, got %s", got)
	}
	if MigrateOff(weekStart, now, th) {
		t.Fatal("a 91%%-remaining week start must not migrate off")
	}

	midWeek := weeklyAt(20, 80, 50)
	if got := WeeklyBandOf(midWeek, now, th); got != BandHot {
		t.Fatalf("80%% used at 50%% elapsed is still hot, got %s", got)
	}
	if !MigrateOff(midWeek, now, th) {
		t.Fatal("a genuinely spent mid-week still migrates off")
	}

	// Control: the easing must be the named threshold rather than a side
	// effect of some other vertex.
	//
	// 🎯T596 changed what this control can honestly assert. Under the old
	// burn ratio, λ=0 restored a raw statistic that painted the week start
	// red, so removing the knob produced a cliff. The pressure model has
	// no such cliff to restore: `required` is computed from the actual
	// runway, so a 91%-remaining window with 94% of its time left is on
	// track by construction, prior or no prior. Asserting BandHot here
	// would be asserting a behaviour the model never produces.
	//
	// What IS load-bearing, and what this now pins: the prior strictly
	// lowers the pressure of a barely-started window. Remove it and the
	// same specimen reads higher — that is the easing, measured directly
	// rather than inferred from a band that happens to move.
	// Not 0: zero means "unset, use the default" for every vertex in this
	// package, so it cannot express "no prior". A negligible k can.
	raw := th
	raw.ShrinkPriorK = 1e-9
	eased := Pressure(9, 100-94.4, th)
	unshrunk := Pressure(9, 100-94.4, raw)
	if !(unshrunk > eased) {
		t.Fatalf("control: the prior did not ease the week start (eased=%.3f unshrunk=%.3f)",
			eased, unshrunk)
	}
	// And it is the early window it eases, not everything: a spent
	// mid-week is hot with or without the prior, so the knob cannot be
	// used to hide a genuine overspend.
	rawMid := WeeklyBandOf(midWeek, now, raw)
	if rawMid != BandHot {
		t.Fatalf("control: spent mid-week must stay hot without the prior, got %s", rawMid)
	}
}

// TestT390_1_6_2NoElapsedCutoff: λ eases the early-window ratio; there
// is no return-ok below a warmup percent. Codex 2026-08-24 (26% used,
// ~4.9% elapsed) is hot. 80% used at 3% elapsed is still hot. Idle at
// 3% elapsed is under (waste), not forced green.
func TestT390_1_6_2NoElapsedCutoff(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 24, 8, 56, 0, 0, time.UTC)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	weeklyAt := func(rem, used, remTimePct float64) Backend {
		resets := now.Add(time.Duration(remTimePct/100*float64(lim)) * time.Second)
		return Backend{
			Provider: "codex",
			Status:   StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(rem), UsedPercent: pct(used),
				ResetsAt: &resets, LimitWindowSeconds: &lim,
			}},
		}
	}

	// 🎯T596 moved this specimen deliberately, and it is the one the owner
	// pointed at: "It makes no sense for it to be red at this point."
	// Codex 26% used at 4.9% elapsed is a real overspend, but with 95% of
	// the window left it is also trivially correctable — five quiet hours
	// undo it. Pressure reads 0.65: amber, not red. The same conduct
	// sustained climbs on its own as the runway shortens, which is the
	// whole point of scaling the prior by time left.
	codex := weeklyAt(74, 26, 95.1)
	if got := WeeklyBandOf(codex, now, th); got != BandAhead {
		t.Fatalf("Codex 26%% used at ~4.9%% elapsed is ahead, not red: got %s", got)
	}
	// 🎯T596: an ahead window is not evicted. Overspending early is a
	// thing to watch, not a thing to flee — the correction is available
	// for as long as the runway is long, and moving seats off a backend
	// that still has 74% of its allowance is the more expensive mistake.
	if MigrateOff(codex, now, th) {
		t.Fatal("an early overspend with three quarters left must not evict seats")
	}

	// Eviction rides the hot band, so the claim moves to the specimen
	// that genuinely earns it: 80% of the allowance gone in 3% of the
	// window is not correctable by easing off.
	spentEarly := weeklyAt(20, 80, 97)
	if got := WeeklyBandOf(spentEarly, now, th); got != BandHot {
		t.Fatalf("80%% used at 3%% elapsed must be hot, got %s", got)
	}
	if !MigrateOff(spentEarly, now, th) {
		t.Fatal("a genuinely spent-early week still migrates off")
	}

	// 🎯T596 answers the warmup question differently, and better. The old
	// model had to prove it was not hiding early windows behind a warmup
	// cutoff, so it called an idle week-start "waste" — but an untouched
	// window with 97% of its time left is not wasting anything yet; there
	// is nothing to correct, and saying otherwise is the same false alarm
	// in the opposite direction.
	idleEarly := weeklyAt(100, 0, 97)
	if got := WeeklyBandOf(idleEarly, now, th); got != BandOK {
		t.Fatalf("an untouched week start has nothing wrong with it, got %s", got)
	}
	// The claim that matters is that waste is not hidden, only timed: the
	// same idle backend speaks up once the runway is short enough for the
	// allowance to go unspent.
	idleLate := weeklyAt(100, 0, 60)
	if got := WeeklyBandOf(idleLate, now, th); got != BandUnder {
		t.Fatalf("idle with 60%% of the week left is under, got %s", got)
	}
}

func TestPickPlanDestWasteThenLoad(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	be := func(name string, rem, used float64) Backend {
		return Backend{
			Provider: name, Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(rem), UsedPercent: pct(used),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		}
	}
	// claude under (waste), grok ok with lower load — waste band wins.
	cands := []DestCand{
		{Provider: "claude", Backend: be("claude", 58, 42), Load: 5},
		{Provider: "grok", Backend: be("grok", 50, 50), Load: 1},
	}
	got, ok := PickPlanDest(cands, now, th)
	if !ok || got != "claude" {
		t.Fatalf("waste band beats load: dest=%q ok=%v", got, ok)
	}

	// two greens: least load (not highest remaining)
	cands = []DestCand{
		{Provider: "grok", Backend: be("grok", 50, 50), Load: 4},
		{Provider: "codex", Backend: be("codex", 55, 45), Load: 1},
	}
	got, ok = PickPlanDest(cands, now, th)
	if !ok || got != "codex" {
		t.Fatalf("least load among greens: dest=%q ok=%v", got, ok)
	}

	// all hot
	hot := be("grok", 20, 80)
	got, ok = PickPlanDest([]DestCand{{Provider: "grok", Backend: hot, Load: 2}}, now, th)
	if ok || got != "" {
		t.Fatalf("all-hot dest empty, got %q ok=%v", got, ok)
	}
}

func TestPlanActionsParkWhenNoDest(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	week := now.Add(3 * 24 * time.Hour)
	lim := DefaultWeeklyWindowSeconds
	zero, used := 0.0, 100.0
	snap := Snapshot{Backends: []Backend{{
		Provider: "grok", Status: StatusAvailable,
		Windows: []Window{{
			Name: WindowWeekly, RemainingPercent: &zero, UsedPercent: &used,
			ResetsAt: &week, LimitWindowSeconds: &lim,
		}},
	}}}
	acts := PlanActions(snap, []AgentRef{
		{Name: "jevons", Provider: "grok", Purpose: "overseer"},
		{Name: "worker", Provider: "grok", Purpose: "work"},
	}, now, th)
	if len(acts) != 1 || acts[0].Name != "worker" || acts[0].To != "" {
		t.Fatalf("overseer skipped; worker parks: %+v", acts)
	}
}

func TestPlanActionsMigrateToDest(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	snap := Snapshot{Backends: []Backend{
		{
			Provider: "grok", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(0), UsedPercent: pct(100),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		},
		{
			Provider: "claude", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(80), UsedPercent: pct(20),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		},
	}}
	acts := PlanActions(snap, []AgentRef{
		{Name: "w", Provider: "grok", Purpose: "work"},
	}, now, th)
	if len(acts) != 1 || acts[0].To != "claude" {
		t.Fatalf("migrate grok → claude: %+v", acts)
	}
}

func TestT517PlanActionsSkipsPOEvenWhenPurposeIsWork(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	snap := Snapshot{Backends: []Backend{
		{
			Provider: "claude", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(0), UsedPercent: pct(100),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		},
		{
			Provider: "codex", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(80), UsedPercent: pct(20),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		},
	}}
	acts := PlanActions(snap, []AgentRef{
		{Name: "jevons", Provider: "grok", Purpose: "overseer"},
		{Name: "jevons-po", Provider: "claude", Purpose: "work", Parent: "jevons"},
		{Name: "jv-t517-worker", Provider: "claude", Purpose: "work", Parent: "jevons-po"},
	}, now, th)
	if len(acts) != 1 || acts[0].Name != "jv-t517-worker" || acts[0].To != "codex" {
		t.Fatalf("only the worker migrates claude→codex, got %+v", acts)
	}
}

func TestT543PlanActionsSkipsAsideCompactSeat(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	snap := Snapshot{Backends: []Backend{
		{
			Provider: "grok", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(0), UsedPercent: pct(100),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		},
		{
			Provider: "codex", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(80), UsedPercent: pct(20),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		},
	}}
	acts := PlanActions(snap, []AgentRef{
		{Name: "jv-t543-worker", Provider: "grok", Purpose: "work", Parent: "jevons-po"},
		{Name: "jv-compact-deadbeef", Provider: "grok", Purpose: "aside", Parent: "jevons-po"},
	}, now, th)
	if len(acts) != 1 || acts[0].Name != "jv-t543-worker" {
		t.Fatalf("compact aside must be invisible to PlanActions, got %+v", acts)
	}
}
