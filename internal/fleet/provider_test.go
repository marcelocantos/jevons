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
