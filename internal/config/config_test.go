// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T44 grep oracle: the default persona must carry no owner-specific
// identity — no personal names, no personal repo paths, no unresolved
// template holes.
func TestDefaultPersonaIsDepersonalized(t *testing.T) {
	p, err := Default().Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, banned := range []string{"Marcelo", "marcelocantos/sqlpipe", "yourworld", "{{", "<no value>"} {
		if strings.Contains(p, banned) {
			t.Errorf("default persona contains %q", banned)
		}
	}
	if !strings.Contains(p, "the owner") {
		t.Error("default persona should use the neutral owner reference")
	}
	if !strings.Contains(p, "# jevons") {
		t.Error("default persona should be titled with the default overseer name")
	}
}

func TestPersonaUsesConfiguredIdentity(t *testing.T) {
	c := Default()
	c.OwnerName = "Ada"
	c.OverseerName = "butler"
	c.PersonaNotes = "sqlpipe is the state-sync repo."
	p, err := c.Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, want := range []string{"# butler", "Ada's personal AI assistant", "## Owner Notes", "state-sync repo"} {
		if !strings.Contains(p, want) {
			t.Errorf("persona missing %q", want)
		}
	}
}

// 🎯T78: overseer persona steers child work onto fleet agents, not harness subagents.
func TestDefaultPersonaFleetSpawnDoctrine(t *testing.T) {
	p, err := Default().Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, want := range []string{
		"Fleet spawn doctrine",
		"jevons_agent_start",
		"jevons_thread_spawn",
		"spawn_subagent",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("default persona missing fleet-spawn doctrine marker %q", want)
		}
	}
	if !strings.Contains(p, "Forbidden as the default") {
		t.Error("default persona must forbid harness subagents as the default for implementation work")
	}
}

// 🎯T114: unified fleet doctrine — aside is a kind of agent; one deliver path.
func TestDefaultPersonaUnifiedFleetDoctrine(t *testing.T) {
	p, err := Default().Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, want := range []string{
		"Unified fleet",
		"aside is a kind of agent",
		"purpose",
		"jevons_event_push",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("default persona missing T114 marker %q", want)
		}
	}
}

// 🎯T111.4: PO/boss multi-slice fan-out doctrine is in the default persona.
func TestDefaultPersonaMultiSliceFanOut(t *testing.T) {
	p, err := Default().Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, want := range []string{
		"Multi-slice fan-out",
		"T111.4",
		"multiple independent slices",
		"zero children",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("default persona missing multi-slice fan-out marker %q", want)
		}
	}
}

// 🎯T87 / 🎯T103 / 🎯T130: impatience + RSI filing reflex in default persona.
func TestDefaultPersonaImpatienceAndRSI(t *testing.T) {
	p, err := Default().Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, want := range []string{
		"Impatience",
		"bias to act",
		"dead air",
		"Recursive self-improvement",
		"bullseye target",
		"filing reflex",
		"T130",
		"standing rule",
		"going forward",
		"from now on",
		"we should always",
		"same turn",
		"jevons_target_file",
		"bullseye_commit",
		"T92",
		"T129",
		"One-off flukes",
		// 🎯T92 / T92.2 ambient: schedule/stream + deeper chat/session surfaces
		"not only `/retro`",
		"periodic schedule",
		"jevons_rsi_cycle",
		"mint bullseye targets",
		"owner-chatlog friction",
		"session transcripts",
		"T92.2",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("persona missing impatience/RSI/T130 marker %q", want)
		}
	}
}

// 🎯T98: persona carries alter-ego identity pointer (draft doctrine linked).
func TestDefaultPersonaCEOAlterEgo(t *testing.T) {
	p, err := Default().Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, want := range []string{
		"alter ego",
		"T98",
		"ceo-alter-ego.md",
		"not a passive butler",
		"CEO seat",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("persona missing T98 alter-ego marker %q", want)
		}
	}
}

// 🎯T31 / T31.1: overseer refuses bare done; independent gate (rule 9).
func TestDefaultPersonaOracleFirstEnforcement(t *testing.T) {
	p, err := Default().Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, want := range []string{
		"Oracle-first as system property",
		"T31",
		"T31.1",
		"independent final judge",
		"rule 9",
		"Refuse bare done",
		"executable oracle evidence",
		"accepted-risk",
		"class-3",
		"ClassifyCompletionReport",
		"instructional doctrine",
		"T31.2",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("default persona missing T31 enforcement marker %q", want)
		}
	}
}

