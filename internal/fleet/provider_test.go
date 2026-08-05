// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/thread"
)

func TestProviderForLaunchNoClobber(t *testing.T) {
	// Stored claude must not be forced to grok on re-launch.
	got := providerForLaunch(claudia.ProviderClaude, "", claudia.ProviderGrok)
	if got != claudia.ProviderClaude {
		t.Fatalf("stored claude clobbered to %q", got)
	}
	// Empty stored + thread override.
	got = providerForLaunch("", claudia.ProviderClaude, claudia.ProviderGrok)
	if got != claudia.ProviderClaude {
		t.Fatalf("thread provider: got %q", got)
	}
	// Empty stored + empty thread → default.
	got = providerForLaunch("", "", claudia.ProviderGrok)
	if got != claudia.ProviderGrok {
		t.Fatalf("default: got %q", got)
	}
}

// Registry dual-write on mint uses thread provider / default, never hard-Grok.
func TestRegisterProviderFromThread(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	f.SetDefaultProvider(claudia.ProviderGrok)

	th := &thread.Thread{
		ID:       "aside-claude",
		WorkDir:  t.TempDir(),
		Provider: "claude",
		Parent:   "jevons",
		Purpose:  thread.PurposeAside,
	}
	// Register only the path Launch uses for mint (skip process Launch).
	prov := providerForLaunch("", claudia.Provider(th.Provider), f.defaultProvider)
	if err := reg.Register(claudia.AgentDef{
		Name: th.ID, WorkDir: th.WorkDir, SessionID: "s1",
		Provider: prov, Parent: th.Parent, Purpose: th.Purpose,
	}); err != nil {
		t.Fatal(err)
	}
	def := reg.Def(th.ID)
	if def == nil || def.Provider != claudia.ProviderClaude {
		t.Fatalf("provider=%v want claude", def)
	}

	// Re-resolve as Launch would on existing row: keep stored.
	again := providerForLaunch(def.Provider, "", f.defaultProvider)
	if again != claudia.ProviderClaude {
		t.Fatalf("resume clobbered to %q", again)
	}
	// Empty override keeps default when stored empty.
	if got := providerForLaunch("", "", cli.DefaultProvider); got != cli.DefaultProvider {
		t.Fatalf("empty → default: %q", got)
	}
}

// 🎯T215: fleet ensureRegistered (Launch's registry half) stitches
// provider=claude without live Grok/Claude process.
func TestClaudeSessionStitchEnsureRegistered(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	f.SetDefaultProvider(claudia.ProviderGrok)

	th := &thread.Thread{
		ID:       "jv-t215-fleet-claude",
		WorkDir:  t.TempDir(),
		Provider: string(claudia.ProviderClaude),
		Parent:   "jevons-po",
		Purpose:  thread.PurposeWork,
	}
	if err := f.ensureRegistered(th); err != nil {
		t.Fatalf("ensureRegistered mint: %v", err)
	}
	def := reg.Def(th.ID)
	if def == nil {
		t.Fatal("missing registry def after ensureRegistered")
	}
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("Provider = %q want claude", def.Provider)
	}
	if def.SessionID == "" {
		t.Fatal("SessionID empty — session handoff broken")
	}
	if def.Materialized {
		t.Fatal("Materialized true before Launch")
	}
	if th.SessionID == "" || th.SessionID != def.SessionID {
		t.Fatalf("thread SessionID handoff = %q def=%q", th.SessionID, def.SessionID)
	}

	// Resume with empty thread provider + Grok default must not clobber.
	th2 := &thread.Thread{
		ID:      th.ID,
		WorkDir: th.WorkDir,
		Parent:  "jevons-po",
		Purpose: thread.PurposeWork,
	}
	if err := f.ensureRegistered(th2); err != nil {
		t.Fatalf("ensureRegistered resume: %v", err)
	}
	again := reg.Def(th.ID)
	if again.Provider != claudia.ProviderClaude {
		t.Fatalf("resume clobbered Provider to %q", again.Provider)
	}
	if again.SessionID != def.SessionID {
		t.Fatalf("resume reminted session: %q → %q", def.SessionID, again.SessionID)
	}

	// Post-Launch Materialized must keep provider/session (claudia handoff).
	again.Materialized = true
	if err := reg.Register(*again); err != nil {
		t.Fatal(err)
	}
	if got := reg.Def(th.ID); got.Provider != claudia.ProviderClaude || !got.Materialized {
		t.Fatalf("after Materialized: provider=%q materialized=%v", got.Provider, got.Materialized)
	}
}


// TestPostReadySettleOnlyForClaude: the launch-path settle exists because
// Claude Session readiness is a pane pattern match that can fire while the
// TUI is still mounting, swallowing the first turn's submit (🎯T282). Grok
// reports ready over ACP and must not pay for it.
func TestPostReadySettleOnlyForClaude(t *testing.T) {
	if d := postReadySettle(claudia.ProviderClaude); d <= 0 {
		t.Errorf("claude settle = %s, want a positive grace period", d)
	}
	for _, p := range []claudia.Provider{claudia.ProviderGrok, claudia.ProviderCodex, ""} {
		if d := postReadySettle(p); d != 0 {
			t.Errorf("provider %q settle = %s, want 0", p, d)
		}
	}
}
