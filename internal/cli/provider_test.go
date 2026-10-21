// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestResolveProviderPrecedence(t *testing.T) {
	t.Setenv(EnvProvider, "")

	if got := ResolveProvider("", ""); got != DefaultProvider {
		t.Fatalf("empty everything: got %q want %q", got, DefaultProvider)
	}
	if got := ResolveProvider("", "  "); got != DefaultProvider {
		t.Fatalf("whitespace cfg: got %q want %q", got, DefaultProvider)
	}
	if got := ResolveProvider("", "claude"); got != claudia.ProviderClaude {
		t.Fatalf("cfg: got %q want claude", got)
	}
	if got := ResolveProvider("codex", "claude"); got != claudia.ProviderCodex {
		t.Fatalf("override beats cfg: got %q", got)
	}
	// Pass-through: unknown / future ids (e.g. Bedrock under claudia T12).
	if got := ResolveProvider("bedrock", ""); got != claudia.Provider("bedrock") {
		t.Fatalf("pass-through: got %q want bedrock", got)
	}
}

func TestResolveProviderEnv(t *testing.T) {
	t.Setenv(EnvProvider, "claude")
	if got := ResolveProvider("", ""); got != claudia.ProviderClaude {
		t.Fatalf("env: got %q want claude", got)
	}
	// cfg beats env
	if got := ResolveProvider("", "codex"); got != claudia.ProviderCodex {
		t.Fatalf("cfg beats env: got %q", got)
	}
	// override beats env
	if got := ResolveProvider("grok", ""); got != claudia.ProviderGrok {
		t.Fatalf("override beats env: got %q", got)
	}
	t.Setenv(EnvProvider, "  claude  ")
	if got := ResolveProvider("", ""); got != claudia.ProviderClaude {
		t.Fatalf("env trim: got %q", got)
	}
}

func TestSelectAgentProviderNoClobber(t *testing.T) {
	defaultProv := claudia.ProviderGrok

	// Resume with stored Claude: empty override must not force Grok.
	got := SelectAgentProvider("", claudia.ProviderClaude, defaultProv)
	if got != claudia.ProviderClaude {
		t.Fatalf("stored claude clobbered to %q", got)
	}

	// Empty override + empty stored → default.
	got = SelectAgentProvider("", "", defaultProv)
	if got != defaultProv {
		t.Fatalf("empty stored: got %q want default %q", got, defaultProv)
	}

	// Empty override + empty stored + empty default → DefaultProvider.
	got = SelectAgentProvider("", "", "")
	if got != DefaultProvider {
		t.Fatalf("all empty: got %q want %q", got, DefaultProvider)
	}

	// Ad hoc override wins even when stored differs.
	got = SelectAgentProvider("claude", claudia.ProviderGrok, defaultProv)
	if got != claudia.ProviderClaude {
		t.Fatalf("override: got %q want claude", got)
	}

	// Whitespace override is ignored → keep stored.
	got = SelectAgentProvider("  ", claudia.ProviderClaude, defaultProv)
	if got != claudia.ProviderClaude {
		t.Fatalf("whitespace override: got %q want claude", got)
	}
}

func TestDefaultProviderIsGrok(t *testing.T) {
	if DefaultProvider != claudia.ProviderGrok {
		t.Fatalf("DefaultProvider = %q, want %q", DefaultProvider, claudia.ProviderGrok)
	}
	if Provider != DefaultProvider {
		t.Fatalf("Provider alias = %q, want DefaultProvider", Provider)
	}
}