// 🎯T31.2: greenfield oracle-coverage map process in persona.
func TestDefaultPersonaGreenfieldOracleElicitation(t *testing.T) {
	p, err := Default().Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, want := range []string{
		"Greenfield oracle elicitation",
		"T31.2",
		"Oracle-coverage map",
		"pinned",
		"fuzzy",
		"when X, expect Y",
		"SPIRAL",
		"DECIDABLE-FROM-TASTE",
		"PROPORTIONALITY",
		"GOODHART",
		"CoverageMap",
		"ClassifyDesignClause",
		"greenfield-oracle-elicitation.md",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("default persona missing T31.2 marker %q", want)
		}
	}
}

// 🎯T104: local master ≠ origin/master; no PR-as-done default.
func TestDefaultPersonaLocalDeliveryDoctrine(t *testing.T) {
	p, err := Default().Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, want := range []string{
		"Delivery: local by default",
		"origin/master",
		"locally",
		"local `master`",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("default persona missing delivery marker %q", want)
		}
	}
	if !strings.Contains(p, "Countermand") && !strings.Contains(p, "do **not**") {
		t.Error("persona must countermand PR/origin re-expansion of local orders")
	}
}

// 🎯T125: Stratum-1 POs never implement; stay interruptible; instructional residual.
func TestDefaultPersonaPONeverImplements(t *testing.T) {
	p, err := Default().Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, want := range []string{
		"PO never implements",
		"T125",
		"Stratum 1",
		"interruptible",
		"Spawn-only for Build work",
		"instructional doctrine",
		"not a hard technical spawn-gate",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("default persona missing T125 marker %q", want)
		}
	}
}

// 🎯T129: overseer routes to jevons-po; never parents product workers under jevons.
func TestDefaultPersonaOverseerNeverParentsWorkers(t *testing.T) {
	p, err := Default().Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, want := range []string{
		"Overseer never parents product workers",
		"T129",
		"jevons-po",
		"parent=jevons",
		"Sole spawn parent",
		"rehydrate",
		"instructional",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("default persona missing T129 marker %q", want)
		}
	}
}

// 🎯T155: new unattended frontier leaves get a worker immediately (parent=jevons-po).
func TestDefaultPersonaUnattendedFrontierAutoSpawn(t *testing.T) {
	p, err := Default().Persona()
	if err != nil {
		t.Fatalf("Persona: %v", err)
	}
	for _, want := range []string{
		"Unattended frontier auto-spawn",
		"T155",
		"parent=jevons-po",
		"same operational cycle",
		"kick off",
		"non-design frontier",
		"immediately",
		"needs-owner",
		"design-discussion",
		"parked-for-design",
		"T112",
		"T67",
		"T29-class",
		"instructional",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("default persona missing T155 marker %q", want)
		}
	}
}

// 🎯T78/T104/T125/T129/T130/T155: agents-guide is the PO/worker product surface that inherits doctrine.
func TestAgentsGuideFleetAndDeliveryDoctrine(t *testing.T) {
	// agents-guide.md is repo-root product docs consumed by PO/workers.
	path := filepath.Join("..", "..", "agents-guide.md")
	b, err := os.ReadFile(path)
	if err != nil {
		// Fallback when tests run from module root via go test ./...
		path = "agents-guide.md"
		b, err = os.ReadFile(path)
		if err != nil {
			t.Fatalf("agents-guide.md: %v", err)
		}
	}
	g := string(b)
	for _, want := range []string{
		"Fleet spawn path",
		"jevons_agent_start",
		"spawn_subagent",
		"Delivery: local by default",
		"local `master`",
		"opened a PR",
		"Multi-slice fan-out",
		"T111.4",
		"Unified participant model",
		"aside is a kind of agent",
		"PO never implements",
		"T125",
		"spawn-only for Build work",
		"interruptible",
		"instructional doctrine",
		"Overseer never parents product workers",
		"T129",
		"parent=jevons",
		"sole spawn parent",
		"Filing reflex",
		"T130",
		"standing rule",
		"going forward",
		"from now on",
		"we should always",
		"jevons_target_file",
		"bullseye_commit",
		"T92",
		"Unattended frontier auto-spawn",
		"T155",
		"parent=jevons-po",
		"same operational cycle",
		"needs-owner",
		"design-discussion",
		"parked-for-design",
		"T112",
		"T67",
		"T29-class",
		"alter ego", // T98
		"T98",
		"ceo-alter-ego.md",
		// 🎯T31.1 oracle-first completion
		"Oracle-first completion",
		"T31",
		"T31.1",
		"Bare \"done\" is not accepted",
		"accepted-risk",
		"class-3",
		"Attestation ≠ execution",
		"ClassifyCompletionReport",
		// 🎯T31.2 greenfield elicitation
		"Greenfield oracle elicitation",
		"T31.2",
		"oracle-coverage map",
		"pinned",
		"fuzzy",
		"when X, expect Y",
		"SPIRAL",
		"DECIDABLE-FROM-TASTE",
		"CoverageMap",
		"greenfield-oracle-elicitation.md",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("agents-guide.md missing doctrine marker %q", want)
		}
	}
}

