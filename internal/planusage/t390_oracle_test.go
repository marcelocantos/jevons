// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Acceptance oracle for 🎯T390, over fixture usage responses.
//
// The target's clause 5 names three properties of what the owner is shown, and
// clause 4 names one about what admission consumes. Each is here with the
// fixture that would have caught its absence, and each carries a CONTROL — an
// input mutated so the property should fail — because a check that cannot go
// red is not evidence that the code is right, only that the assertion is weak.
// Three of the four regressions this file rules out are silent: they render a
// plausible number rather than an error, which is exactly why the target was
// filed against a subsystem that had been reporting $0.00 and 100% headroom
// while the fleet ran.
//
//	clause 5a  a backend with windows shows BOTH percentages and the rollover
//	clause 5b  a backend publishing nothing says "unavailable", never 0 or blank
//	clause 5c  an aged reading is marked stale rather than served as current
//	clause 4   the same source binds capacity admission, and nil ≠ 0
//
// The assertions run over the JSON the daemon serves at GET /api/plan-usage —
// the bytes the cockpit reads — rather than over Go fields, so a field that
// stops being marshalled fails here too. handlePlanUsage's only transformation
// of the snapshot is json.Encoder, so these are the served bytes.
package planusage_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// fixedNow is the oracle's clock. Every fixture age is expressed against it,
// so staleness is decided by arithmetic and not by how long the test took.
var fixedNow = time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

func pct(v float64) *float64 { return &v }

// claudeReading is the shape claudia publishes for a Claude OAuth account:
// five_hour→session and seven_day→weekly, both with a rollover.
func claudeReading(fetchedAt time.Time) claudia.PlanUsage {
	sessionReset := fixedNow.Add(97 * time.Minute)
	weeklyReset := fixedNow.Add(52 * time.Hour)
	return claudia.PlanUsage{
		Provider:  claudia.ProviderClaude,
		Status:    claudia.PlanUsageAvailable,
		PlanType:  "max",
		FetchedAt: fetchedAt,
		Windows: []claudia.PlanWindow{
			{
				Name:             claudia.PlanWindowSession,
				UsedPercent:      pct(38),
				RemainingPercent: pct(62),
				ResetsAt:         &sessionReset,
				LimitWindow:      5 * time.Hour,
			},
			{
				Name:             claudia.PlanWindowWeekly,
				UsedPercent:      pct(71),
				RemainingPercent: pct(29),
				ResetsAt:         &weeklyReset,
				LimitWindow:      7 * 24 * time.Hour,
			},
		},
	}
}

// grokReading is the honest residual inherited from claudia 🎯T18: SuperGrok
// publishes no remaining, so the answer is a stated unavailable with a reason.
func grokReading(fetchedAt time.Time) claudia.PlanUsage {
	return claudia.PlanUsage{
		Provider:  claudia.ProviderGrok,
		Status:    claudia.PlanUsageUnavailable,
		Reason:    "SuperGrok publishes no plan-remaining API",
		FetchedAt: fetchedAt,
	}
}

// serve runs the fixtures through the real Reader and returns the bytes the
// daemon would serve, decoded generically. Decoding into map[string]any rather
// than back into planusage.Snapshot is deliberate: a field that silently stops
// being emitted would round-trip through the typed struct as a zero value and
// the assertion would pass over a blank the owner can see.
func serve(t *testing.T, readings []claudia.PlanUsage, load map[string]int, staleAfter time.Duration) map[string]any {
	t.Helper()
	r := planusage.NewReader(planusage.ReaderArgs{
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return readings, nil
		},
		Load:       func() map[string]int { return load },
		Now:        func() time.Time { return fixedNow },
		StaleAfter: staleAfter,
	})
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh fixtures: %v", err)
	}
	raw, err := json.Marshal(r.Snapshot())
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode served payload: %v", err)
	}
	return out
}

