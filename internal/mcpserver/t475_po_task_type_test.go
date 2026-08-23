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

// 🎯T475: omit-provider / omit-task_type product-owner mint does not
// inherit work→code_implement (compiled seed → Claude/Opus). PO name
// defaults to task_type=ceo; the start note cites that class. Workers
// still default work→code_implement.
func TestT475POMintDefaultsCEONotCodeImplement(t *testing.T) {
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
	ceoRoute := s.effectivePortfolio().Route(cost.TaskCEO, nil)
	if ceoRoute.Provider != cost.HarnessGrok {
		t.Fatalf("fixture: ceo seed → %q, want grok", ceoRoute.Provider)
	}

	po, existed, note, err := s.stitchAgentStart(
		"jevons-po", t.TempDir(), "", "", "",
		"jevons", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Fatal("PO mint reported existed")
	}
	if po.Provider != claudia.ProviderGrok {
		t.Fatalf("PO provider=%q want grok", po.Provider)
	}
	if strings.Contains(strings.ToLower(po.Model), "claude") ||
		strings.Contains(strings.ToLower(po.Model), "opus") {
		t.Fatalf("PO inherited Claude/Opus model: %q", po.Model)
	}
	if !strings.Contains(note, "task_type: ceo") {
		t.Fatalf("PO start note missing task_type=ceo: %q", note)
	}
	if strings.Contains(note, "via code_implement") {
		t.Fatalf("PO mint still cited code_implement path: %q", note)
	}

	worker, _, wnote, err := s.stitchAgentStart(
		"jv-t475-worker", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if worker.Provider != claudia.ProviderGrok {
		t.Fatalf("worker provider=%q want grok (config)", worker.Provider)
	}
	if !strings.Contains(wnote, "task_type: code_implement") {
		t.Fatalf("worker start note missing code_implement: %q", wnote)
	}
	if !strings.Contains(wnote, "compiled_seed would have picked claude") {
		t.Fatalf("worker should still name code_implement seed loser: %q", wnote)
	}
}

func TestT475ExplicitTaskTypeStillWinsOnPOName(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))

	_, _, note, err := s.stitchAgentStart(
		"probe-po", t.TempDir(), "", "", "code_implement",
		"jevons", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "task_type: code_implement") {
		t.Fatalf("explicit task_type lost: %q", note)
	}
}