// 🎯T125/T129/T130/T155: product AGENTS.md (and CLAUDE thin import) carry fleet doctrine.
func TestAGENTSDoctrinePONeverImplements(t *testing.T) {
	path := filepath.Join("..", "..", "AGENTS.md")
	b, err := os.ReadFile(path)
	if err != nil {
		path = "AGENTS.md"
		b, err = os.ReadFile(path)
		if err != nil {
			t.Fatalf("AGENTS.md: %v", err)
		}
	}
	a := string(b)
	for _, want := range []string{
		"PO never implements",
		"T125",
		"spawn-only",
		"interruptible",
		"Overseer never parents product workers",
		"T129",
		"parent=jevons",
		"jevons-po",
		"Filing reflex",
		"T130",
		"standing rule",
		"going forward",
		"from now on",
		"we should always",
		"jevons_target_file",
		"bullseye_commit",
		"T92",
		"same turn",
		"Unattended frontier auto-spawn",
		"T155",
		"parent=jevons-po",
		"needs-owner",
		"design-discussion",
		"parked-for-design",
		"T112",
		"T67",
		"T29-class",
		// 🎯T31.1
		"Oracle-first completion",
		"T31",
		"T31.1",
		"oracle evidence",
		"accepted-risk",
		"class-3",
		"attestation ≠ execution",
		// 🎯T31.2
		"Greenfield oracle elicitation",
		"T31.2",
		"oracle-coverage map",
		"pinned",
		"fuzzy",
		"when X expect Y",
		"SPIRAL",
		"DECIDABLE-FROM-TASTE",
		"CoverageMap",
		"greenfield-oracle-elicitation.md",
	} {
		if !strings.Contains(a, want) {
			t.Errorf("AGENTS.md missing doctrine marker %q", want)
		}
	}
	// CLAUDE.md is the thin adapter; must import AGENTS so doctrine is in the load path.
	claudePath := filepath.Join(filepath.Dir(path), "CLAUDE.md")
	cb, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(cb), "AGENTS.md") {
		t.Error("CLAUDE.md must import AGENTS.md so T125/T129/T130/T155 doctrine is on the instruction surface")
	}
}

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OverseerName != "jevons" || cfg.Port != 13705 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.BindAddr != "127.0.0.1" {
		t.Fatalf("default bind must be loopback-only (T6), got %q", cfg.BindAddr)
	}
}

func TestLoadOverlaysAndBackfills(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("owner_name: Ada\nport: 999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OwnerName != "Ada" || cfg.Port != 999 {
		t.Fatalf("overlay not applied: %+v", cfg)
	}
	if cfg.OverseerName != "jevons" || cfg.StateDir == "" {
		t.Fatalf("unset fields not backfilled: %+v", cfg)
	}
}

// 🎯T148: provider field loads from YAML; empty is valid (env/grok fallback).
func TestLoadProviderField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("provider: claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "claude" {
		t.Fatalf("provider = %q want claude", cfg.Provider)
	}
	cfgEmpty, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfgEmpty.Provider != "" {
		t.Fatalf("default provider should be empty string (env/grok at resolve), got %q", cfgEmpty.Provider)
	}
}

func TestLoadMalformedFileFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(":\n\t bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("malformed config must be a hard error")
	}
}