// backend picks one provider's object out of the served payload.
func backend(t *testing.T, payload map[string]any, provider string) map[string]any {
	t.Helper()
	list, _ := payload["backends"].([]any)
	for _, e := range list {
		b, _ := e.(map[string]any)
		if b["provider"] == provider {
			return b
		}
	}
	t.Fatalf("provider %q absent from served payload: %v", provider, payload)
	return nil
}

// window picks a named window out of a backend object, reporting whether it
// was published at all — the distinction clause 2 turns on.
func window(b map[string]any, name string) (map[string]any, bool) {
	list, _ := b["windows"].([]any)
	for _, e := range list {
		w, _ := e.(map[string]any)
		if w["name"] == name {
			return w, true
		}
	}
	return nil, false
}

// rendersBothWindows is clause 5a as a predicate, so the control can assert it
// goes false rather than duplicating the check with the sense flipped.
func rendersBothWindows(b map[string]any) bool {
	for _, name := range []string{planusage.WindowSession, planusage.WindowWeekly} {
		w, ok := window(b, name)
		if !ok {
			return false
		}
		if _, ok := w["remaining_percent"].(float64); !ok {
			return false
		}
		if s, ok := w["resets_at"].(string); !ok || s == "" {
			return false
		}
	}
	return true
}

// TestT390Clause5aWindowsRenderPercentAndRollover: a backend that publishes
// windows shows both percentages and when each rolls over.
func TestT390Clause5aWindowsRenderPercentAndRollover(t *testing.T) {
	payload := serve(t,
		[]claudia.PlanUsage{claudeReading(fixedNow.Add(-30 * time.Second))},
		map[string]int{"claude": 7},
		planusage.DefaultStaleAfter)

	b := backend(t, payload, "claude")
	if b["status"] != planusage.StatusAvailable {
		t.Fatalf("claude status = %v, want %q", b["status"], planusage.StatusAvailable)
	}
	if !rendersBothWindows(b) {
		t.Fatalf("session and weekly must each render remaining_percent and resets_at, got %v", b["windows"])
	}

	session, _ := window(b, planusage.WindowSession)
	if got := session["remaining_percent"]; got != float64(62) {
		t.Errorf("session remaining_percent = %v, want 62 (claudia's number, unaltered)", got)
	}
	if got := session["used_percent"]; got != float64(38) {
		t.Errorf("session used_percent = %v, want 38", got)
	}
	resets, _ := session["resets_at"].(string)
	if want := fixedNow.Add(97 * time.Minute).Format(time.RFC3339); resets != want {
		t.Errorf("session resets_at = %q, want %q", resets, want)
	}
	weekly, _ := window(b, planusage.WindowWeekly)
	if got := weekly["remaining_percent"]; got != float64(29) {
		t.Errorf("weekly remaining_percent = %v, want 29", got)
	}

	// CONTROL: the same backend with its windows withheld. If the predicate
	// above passed on this too it would be measuring nothing, and a producer
	// that stopped publishing windows would render as a healthy row.
	stripped := claudeReading(fixedNow.Add(-30 * time.Second))
	stripped.Windows = nil
	ctl := backend(t, serve(t,
		[]claudia.PlanUsage{stripped},
		map[string]int{"claude": 7},
		planusage.DefaultStaleAfter), "claude")
	if rendersBothWindows(ctl) {
		t.Error("control: a backend with no windows must not satisfy the render check")
	}
}

