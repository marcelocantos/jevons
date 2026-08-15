// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/cost"
)

// 🎯T215: Hermetic Jevons→claudia Session stitch for provider=claude.
//
// Exercises the same agent_start surface used by fleet workers
// (stitchAgentStart → registry def → Launch Config handoff) without
// spawning live Grok or Claude.
//
// Residual (documented): live Claude Session smoke remains optional /
// claudia 🎯T11.2. This oracle covers registry Provider + Materialized/
// session handoff fail-closed; it does not replace a real Claude TUI run.

// 🎯T324 hermetic: Launch provider=grok with empty pin → default model bound.
func TestStitchAgentStartBindsGrokDefaultModel(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))

	// Explicit provider=grok (T148). Omit-provider now follows config
	// (🎯T476); keep the pin so this oracle stays about T324 model bind.
	def, existed, _, err := s.stitchAgentStart(
		"cold-grok", t.TempDir(), "", string(claudia.ProviderGrok), "",
		"jevons-po", claudia.PurposeWork, "T324",
	)
	if err != nil {
		t.Fatalf("stitchAgentStart: %v", err)
	}
	if existed {
		t.Fatal("mint reported existed")
	}
	if def.Provider != claudia.ProviderGrok {
		t.Fatalf("provider=%s want grok", def.Provider)
	}
	if def.Model != cli.DefaultGrokModel {
		t.Fatalf("Model=%q want default %q (cold Grok must bind condensable model)", def.Model, cli.DefaultGrokModel)
	}
	// Resume with empty pin keeps the bound default (not re-emptied).
	again, existed, _, err := s.stitchAgentStart(
		"cold-grok", def.WorkDir, "", "", "",
		"jevons-po", claudia.PurposeWork, "T324",
	)
	if err != nil || !existed {
		t.Fatalf("resume: existed=%v err=%v", existed, err)
	}
	if again.Model != cli.DefaultGrokModel {
		t.Fatalf("resume Model=%q want still %q", again.Model, cli.DefaultGrokModel)
	}
	// Explicit pin wins over default.
	pinned, _, _, err := s.stitchAgentStart(
		"pinned-grok", t.TempDir(), "grok-4.5-build", string(claudia.ProviderGrok), "",
		"jevons-po", claudia.PurposeWork, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if pinned.Model != "grok-4.5-build" {
		t.Fatalf("explicit pin Model=%q", pinned.Model)
	}
}

func TestClaudeSessionStitchAgentStartSurface(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	// Daemon default stays Grok — stitch must still honour ad hoc claude.
	s.SetDefaultProvider(string(claudia.ProviderGrok))

	workdir := t.TempDir()
	const name = "jv-t215-claude-worker"

	def, existed, _, err := s.stitchAgentStart(
		name, workdir, "", string(claudia.ProviderClaude), "",
		"jevons-po", claudia.PurposeWork, "T215",
	)
	if err != nil {
		t.Fatalf("stitchAgentStart mint: %v", err)
	}
	if existed {
		t.Fatal("mint reported existed=true")
	}
	if def == nil {
		t.Fatal("nil def")
	}

	// Fail closed: Provider must be claude, never clobbered to Grok.
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("Provider = %q, want claude (clobbered to Grok?)", def.Provider)
	}
	if def.SessionID == "" {
		t.Fatal("SessionID empty after mint — session handoff broken")
	}
	// Pre-Launch: Materialized must be false (RequireResume off on first start).
	if def.Materialized {
		t.Fatal("Materialized=true on mint — first Launch would RequireResume wrongly")
	}
	if def.Parent != "jevons-po" || def.Purpose != claudia.PurposeWork {
		t.Fatalf("lineage parent=%q purpose=%q", def.Parent, def.Purpose)
	}
	if def.TargetID != "T215" {
		t.Fatalf("TargetID = %q want T215", def.TargetID)
	}

	// Launch Config handoff (what registry.Launch would pass to Start).
	prov, sid, requireResume := launchConfigFromDef(def)
	if prov != claudia.ProviderClaude {
		t.Fatalf("Launch provider handoff = %q, want claude", prov)
	}
	if sid != def.SessionID {
		t.Fatalf("Launch session handoff = %q, want %q", sid, def.SessionID)
	}
	if requireResume {
		t.Fatal("RequireResume true on first Launch — Materialized/session handoff wrong")
	}

	// Simulate successful claudia.Registry.Launch post-hook: Materialized=true,
	// SessionID stable. Fail closed if session cleared.
	mintedSID := def.SessionID
	def.Materialized = true
	if err := reg.Register(*def); err != nil {
		t.Fatal(err)
	}
	after := reg.Def(name)
	if after == nil || !after.Materialized {
		t.Fatal("post-Launch Materialized not persisted")
	}
	if after.SessionID != mintedSID {
		t.Fatalf("session handoff lost: %q → %q", mintedSID, after.SessionID)
	}
	if after.Provider != claudia.ProviderClaude {
		t.Fatalf("Provider clobbered after Materialized write: %q", after.Provider)
	}

	// Resume path: empty providerArg + daemon default Grok must keep claude.
	resumed, existed, _, err := s.stitchAgentStart(
		name, workdir, "", "", "", // no provider override
		"jevons-po", claudia.PurposeWork, "",
	)
	if err != nil {
		t.Fatalf("stitchAgentStart resume: %v", err)
	}
	if !existed {
		t.Fatal("resume reported existed=false")
	}
	if resumed.Provider != claudia.ProviderClaude {
		t.Fatalf("resume clobbered Provider to %q (want claude)", resumed.Provider)
	}
	if resumed.SessionID != mintedSID {
		t.Fatalf("resume reminted SessionID: %q → %q", mintedSID, resumed.SessionID)
	}
	if !resumed.Materialized {
		t.Fatal("resume cleared Materialized — RequireResume would be lost")
	}

	prov, sid, requireResume = launchConfigFromDef(resumed)
	if prov != claudia.ProviderClaude || sid != mintedSID || !requireResume {
		t.Fatalf("resume Launch handoff provider=%q sid=%q requireResume=%v",
			prov, sid, requireResume)
	}
}

