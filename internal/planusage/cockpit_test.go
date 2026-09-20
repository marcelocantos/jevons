// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"strings"
	"testing"
	"time"
)

// 🎯T677: the inference this test used to pin is gone. A rate-limited
// meter says nothing about the allowance behind it, so a backend whose
// reading failed is reported as unreadable — never as spent.
func TestT677FailedReadingIsNotExhaustion(t *testing.T) {
	limited := Backend{
		Provider: "claude",
		Status:   StatusUnavailable,
		Reason:   `Claude usage HTTP 429: { "error": { "type": "rate_limit_error" } }`,
	}
	th := DefaultThresholds()

	// The park decision: a failed reading must not veto a seat. Session
	// classifies as unpublished, which is explicitly not a veto.
	if got := SessionStatusOf(limited, th); got != SessionUnpublished {
		t.Fatalf("SessionStatusOf(429) = %q, want %q — a failed reading parked a live worker on 2026-09-20", got, SessionUnpublished)
	}
	if got := WeeklyBandOf(limited, time.Now(), th); got != BandUnpublished {
		t.Fatalf("WeeklyBandOf(429) = %q, want %q", got, BandUnpublished)
	}
	if MintIneligible(limited, time.Now(), th) {
		t.Fatal("a provider we could not read was refused new work as though it were spent")
	}

	// A published zero is still exhaustion: only the inference went away.
	zero := 0.0
	spent := Backend{
		Provider: "claude",
		Status:   StatusAvailable,
		Windows: []Window{
			{Name: WindowSession, RemainingPercent: &zero},
			{Name: WindowWeekly, RemainingPercent: &zero},
		},
	}
	if got := SessionStatusOf(spent, th); got != SessionExhausted {
		t.Fatalf("SessionStatusOf(published 0%%) = %q, want %q", got, SessionExhausted)
	}

	// And the snapshot the cockpit reads is no longer rewritten: an
	// unreadable backend stays unavailable with its reason, instead of
	// being handed over as available with two zero windows.
	out := CockpitSnapshot(Snapshot{Backends: []Backend{limited}})
	if len(out.Backends) != 1 {
		t.Fatalf("CockpitSnapshot returned %d backends", len(out.Backends))
	}
	if out.Backends[0].Available() {
		t.Fatal("a failed reading was published to the cockpit as an available backend")
	}
	if len(out.Backends[0].Windows) != 0 {
		t.Fatalf("zero-remaining windows were synthesised from a failed reading: %+v", out.Backends[0].Windows)
	}
}

func TestFormatCockpitMatchesTicker(t *testing.T) {
	zero, eighty, used := 0.0, 83.0, 17.0
	reset := time.Date(2026, 8, 22, 8, 21, 6, 0, time.UTC)
	snap := Snapshot{
		At: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC),
		Backends: []Backend{
			{
				Provider: "claude",
				Status:   StatusUnavailable,
				Reason:   `Claude usage HTTP 429: { "error": { "type": "rate_limit_error", "message": "Rate limited." } }`,
			},
			{
				Provider: "codex",
				Status:   StatusAvailable,
				PlanType: "pro",
				Windows: []Window{{
					Name: WindowWeekly, RemainingPercent: &eighty, UsedPercent: &used, ResetsAt: &reset,
				}},
			},
			{
				Provider: "grok",
				Status:   StatusUnavailable,
				Reason:   "SuperGrok publishes no plan-remaining API",
			},
			{
				Provider:    "bedrock",
				Status:      StatusUnavailable,
				Reason:      "AWS Bedrock does not publish subscription remaining",
				FleetAgents: 0,
			},
		},
	}
	text := FormatCockpit(snap)
	// 🎯T677: a rate-limited Claude reads as unavailable with its reason,
	// not as EXHAUSTED with two invented zeroes. The overseer sees that
	// the meter failed, which is the fact, and can still route on the
	// providers that did answer.
	if !strings.Contains(text, "claude") || !strings.Contains(text, "unavailable") {
		t.Fatalf("429 Claude must read unavailable:\n%s", text)
	}
	if strings.Contains(text, "EXHAUSTED") {
		t.Fatalf("a failed reading was announced as an exhausted allowance:\n%s", text)
	}
	if strings.Contains(text, "session 0%") || strings.Contains(text, "weekly 0%") {
		t.Fatalf("zero windows were invented from a failed reading:\n%s", text)
	}
	if !strings.Contains(text, "codex") || !strings.Contains(text, "weekly 83%") {
		t.Fatalf("live Codex weekly must print remaining:\n%s", text)
	}
	if !strings.Contains(text, "grok") || !strings.Contains(text, "unavailable") {
		t.Fatalf("unpublished Grok stays unavailable:\n%s", text)
	}
	if strings.Contains(text, "gk ") && strings.Contains(text, "0%") && strings.Contains(strings.ToLower(text), "grok") && strings.Count(text, "0%") < 2 {
		// grok must not invent a 0% bar; the 0% lines belong to claude
	}
	if strings.Contains(text, "bedrock") {
		t.Fatalf("idle Bedrock stays off the bar:\n%s", text)
	}
	if strings.Contains(text, "grok") && strings.Contains(text, "weekly 0%") {
		// if grok line also has weekly 0% we invented a number
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "grok") && strings.Contains(line, "weekly 0%") {
				t.Fatalf("unpublished Grok must not invent 0%%:\n%s", text)
			}
		}
	}
	if !strings.Contains(text, "Route:") || !strings.Contains(text, "codex") {
		t.Fatalf("route hint should pick Codex weekly remaining:\n%s", text)
	}
	_ = zero
}

func TestShowOnBar(t *testing.T) {
	if ShowOnBar(Backend{Provider: "bedrock", Status: StatusUnavailable}) {
		t.Fatal("idle bedrock off")
	}
	if !ShowOnBar(Backend{Provider: "bedrock", Status: StatusUnavailable, FleetAgents: 2}) {
		t.Fatal("running bedrock on")
	}
	if !ShowOnBar(Backend{Provider: "grok", Status: StatusUnavailable}) {
		t.Fatal("unpublished grok on")
	}
}