// TestT390Clause5bUnavailableIsStatedNotZeroed: Grok and a producer bug both
// have to say unavailable out loud. The failure this rules out is silent — a
// 0 renders as a full-width empty bar and reads as "you have nothing left",
// which is a different and false statement from "nobody published a number".
func TestT390Clause5bUnavailableIsStatedNotZeroed(t *testing.T) {
	payload := serve(t,
		[]claudia.PlanUsage{claudeReading(fixedNow), grokReading(fixedNow)},
		map[string]int{"claude": 7, "grok": 2},
		planusage.DefaultStaleAfter)

	g := backend(t, payload, "grok")
	if g["status"] != planusage.StatusUnavailable {
		t.Fatalf("grok status = %v, want %q", g["status"], planusage.StatusUnavailable)
	}
	if reason, _ := g["reason"].(string); reason == "" {
		t.Error("an unavailable backend must carry the provider's reason, not a bare verdict")
	}
	if _, ok := g["windows"]; ok {
		t.Errorf("unavailable backend must publish no windows, got %v", g["windows"])
	}
	// The specific lie: a zero where a number was never published.
	if w, ok := window(g, planusage.WindowSession); ok {
		t.Errorf("grok must not carry an invented session window: %v", w)
	}

	// CONTROL: a producer claiming available while publishing nothing. Taken
	// at face value that draws an empty row that looks like a reading; the
	// consumer downgrades it and says why.
	buggy := claudia.PlanUsage{
		Provider:  claudia.ProviderCodex,
		Status:    claudia.PlanUsageAvailable,
		FetchedAt: fixedNow,
	}
	c := backend(t, serve(t,
		[]claudia.PlanUsage{buggy},
		map[string]int{"codex": 1},
		planusage.DefaultStaleAfter), "codex")
	if c["status"] != planusage.StatusUnavailable {
		t.Errorf("available-with-no-windows must be downgraded to unavailable, got %v", c["status"])
	}
	if reason, _ := c["reason"].(string); reason == "" {
		t.Error("the downgrade must say why it happened")
	}
}

// TestT390Clause5cAgedReadingIsMarkedStale: a number that was true forty
// minutes ago is not a lie, but presenting it as now is.
func TestT390Clause5cAgedReadingIsMarkedStale(t *testing.T) {
	const age = 40 * time.Minute
	payload := serve(t,
		[]claudia.PlanUsage{claudeReading(fixedNow.Add(-age))},
		map[string]int{"claude": 7},
		planusage.DefaultStaleAfter)

	b := backend(t, payload, "claude")
	if stale, _ := b["stale"].(bool); !stale {
		t.Fatalf("a reading %s old (bound %s) must be marked stale, got %v", age, planusage.DefaultStaleAfter, b)
	}
	if secs, _ := b["age_seconds"].(float64); secs != age.Seconds() {
		t.Errorf("age_seconds = %v, want %v — the owner reads the age, not just the flag", secs, age.Seconds())
	}
	// Staleness must not blank the reading: the number is still shown, aged.
	if !rendersBothWindows(b) {
		t.Error("a stale reading is still rendered, with its age — not withheld")
	}

	// CONTROL: the identical reading inside the bound is NOT stale. Without
	// this, a consumer that marked everything stale would pass the assertion
	// above and the flag would carry no information.
	fresh := backend(t, serve(t,
		[]claudia.PlanUsage{claudeReading(fixedNow.Add(-30 * time.Second))},
		map[string]int{"claude": 7},
		planusage.DefaultStaleAfter), "claude")
	if stale, _ := fresh["stale"].(bool); stale {
		t.Error("control: a 30s-old reading must not be marked stale")
	}

	// SECOND CONTROL: the cache holds raw readings, not a finished snapshot,
	// so the same fetch read later is stale without re-fetching. This is the
	// freeze clause 5 rules out — "3 seconds old" baked in at fetch time.
	r := planusage.NewReader(planusage.ReaderArgs{
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return []claudia.PlanUsage{claudeReading(fixedNow)}, nil
		},
		Load:       func() map[string]int { return map[string]int{"claude": 7} },
		Now:        func() time.Time { return fixedNow.Add(time.Hour) },
		StaleAfter: planusage.DefaultStaleAfter,
	})
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	aged, ok := r.Snapshot().Backend("claude")
	if !ok {
		t.Fatal("claude absent after refresh")
	}
	if !aged.Stale {
		t.Error("a reading aged by the passage of time alone must become stale without re-fetching")
	}
}

