// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"strings"
	"testing"
	"time"
)

// TestTripClassesHermetic is the 🎯T334 oracle: every named trip class
// resolves to a stable clamp posture, and the overseer is never budget-killed.
func TestTripClassesHermetic(t *testing.T) {
	protected := map[string]bool{"jevons": true}

	cases := []struct {
		name       string
		alert      Alert
		wantClass  string
		wantAction string
		wantProt   bool
		wantInfo   bool
	}{
		{
			name:       "fleet kill → kill",
			alert:      Alert{Kind: AlertFleetRate, Level: LevelKill, Detail: "fleet burn"},
			wantClass:  TripFleetRate,
			wantAction: ActionKill,
		},
		{
			name:       "fleet warn → warn",
			alert:      Alert{Kind: AlertFleetRate, Level: LevelWarn},
			wantClass:  TripFleetRate,
			wantAction: ActionWarn,
		},
		{
			name:       "worker throttle → throttle",
			alert:      Alert{Kind: AlertWorkerRate, Level: LevelThrottle, Worker: "po"},
			wantClass:  TripWorkerRate,
			wantAction: ActionThrottle,
		},
		{
			name:       "worker kill non-protected → kill",
			alert:      Alert{Kind: AlertWorkerRate, Level: LevelKill, Worker: "po"},
			wantClass:  TripWorkerRate,
			wantAction: ActionKill,
		},
		{
			name:       "overseer kill → protected_pause never kill",
			alert:      Alert{Kind: AlertWorkerRate, Level: LevelKill, Worker: "jevons"},
			wantClass:  TripProtectedOverseer,
			wantAction: ActionProtectedPause,
			wantProt:   true,
		},
		{
			name:       "overseer throttle → protected_skip",
			alert:      Alert{Kind: AlertWorkerRate, Level: LevelThrottle, Worker: "jevons"},
			wantClass:  TripProtectedOverseer,
			wantAction: ActionProtectedSkip,
			wantProt:   true,
		},
		{
			name:       "global rate always informational",
			alert:      Alert{Kind: AlertGlobalRate, Level: LevelKill, Detail: "owner session"},
			wantClass:  TripGlobalRate,
			wantAction: ActionInformational,
			wantInfo:   true,
		},
		{
			name:       "session count → warn",
			alert:      Alert{Kind: AlertSessionCount, Level: LevelWarn},
			wantClass:  TripSessionCount,
			wantAction: ActionWarn,
		},
		{
			name:       "spawn storm → throttle posture",
			alert:      Alert{Kind: AlertSpawnStorm, Level: LevelThrottle, Detail: "40 sessions"},
			wantClass:  TripSpawnStorm,
			wantAction: ActionThrottle,
		},
		{
			name:       "orphan sessions → warn",
			alert:      Alert{Kind: AlertOrphanSessions, Level: LevelWarn},
			wantClass:  TripOrphanSessions,
			wantAction: ActionWarn,
		},
		{
			name:       "projected overspend → warn",
			alert:      Alert{Kind: AlertProjection, Level: LevelWarn},
			wantClass:  TripProjectedSpend,
			wantAction: ActionWarn,
		},
		{
			name:       "hard ceiling kill → spawn_halt",
			alert:      Alert{Kind: AlertHardCeiling, Level: LevelKill},
			wantClass:  TripHardCeiling,
			wantAction: ActionSpawnHalt,
		},
		{
			name:       "collector stale → warn",
			alert:      Alert{Kind: AlertCollectorStale, Level: LevelWarn},
			wantClass:  TripCollectorStale,
			wantAction: ActionWarn,
		},
	}

	var all []Trip
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveTrip(tc.alert, protected)
			if got.Class != tc.wantClass {
				t.Fatalf("class = %q, want %q", got.Class, tc.wantClass)
			}
			if got.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", got.Action, tc.wantAction)
			}
			if got.OverseerProtected != tc.wantProt {
				t.Fatalf("protected = %v, want %v", got.OverseerProtected, tc.wantProt)
			}
			if got.Informational != tc.wantInfo {
				t.Fatalf("informational = %v, want %v", got.Informational, tc.wantInfo)
			}
			if got.Action == ActionKill && got.OverseerProtected {
				t.Fatal("overseer-protected trip resolved to kill")
			}
			all = append(all, got)
		})
	}
	if !OverseerNeverKilled(all) {
		t.Fatal("OverseerNeverKilled failed over hermetic trip set")
	}
}

