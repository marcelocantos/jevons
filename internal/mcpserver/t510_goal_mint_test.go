// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// 🎯T510 — work mint sets AgentDef.Goal; remint keeps it; asides stay empty.
func TestT510WorkMintSetsGoal(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)

	def, existed, _, err := s.stitchAgentStart(
		"jv-t510-work", t.TempDir(), "", string(claudia.ProviderCodex), "",
		"jevons-po", claudia.PurposeWork, "T510", "implement the Goal ingest",
	)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if existed || def == nil {
		t.Fatalf("existed=%v def=%v", existed, def)
	}
	if def.Goal != "implement the Goal ingest" {
		t.Fatalf("Goal = %q, want opening prompt", def.Goal)
	}
	if def.SandboxMode != "workspace-write" {
		t.Fatalf("SandboxMode = %q, want workspace-write", def.SandboxMode)
	}
	cfg := startConfigFromDef(def)
	if cfg.Goal != def.Goal {
		t.Fatalf("Launch Config.Goal = %q, want %q", cfg.Goal, def.Goal)
	}

	again, existed, _, err := s.stitchAgentStart(
		"jv-t510-work", def.WorkDir, "", string(claudia.ProviderGrok), "",
		"jevons-po", claudia.PurposeWork, "T510", "a later remint must not rewrite Goal",
	)
	if err != nil || !existed {
		t.Fatalf("remint: existed=%v err=%v", existed, err)
	}
	if again.Goal != "implement the Goal ingest" {
		t.Fatalf("remint rewrote Goal to %q", again.Goal)
	}
}

func TestT510WorkMintTargetIDGoalWhenPromptEmpty(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	def, _, _, err := s.stitchAgentStart(
		"jv-t510-tid", t.TempDir(), "", string(claudia.ProviderClaude), "",
		"jevons-po", claudia.PurposeWork, "T510", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Goal != "Achieve 🎯T510" {
		t.Fatalf("Goal = %q", def.Goal)
	}
}

func TestT510AsideAndOverseerLeaveGoalEmpty(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	aside, _, _, err := s.stitchAgentStart(
		"aside-t510", t.TempDir(), "", string(claudia.ProviderGrok), "",
		"jevons", claudia.PurposeAside, "", "do not loop",
	)
	if err != nil {
		t.Fatal(err)
	}
	if aside.Goal != "" {
		t.Fatalf("aside Goal = %q", aside.Goal)
	}
	ov, _, _, err := s.stitchAgentStart(
		"jevons", t.TempDir(), "", string(claudia.ProviderGrok), "",
		"", claudia.PurposeOverseer, "", "owner chat",
	)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Goal != "" {
		t.Fatalf("overseer Goal = %q", ov.Goal)
	}
}

// TestT510GoalJourneyEveryBackend is the hermetic half of J21: mint on
// each Session provider produces Config.Goal, and a remint keeps it.
func TestT510GoalJourneyEveryBackend(t *testing.T) {
	providers := []claudia.Provider{claudia.ProviderClaude, claudia.ProviderGrok, claudia.ProviderCodex}
	for _, from := range providers {
		for _, to := range providers {
			if from == to {
				continue
			}
			t.Run(string(from)+"→"+string(to), func(t *testing.T) {
				reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
				if err != nil {
					t.Fatal(err)
				}
				s := New(t.TempDir(), nil, nil)
				s.SetRegistry(reg)
				const objective = "portable across providers"
				def, _, _, err := s.stitchAgentStart(
					"jv-t510-"+string(from), t.TempDir(), "", string(from), "",
					"jevons-po", claudia.PurposeWork, "T510", objective,
				)
				if err != nil {
					t.Fatal(err)
				}
				if def.Goal != objective {
					t.Fatalf("mint Goal = %q", def.Goal)
				}
				if startConfigFromDef(def).Goal != objective {
					t.Fatalf("Launch Config.Goal = %q", startConfigFromDef(def).Goal)
				}
				again, existed, _, err := s.stitchAgentStart(
					def.Name, def.WorkDir, "", string(to), "",
					"jevons-po", claudia.PurposeWork, "T510", "must not rewrite",
				)
				if err != nil || !existed {
					t.Fatalf("remint: existed=%v err=%v", existed, err)
				}
				if again.Goal != objective {
					t.Fatalf("remint Goal = %q", again.Goal)
				}
				if startConfigFromDef(again).Goal != objective {
					t.Fatalf("remint Launch Config.Goal = %q", startConfigFromDef(again).Goal)
				}
			})
		}
	}
}

func TestT510WorkSessionGoalHelper(t *testing.T) {
	if g := fleet.WorkSessionGoal(claudia.PurposeWork, "T1", "brief", true); g != "brief" {
		t.Fatalf("prompt wins: %q", g)
	}
	if g := fleet.WorkSessionGoal(claudia.PurposeWork, "T1", "", true); g != "Achieve 🎯T1" {
		t.Fatalf("target: %q", g)
	}
	if g := fleet.WorkSessionGoal(claudia.PurposeAside, "T1", "x", true); g != "" {
		t.Fatalf("aside: %q", g)
	}
	if g := fleet.WorkSessionGoal(claudia.PurposeWork, "", "", false); g != "" {
		t.Fatalf("one-shot: %q", g)
	}
}
