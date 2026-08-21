// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func t32521Mint(t *testing.T, s *Server, name, model, provider, taskType, purpose, prompt string) *claudia.AgentDef {
	t.Helper()
	def, existed, _, err := s.stitchAgentStart(
		name, t.TempDir(), model, provider, taskType,
		"jevons-po", purpose, "", prompt,
	)
	if err != nil {
		t.Fatalf("stitch %s: %v", name, err)
	}
	if existed {
		t.Fatalf("%s reported existed", name)
	}
	return def
}

func TestStitchFastCheapPinsGrokPeerOnMechanicalAndOps(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))

	mech := t32521Mint(t, s, "jv-mech", "", "", "mechanical", claudia.PurposeWork, "")
	if mech.Provider != claudia.ProviderGrok {
		t.Fatalf("mechanical provider=%q want grok", mech.Provider)
	}
	if mech.Model != cost.ModelGrokFast {
		t.Fatalf("mechanical model=%q want %s", mech.Model, cost.ModelGrokFast)
	}

	ops := t32521Mint(t, s, "jv-ops", "", "", "ops_classify", claudia.PurposeWork, "")
	if ops.Model != cost.ModelGrokFast {
		t.Fatalf("ops model=%q want %s", ops.Model, cost.ModelGrokFast)
	}

	nudge := t32521Mint(t, s, "jv-nudge", "", "", "nudge", claudia.PurposeWork, "")
	if nudge.Model != cost.ModelGrokFast {
		t.Fatalf("nudge alias model=%q", nudge.Model)
	}
}

func TestStitchFastCheapPinsSparkWhenDestIsCodex(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("grok", 20, 80, now),
			t39015Weekly("codex", 80, 20, now),
		}}
	})

	def := t32521Mint(t, s, "jv-spark", "", "", "mechanical", claudia.PurposeWork, "")
	if def.Provider != claudia.ProviderCodex {
		t.Fatalf("omit mint dest=%q want codex (grok ahead); model=%q", def.Provider, def.Model)
	}
	if def.Model != cost.ModelCodexSpark {
		t.Fatalf("codex dest model=%q want %s", def.Model, cost.ModelCodexSpark)
	}
}

func TestStitchDoesNotPinFastCheapOnImplementOrOverseer(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))

	work := t32521Mint(t, s, "jv-impl", "", "", "", claudia.PurposeWork, "")
	if work.Model == cost.ModelGrokFast || work.Model == cost.ModelCodexSpark {
		t.Fatalf("code_implement mint pinned fast-cheap: %q", work.Model)
	}
	if work.Model != cli.DefaultGrokModel {
		t.Fatalf("code_implement model=%q want provider default %q", work.Model, cli.DefaultGrokModel)
	}

	ceo := t32521Mint(t, s, "jv-ceo", "", "", "ceo", claudia.PurposeOverseer, "")
	if ceo.Model == cost.ModelGrokFast || ceo.Model == cost.ModelCodexSpark {
		t.Fatalf("overseer mint pinned fast-cheap: %q", ceo.Model)
	}
}

func TestStitchExplicitModelStillWinsOnFastCheap(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))

	def := t32521Mint(t, s, "jv-pin", "grok-4.5", "", "mechanical", claudia.PurposeWork, "")
	if def.Model != "grok-4.5" {
		t.Fatalf("explicit model lost: %q", def.Model)
	}
	exp := t32521Mint(t, s, "jv-codex-explicit", "", "codex", "mechanical", claudia.PurposeWork, "")
	if exp.Provider != claudia.ProviderCodex {
		t.Fatalf("explicit provider lost: %q", exp.Provider)
	}
	if exp.Model != cost.ModelCodexSpark {
		t.Fatalf("explicit codex mechanical model=%q want spark", exp.Model)
	}
}

func TestStitchDoesNotPinSparkOnRedCodexWeekly(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("grok", 80, 20, now),
			t39015Weekly("codex", 0, 100, now),
		}}
	})

	omit := t32521Mint(t, s, "jv-no-spark-omit", "", "", "mechanical", claudia.PurposeWork, "")
	if omit.Provider == claudia.ProviderCodex {
		t.Fatalf("omit mint landed on red Codex: %+v", omit)
	}
	if omit.Model == cost.ModelCodexSpark {
		t.Fatalf("omit mint pinned Spark: %q", omit.Model)
	}

	_, _, note, err := s.stitchAgentStart(
		"jv-no-spark-explicit", t.TempDir(), "", "codex", "mechanical",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	exp := s.registry.Def("jv-no-spark-explicit")
	if exp.Provider != claudia.ProviderCodex {
		t.Fatalf("explicit provider lost: %q", exp.Provider)
	}
	if exp.Model == cost.ModelCodexSpark {
		t.Fatalf("explicit red Codex pinned Spark: model=%q note=%q", exp.Model, note)
	}
	if !strings.Contains(note, "ineligible") || !strings.Contains(note, "T390.1.5") {
		t.Fatalf("red weekly skip must be visible: %q", note)
	}
}

func TestStitchEscalatesFastCheapWhenPromptExceedsWindow(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))

	// rune/4 estimate: 4 runes per token. Spark 128k + 1 token.
	huge := strings.Repeat("x", (cost.ContextCodexSpark+1)*4)
	if cost.EstimatePromptTokens(huge) <= cost.ContextCodexSpark {
		t.Fatalf("fixture tokens=%d", cost.EstimatePromptTokens(huge))
	}
	_, _, note, err := s.stitchAgentStart(
		"jv-escalate", t.TempDir(), "", "codex", "mechanical",
		"jevons-po", claudia.PurposeWork, "", huge,
	)
	if err != nil {
		t.Fatal(err)
	}
	def := s.registry.Def("jv-escalate")
	if def.Model == cost.ModelCodexSpark {
		t.Fatalf("over-window still pinned Spark: model=%q", def.Model)
	}
	if !strings.Contains(note, "escalated") || !strings.Contains(note, cost.ModelCodexSpark) {
		t.Fatalf("escalate must be visible: %q", note)
	}
}
