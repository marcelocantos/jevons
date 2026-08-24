// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/turnev"
)

func TestT543CompactSeatInvisibleToRecoverAndNudge(t *testing.T) {
	compact := claudia.AgentDef{
		Name: "jv-compact-deadbeef", SessionID: "compact-sess",
		Purpose: claudia.PurposeAside, Parent: "jevons-po",
		Provider: claudia.ProviderGrok,
	}

	act, reason := ClassifyFleetRecover(FleetRecoverObs{
		Name: compact.Name, Purpose: compact.Purpose,
		ProcessRunning: true, HasOpenMission: true, NeedsRecover: true,
		FailureClass: agenterr.ClassBackendUnavailable,
	})
	if act != FleetRecoverSkip || reason != "not_work_purpose" {
		t.Fatalf("fleet_recover compact: %s/%s; want skip/not_work_purpose", act, reason)
	}

	nudge, nudgeReason := ClassifyIdleNudge(IdleNudgeObs{
		Name: compact.Name, Purpose: compact.Purpose,
		ProcessRunning: true, Phase: "idle", IdleFor: 10 * time.Minute,
		HasOpenMission: true,
	})
	if nudge != IdleNudgeSkip || nudgeReason != "not_work_purpose" {
		t.Fatalf("idle_nudge compact: %s/%s; want skip/not_work_purpose", nudge, nudgeReason)
	}

	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	worker := claudia.AgentDef{
		Name: "jv-t543-worker", SessionID: "s-w",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Provider: claudia.ProviderGrok, TargetID: "T543", AutoStart: true,
	}
	for _, d := range []claudia.AgentDef{compact, worker} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	acts := planusage.PlanActions(planusage.Snapshot{At: now, Backends: []planusage.Backend{
		t39015Weekly("grok", 0, 100, now),
		t39015Weekly("codex", 80, 20, now),
	}}, []planusage.AgentRef{
		{Name: worker.Name, Provider: string(worker.Provider), Purpose: worker.Purpose, Parent: worker.Parent},
		{Name: compact.Name, Provider: string(compact.Provider), Purpose: compact.Purpose, Parent: compact.Parent},
	}, now, planusage.DefaultThresholds())
	if len(acts) != 1 || acts[0].Name != worker.Name {
		t.Fatalf("PlanActions must ignore compact aside, got %+v", acts)
	}

	var recoverPushed, nudgePushed []string
	SweepFleetRecover(FleetRecoverSweepArgs{
		Reg:            reg,
		Now:            now,
		Push:           func(target string, interrupt bool, event, text string) error { recoverPushed = append(recoverPushed, target); return nil },
		ProcessRunning: func(string) bool { return true },
		MissionOpen:    func(string) bool { return true },
	})
	for _, name := range recoverPushed {
		if name == compact.Name {
			t.Fatal("fleet_recover delivered to compact seat")
		}
	}
	reps := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg:            reg,
		Now:            now,
		Push:           func(target, event, text string) error { nudgePushed = append(nudgePushed, target); return nil },
		ProcessRunning: func(string) bool { return true },
		MissionOpen:    func(string) bool { return true },
		SessionPhase:   func(claudia.AgentDef) turnev.Phase { return turnev.PhaseIdle },
	})
	for _, r := range reps {
		if r.Name == compact.Name && r.Action != IdleNudgeSkip {
			t.Fatalf("idle sweep acted on compact: %+v", r)
		}
	}
	for _, name := range nudgePushed {
		if name == compact.Name {
			t.Fatal("idle_nudge delivered to compact seat")
		}
	}
}
