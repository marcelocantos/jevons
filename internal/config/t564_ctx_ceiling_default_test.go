// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/ctxcap"
)

// 🎯T564: with default config a seat far above the old 100k bar is neither
// hold nor unworkable — the per-seat ceiling is observation-only unless a
// deployment opts back in, and context_ceiling_disabled still wins.
func TestT564DefaultConfigDoesNotEnforceContextCeiling(t *testing.T) {
	cfg := Default()
	if cfg.ContextCeilingEnforced() {
		t.Fatal("default config enforces the per-seat context ceiling")
	}
	pol := ctxcap.Policy{Ceiling: cfg.ContextCeilingTokens, Disabled: !cfg.ContextCeilingEnforced()}
	for _, obs := range []ctxcap.Observation{
		{Agent: "jv-big", HasContext: true, Context: 150_000},
		{Agent: "jv-seed", HasContext: true, Context: 150_000, SeedOnly: true},
	} {
		d := pol.Evaluate(obs)
		if d.Verdict != ctxcap.VerdictOK || ctxcap.Unworkable(d) || ctxcap.ActionFor(d) == ctxcap.ActionUnworkable {
			t.Fatalf("%s: default policy gave verdict %q (%s); want ok, not hold/unworkable", obs.Agent, d.Verdict, d.Reason)
		}
	}

	on := Config{ContextCeilingEnabled: true}
	if !on.ContextCeilingEnforced() {
		t.Fatal("context_ceiling_enabled: true does not opt in")
	}
	both := Config{ContextCeilingEnabled: true, ContextCeilingDisabled: true}
	if both.ContextCeilingEnforced() {
		t.Fatal("context_ceiling_disabled must win over context_ceiling_enabled")
	}
}