// TestT390Clause4CapacityConsumesTheSameSource: admission reads plan remaining
// from this reader, and nil stays distinguishable from 0.
//
// This is the clause with a live incident behind it: every dollar dimension
// was unknown, unknown rendered as fine, and capacity reported 100% headroom
// on an account that was already out of allowance (🎯T406).
func TestT390Clause4CapacityConsumesTheSameSource(t *testing.T) {
	// Weekly at 8% is the tightest published window on the backend the fleet
	// is actually running on, so it is what must bind.
	tight := claudeReading(fixedNow)
	tight.Windows[1].RemainingPercent = pct(8)
	tight.Windows[1].UsedPercent = pct(92)

	r := planusage.NewReader(planusage.ReaderArgs{
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return []claudia.PlanUsage{tight, grokReading(fixedNow)}, nil
		},
		Load:       func() map[string]int { return map[string]int{"claude": 7, "grok": 2} },
		Now:        func() time.Time { return fixedNow },
		StaleAfter: planusage.DefaultStaleAfter,
	})
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	frac, source := r.CapacityRemaining()
	if frac == nil {
		t.Fatal("a published window must produce a remaining fraction")
	}
	if *frac != 0.08 {
		t.Errorf("CapacityRemaining = %v, want 0.08 (the tightest window, not the first)", *frac)
	}
	if source != "claude weekly" {
		t.Errorf("source = %q, want %q — the owner must be able to trace the number", source, "claude weekly")
	}

	// The product path: the daemon hands exactly this to the governor.
	a := capacity.Assess(capacity.Snapshot{PlanRemaining: frac, PlanSource: source}, capacity.DefaultPolicy())
	if a.PlanHeadroom != 0.08 {
		t.Errorf("PlanHeadroom = %v, want 0.08", a.PlanHeadroom)
	}
	if a.Headroom != 0.08 {
		t.Errorf("Headroom = %v, want 0.08 — plan remaining must bind when it is the tightest known dimension", a.Headroom)
	}
	if a.Pressure <= capacity.PressureNormal {
		t.Errorf("pressure = %v at 8%% plan remaining, want above normal", a.Pressure)
	}
	var named bool
	for _, reason := range a.Reasons {
		if len(reason) > 0 && contains(reason, "claude weekly") {
			named = true
		}
	}
	if !named {
		t.Errorf("a bound plan window must be named in the reasons, got %v", a.Reasons)
	}

	// CONTROL 1: a backend the fleet is NOT running on does not constrain it.
	// An exhausted Codex allowance is not a reason to park work on Claude.
	idle := planusage.NewReader(planusage.ReaderArgs{
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return []claudia.PlanUsage{tight}, nil
		},
		Load:       func() map[string]int { return map[string]int{"claude": 0} },
		Now:        func() time.Time { return fixedNow },
		StaleAfter: planusage.DefaultStaleAfter,
	})
	if err := idle.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if f, _ := idle.CapacityRemaining(); f != nil {
		t.Errorf("a backend with no fleet agents must not bind admission, got %v", *f)
	}

	// CONTROL 2: nothing published is nil, and nil must stay unknown rather
	// than becoming a confident 100%. This is the exact shape of the 🎯T406
	// incident, asserted at the layer that produced it.
	none := planusage.NewReader(planusage.ReaderArgs{
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return []claudia.PlanUsage{grokReading(fixedNow)}, nil
		},
		Load:       func() map[string]int { return map[string]int{"grok": 4} },
		Now:        func() time.Time { return fixedNow },
		StaleAfter: planusage.DefaultStaleAfter,
	})
	if err := none.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	f, src := none.CapacityRemaining()
	if f != nil {
		t.Errorf("no published window must yield nil, got %v", *f)
	}
	if src != "" {
		t.Errorf("no published window must yield no source, got %q", src)
	}
	unknown := capacity.Assess(capacity.Snapshot{PlanRemaining: f, PlanSource: src}, capacity.DefaultPolicy())
	if unknown.PlanHeadroom >= 0 {
		t.Errorf("PlanHeadroom = %v with nothing published, want unknown (negative) — never a confident number", unknown.PlanHeadroom)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
