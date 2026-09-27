// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"
)

func TestPlanActionsUsesClaudiaForSidecarProviderIdentity(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	week := DefaultWeeklyWindowSeconds
	reading := func(provider string, used, remaining float64, reset time.Time) Backend {
		return Backend{Provider: provider, Status: StatusAvailable, FetchedAt: now,
			Windows: []Window{{Name: WindowWeekly, UsedPercent: &used,
				RemainingPercent: &remaining, ResetsAt: &reset, LimitWindowSeconds: &week}}}
	}
	snap := Snapshot{At: now, Backends: []Backend{
		reading("grok", 87, 13, now.Add(7*24*time.Hour/2)),
		reading("codex", 30, 70, now.Add(7*24*time.Hour-time.Hour)),
		reading("claude", 93, 7, now.Add(4*time.Hour)),
	}}
	seats := []AgentRef{{Name: "jevons", Provider: "xai-oauth", Purpose: "overseer"}}
	acts := PlanActions(snap, seats, now, DefaultThresholds())
	if len(acts) != 1 || acts[0].From != "grok" || acts[0].To != "claude" || acts[0].Author != "claudia" {
		t.Fatalf("hot Grok sidecar seat must migrate to eligible Claude: %+v", acts)
	}
	if acts[0].Model == "" {
		t.Fatalf("Claudia's destination model must reach the migration action: %+v", acts[0])
	}
	capSnap := snap
	capSnap.Backends = append([]Backend(nil), snap.Backends...)
	capSnap.Backends[1] = reading("codex", 60, 40, now.Add(3*24*time.Hour))
	withCap := PlanActions(capSnap, seats, now, DefaultThresholds(), DestCand{
		Provider: "claude", Backend: capSnap.Backends[2], Load: 2, Cap: 2,
	})
	if len(withCap) != 1 || withCap[0].To != "codex" {
		t.Fatalf("Claudia must choose an eligible provider outside the host's full fleet cap: %+v", withCap)
	}
	seats[0].Provider = "anthropic"
	if again := PlanActions(snap, seats, now, DefaultThresholds()); len(again) != 0 {
		t.Fatalf("second sweep on Claude must stay: %+v", again)
	}
}