func TestClassifySnapshotIncludesSpawnStormAndProtectsOverseer(t *testing.T) {
	snap := &Snapshot{Alerts: []Alert{
		{Kind: AlertSpawnStorm, Level: LevelThrottle, Detail: "storm"},
		{Kind: AlertWorkerRate, Level: LevelKill, Worker: "jevons"},
		{Kind: AlertFleetRate, Level: LevelPause},
		{Kind: AlertGlobalRate, Level: LevelKill},
	}}
	trips := ClassifySnapshot(snap, []string{"jevons"})
	if len(trips) != 4 {
		t.Fatalf("trips = %d, want 4", len(trips))
	}
	if !OverseerNeverKilled(trips) {
		t.Fatal("overseer kill slipped through ClassifySnapshot")
	}
	var sawStorm, sawProt, sawInfo bool
	for _, tr := range trips {
		switch tr.Class {
		case TripSpawnStorm:
			sawStorm = true
		case TripProtectedOverseer:
			sawProt = true
			if tr.Action != ActionProtectedPause {
				t.Fatalf("overseer action = %s", tr.Action)
			}
		case TripGlobalRate:
			sawInfo = true
			if !tr.Informational {
				t.Fatal("global not informational")
			}
		}
	}
	if !sawStorm || !sawProt || !sawInfo {
		t.Fatalf("missing classes: storm=%v prot=%v info=%v", sawStorm, sawProt, sawInfo)
	}
}

func TestAuditFieldsAndOverseerAlertFormat(t *testing.T) {
	d := NewClampDecision(time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		LevelKill, "action", ActionKill, "po",
		"budget: KILLING worker po (kill breach confirmed)", "ok")
	if d.TripClass != TripWorkerRate {
		t.Fatalf("infer trip = %q, want worker-rate", d.TripClass)
	}
	fields := AuditFields(d)
	if fields["trip_class"] != TripWorkerRate || fields["action"] != ActionKill {
		t.Fatalf("audit fields = %+v", fields)
	}
	if fields["target"] != "po" || fields["outcome"] != "ok" {
		t.Fatalf("target/outcome missing: %+v", fields)
	}
	wire := FormatOverseerAlert(LevelKill, "budget: fleet burn over KILL threshold")
	if !strings.Contains(wire, "COST-SAFETY") || !strings.Contains(wire, "kill") {
		t.Fatalf("overseer alert wire = %q", wire)
	}
}

func TestInferTripClassFromMessage(t *testing.T) {
	cases := map[string]string{
		"dead-man's switch: no owner contact":            TripDeadMan,
		"budget: spawning halted (global kill breach)":   TripSpawnHalt,
		"budget: clamp cleared — spawning re-enabled":    TripSpawnResume,
		"budget: worker jevons over KILL — protected":    TripProtectedOverseer,
		"total machine burn is kill-level — informational": TripInformational,
		"budget: fleet burn over warn threshold":         TripFleetRate,
		"budget: worker po over warn threshold":          TripWorkerRate,
	}
	for msg, want := range cases {
		if got := InferTripClassFromMessage(msg); got != want {
			t.Errorf("Infer(%q) = %q, want %q", msg, got, want)
		}
	}
}

// TestEscalationLadderTripClasses ties enforcer acts to trip-class audit
// outcomes: warn → no kill; kill of protected → not killed.
func TestProtectedOverseerNeverBudgetKilledViaEnforcer(t *testing.T) {
	cfg := DefaultBudgetConfig() // protects jevons
	h := newHarness(cfg)
	// Sustained kill on overseer.
	h.e.Act(alertsOf(AlertWorkerRate, LevelKill, "jevons"))
	h.e.Act(alertsOf(AlertWorkerRate, LevelKill, "jevons"))
	if len(h.acts.killed) != 0 {
		t.Fatalf("overseer was budget-killed: %+v", h.acts.killed)
	}
	trips := ClassifySnapshot(&Snapshot{Alerts: []Alert{
		{Kind: AlertWorkerRate, Level: LevelKill, Worker: "jevons"},
	}}, cfg.ProtectedWorkers)
	if !OverseerNeverKilled(trips) || trips[0].Action != ActionProtectedPause {
		t.Fatalf("policy trips = %+v", trips)
	}
}
