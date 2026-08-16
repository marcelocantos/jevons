// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T390.1: LimitWindow seconds reach the JSON the cockpit reads, so the
// time triangle is placed from published duration rather than guessed.
//
// The silent regression this rules out: Convert dropping claudia's
// LimitWindow. The bar would still paint remaining %, the triangle would
// sit on a hardcoded 5h/7d even when Codex published a different window,
// and nothing in the T390 oracles would go red — they never looked at
// the duration field.
package planusage_test

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func TestT390_1LimitWindowSecondsReachTheWire(t *testing.T) {
	payload := serve(t,
		[]claudia.PlanUsage{claudeReading(fixedNow.Add(-30 * time.Second))},
		map[string]int{"claude": 7},
		planusage.DefaultStaleAfter)

	b := backend(t, payload, "claude")
	session, ok := window(b, planusage.WindowSession)
	if !ok {
		t.Fatal("claude session window missing")
	}
	got, ok := session["limit_window_seconds"].(float64)
	if !ok {
		t.Fatalf("session limit_window_seconds missing or not a number: %v", session)
	}
	if got != 5*3600 {
		t.Errorf("session limit_window_seconds = %v, want 18000 (5h)", got)
	}
	weekly, ok := window(b, planusage.WindowWeekly)
	if !ok {
		t.Fatal("claude weekly window missing")
	}
	got, ok = weekly["limit_window_seconds"].(float64)
	if !ok {
		t.Fatalf("weekly limit_window_seconds missing or not a number: %v", weekly)
	}
	if got != 7*24*3600 {
		t.Errorf("weekly limit_window_seconds = %v, want 604800 (7d)", got)
	}

	// CONTROL: a published window with no duration must omit the field.
	// Inventing 5h here is how a Codex 3-hour primary would land a
	// triangle at the wrong place.
	bareReset := fixedNow.Add(time.Hour)
	bare := claudia.PlanUsage{
		Provider:  claudia.ProviderCodex,
		Status:    claudia.PlanUsageAvailable,
		FetchedAt: fixedNow,
		Windows: []claudia.PlanWindow{{
			Name:             claudia.PlanWindowWeekly,
			UsedPercent:      pct(10),
			RemainingPercent: pct(90),
			ResetsAt:         &bareReset,
		}},
	}
	ctl := backend(t, serve(t,
		[]claudia.PlanUsage{bare},
		map[string]int{"codex": 1},
		planusage.DefaultStaleAfter), "codex")
	w, ok := window(ctl, planusage.WindowWeekly)
	if !ok {
		t.Fatal("control weekly window missing")
	}
	if _, present := w["limit_window_seconds"]; present {
		t.Errorf("control: unpublished duration must be omitted, got %v", w["limit_window_seconds"])
	}
}

func TestT390_1GrokWeeklyWindowIsServedWhenPublished(t *testing.T) {
	reset := fixedNow.Add(3 * 24 * time.Hour)
	grok := claudia.PlanUsage{
		Provider:  claudia.ProviderGrok,
		Status:    claudia.PlanUsageAvailable,
		FetchedAt: fixedNow,
		Windows: []claudia.PlanWindow{{
			Name:             claudia.PlanWindowWeekly,
			UsedPercent:      pct(42),
			RemainingPercent: pct(58),
			ResetsAt:         &reset,
			LimitWindow:      7 * 24 * time.Hour,
		}},
	}
	payload := serve(t,
		[]claudia.PlanUsage{grok},
		map[string]int{"grok": 3},
		planusage.DefaultStaleAfter)
	g := backend(t, payload, "grok")
	if g["status"] != planusage.StatusAvailable {
		t.Fatalf("grok status = %v, want available", g["status"])
	}
	w, ok := window(g, planusage.WindowWeekly)
	if !ok {
		t.Fatal("grok weekly window missing")
	}
	if got := w["remaining_percent"]; got != float64(58) {
		t.Errorf("grok weekly remaining = %v, want 58", got)
	}
	if got, _ := w["limit_window_seconds"].(float64); got != 7*24*3600 {
		t.Errorf("grok weekly limit_window_seconds = %v, want 604800", w["limit_window_seconds"])
	}
}
