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

func TestLoadMalformedFileFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(":\n\t bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("malformed config must be a hard error")
	}
}
