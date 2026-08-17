// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"strings"
	"testing"
	"time"
)

func TestIsExhaustedReason(t *testing.T) {
	if !IsExhaustedReason(`Claude usage HTTP 429: { "error": { "type": "rate_limit_error" } }`) {
		t.Fatal("429 rate_limit_error should be exhausted")
	}
	if !IsExhaustedReason("Rate limited. Please try again later.") {
		t.Fatal("rate limited should be exhausted")
	}
	if IsExhaustedReason("SuperGrok publishes no plan-remaining API") {
		t.Fatal("unpublished is not exhausted")
	}
	if IsExhaustedReason("") {
		t.Fatal("empty is not exhausted")
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
	if !strings.Contains(text, "claude") || !strings.Contains(text, "EXHAUSTED") {
		t.Fatalf("429 Claude must paint EXHAUSTED:\n%s", text)
	}
	if !strings.Contains(text, "session 0%") || !strings.Contains(text, "weekly 0%") {
		t.Fatalf("429 Claude must show 0%% session+weekly:\n%s", text)
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