// Fail-closed oracle: if SelectAgentProvider were skipped and Grok forced,
// the stitch would not meet T215. Pin the actual selection behaviour.
func TestClaudeSessionStitchFailsClosedOnGrokClobber(t *testing.T) {
	wrong := claudia.ProviderGrok // e.g. unconditional defaultProv
	if wrong == claudia.ProviderClaude {
		t.Fatal("fixture broken")
	}
	got := cli.SelectAgentProvider("", claudia.ProviderClaude, claudia.ProviderGrok)
	if got != claudia.ProviderClaude {
		t.Fatalf("SelectAgentProvider clobbered stored claude to %q", got)
	}
	got = cli.SelectAgentProvider(string(claudia.ProviderClaude), "", claudia.ProviderGrok)
	if got != claudia.ProviderClaude {
		t.Fatalf("override claude lost: %q", got)
	}
}

// 🎯T476: empty provider on mint follows the daemon default (config.yaml),
// not T325.2 portfolio routing. The compiled seed still prefers claude
// for code_implement; that is the losing knob, cited on the start note.
// Explicit provider= still wins (T148). Route() itself is covered in
// internal/cost (soft-cap spread is not the omit-provider mint path).
func TestStitchAgentStartPortfolioRoutesCodeImplement(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))

	def, _, note, err := s.stitchAgentStart(
		"jv-portfolio-worker", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.ProviderGrok {
		t.Fatalf("empty provider work mint → %q, want grok (config); note=%q", def.Provider, note)
	}
	if !strings.Contains(note, "provider_knob: config") {
		t.Fatalf("provider knob cite missing: %q", note)
	}
	if !strings.Contains(note, "compiled_seed would have picked claude") {
		t.Fatalf("losing compiled seed not cited: %q", note)
	}

	// task_type=mechanical is recorded for the loser citation but does
	// not override config.yaml (mechanical's compiled prefer is grok,
	// so they agree and no loser is named).
	mech, _, note, err := s.stitchAgentStart(
		"jv-mech-worker", t.TempDir(), "", "", "mechanical",
		"jevons-po", claudia.PurposeWork, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if mech.Provider != claudia.ProviderGrok {
		t.Fatalf("mechanical omit-provider → %q want grok; note=%q", mech.Provider, note)
	}
	if !strings.Contains(note, "provider_knob: config") {
		t.Fatalf("mechanical knob cite missing: %q", note)
	}

	// Soft-cap fill must not reassign an omit-provider mint off config.
	p := s.effectivePortfolio()
	capClaude := p.SoftCaps[cost.HarnessClaude]
	for i := 0; i < capClaude; i++ {
		if err := reg.Register(claudia.AgentDef{
			Name: fmt.Sprintf("fill-claude-%d", i), WorkDir: t.TempDir(),
			SessionID: fmt.Sprintf("s%d", i), Provider: claudia.ProviderClaude,
		}); err != nil {
			t.Fatal(err)
		}
	}
	spread, _, note, err := s.stitchAgentStart(
		"jv-spread-worker", t.TempDir(), "", "", "code_implement",
		"jevons-po", claudia.PurposeWork, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if spread.Provider != claudia.ProviderGrok {
		t.Fatalf("after claude at soft cap, omit mint → %q want config grok; note=%q", spread.Provider, note)
	}
}
