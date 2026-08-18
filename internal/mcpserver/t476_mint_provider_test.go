// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cost"
)

// 🎯T476: omit-provider work mint follows config.yaml (grok), not the
// compiled seed that still prefers Claude for code_implement. The start
// note cites which knob won and names the seed as the loser.
func TestStitchOmitProviderFollowsConfigNotCompiledSeed(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))

	if got := cost.WorkMintPortfolioProvider(s.effectivePortfolio()); got != cost.HarnessClaude {
		t.Fatalf("fixture: compiled seed work mint → %q, want claude", got)
	}

	def, existed, note, err := s.stitchAgentStart(
		"jv-t476-seed", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Fatal("mint reported existed")
	}
	if def.Provider != claudia.ProviderGrok {
		t.Fatalf("omit-provider mint → %q, want grok (config)", def.Provider)
	}
	if strings.Contains(strings.ToLower(def.Model), "claude") {
		t.Fatalf("compiled seed model pin leaked onto config mint: model=%q", def.Model)
	}
	if !strings.Contains(note, "provider_knob: config") {
		t.Fatalf("start note missing winning knob: %q", note)
	}
	if !strings.Contains(note, "compiled_seed would have picked claude") {
		t.Fatalf("start note missing losing compiled seed: %q", note)
	}
}

// 🎯T476: leftover ~/.jevons/llm-portfolio.json (2026-08-09 Claude/Opus
// pin) does not silently override config.yaml provider=grok, and does
// not apply its model pin. Start note names both knobs.
func TestStitchOmitProviderFollowsConfigNotLeftoverFile(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))

	leftover := &cost.Portfolio{
		DefaultProvider: cost.HarnessClaude,
		Routes: map[string]cost.TaskRoute{
			cost.TaskCodeImplement: {
				Prefer: []string{cost.HarnessClaude},
				Model:  "claude-opus-5",
			},
		},
	}
	s.SetLLMPortfolioSource(leftover, true)

	def, _, note, err := s.stitchAgentStart(
		"jv-t476-file", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.ProviderGrok {
		t.Fatalf("leftover file won: provider=%q note=%q", def.Provider, note)
	}
	if def.Model == "claude-opus-5" || strings.Contains(strings.ToLower(def.Model), "claude") {
		t.Fatalf("leftover file model pin applied: model=%q", def.Model)
	}
	if !strings.Contains(note, "provider_knob: config") {
		t.Fatalf("start note missing winning knob: %q", note)
	}
	if !strings.Contains(note, "portfolio_file would have picked claude") {
		t.Fatalf("start note missing losing file: %q", note)
	}

	// Explicit argument still wins and is cited.
	exp, _, note, err := s.stitchAgentStart(
		"jv-t476-explicit", t.TempDir(), "", "claude", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Provider != claudia.ProviderClaude {
		t.Fatalf("explicit provider lost: %q", exp.Provider)
	}
	if !strings.Contains(note, "provider_knob: explicit") {
		t.Fatalf("explicit start note: %q", note)
	}
}

// handleAgentStart formats the stitch cite into the tool result the
// caller sees. Keep the fragment stable so finish reports can quote it.
func TestStartResultCitesProviderKnob(t *testing.T) {
	pick := cost.PickMintProvider(cost.MintProviderArgs{
		ConfigProvider:    cost.HarnessGrok,
		Portfolio:         cost.RouteDecision{Provider: cost.HarnessClaude, TaskType: cost.TaskCodeImplement},
		PortfolioFromFile: true,
	})
	msg := formatAgentStartResult("probe", "/tmp/w", "jevons-po", "work", "", "grok", "sess", pick.Cite(), "")
	if !strings.Contains(msg, "provider: grok") {
		t.Fatalf("missing provider field: %q", msg)
	}
	if !strings.Contains(msg, "provider_knob: config") {
		t.Fatalf("missing winning knob: %q", msg)
	}
	if !strings.Contains(msg, "portfolio_file would have picked claude") {
		t.Fatalf("missing loser: %q", msg)
	}
}
