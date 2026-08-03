// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/config"
)

// TestGrokMCPServerSpec pins the user-scoped MCP registration the overseer
// resumes with (🎯T58): the server name comes from config, and the URL must
// be a concrete-host streamable-HTTP endpoint at /mcp. A regression here
// (wrong path, "localhost", missing port) would make tools fail to attach
// on session/load — the exact failure this target eliminates.
func TestGrokMCPServerSpec(t *testing.T) {
	cfg := config.Config{MCPServerName: "jevonsmcp", Port: 13705}
	name, url := grokMCPServerSpec(cfg, "127.0.0.1")
	if name != "jevonsmcp" {
		t.Errorf("name = %q, want jevonsmcp", name)
	}
	if want := "http://127.0.0.1:13705/mcp"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
	if strings.Contains(url, "localhost") {
		t.Errorf("url %q uses localhost — must be a concrete address (::1 vs 127.0.0.1 mismatch)", url)
	}
	if !strings.HasSuffix(url, "/mcp") {
		t.Errorf("url %q must end in /mcp (mcpserver mount path)", url)
	}

	// Non-default name and port propagate.
	cfg2 := config.Config{MCPServerName: "custom", Port: 9999}
	name2, url2 := grokMCPServerSpec(cfg2, "10.0.0.5")
	if name2 != "custom" || url2 != "http://10.0.0.5:9999/mcp" {
		t.Errorf("spec = (%q,%q), want (custom, http://10.0.0.5:9999/mcp)", name2, url2)
	}
}

// TestGrokCandidatePaths ensures the CLI fallback locations are the known
// install dirs, so grokBin resolves the Grok binary when it is not on PATH.
func TestGrokCandidatePaths(t *testing.T) {
	paths := grokCandidatePaths()
	if len(paths) == 0 {
		t.Fatal("no candidate paths (UserHomeDir failed?)")
	}
	for _, p := range paths {
		if !strings.HasSuffix(p, "grok") {
			t.Errorf("candidate %q does not end in grok", p)
		}
	}
	joined := strings.Join(paths, "\n")
	for _, want := range []string{"/.grok/bin/grok", "/opt/homebrew/bin/grok"} {
		if !strings.Contains(joined, want) {
			t.Errorf("candidate paths missing %q; got:\n%s", want, joined)
		}
	}
}

// TestGrokBin always resolves to a non-empty command ending in "grok" — a
// real path when present, else the bare name so exec yields a clear error.
func TestGrokBin(t *testing.T) {
	if got := grokBin(); !strings.HasSuffix(got, "grok") {
		t.Errorf("grokBin() = %q, want a path ending in grok", got)
	}
}

// 🎯T212: boot routes MCP ensure by overseer provider — Claude is not
// left on the Grok-only ensureGrokMCPServer path.
func TestOverseerMCPKindForProvider(t *testing.T) {
	cases := []struct {
		p    claudia.Provider
		want overseerMCPKind
	}{
		{claudia.ProviderGrok, overseerMCPGrok},
		{"", overseerMCPGrok}, // empty → default Grok fleet residual
		{claudia.ProviderClaude, overseerMCPClaude},
		{claudia.ProviderCodex, overseerMCPNone},
		{claudia.ProviderBedrock, overseerMCPNone},
		{claudia.Provider("unknown-future"), overseerMCPGrok},
	}
	for _, tc := range cases {
		if got := overseerMCPKindForProvider(tc.p); got != tc.want {
			t.Errorf("overseerMCPKindForProvider(%q) = %q, want %q", tc.p, got, tc.want)
		}
	}
}

// 🎯T212: Claude user-scoped mcp add argv must include -s user and http
// transport so resume keeps tools (mirrors Grok user-scoped config.toml).
func TestClaudeMCPAddArgs(t *testing.T) {
	args := claudeMCPAddArgs("jevonsmcp", "http://127.0.0.1:13705/mcp")
	if len(args) < 4 || args[0] != "mcp" || args[1] != "add" {
		t.Fatalf("want mcp add prefix, got %v", args)
	}
	if !containsPair(args, "--transport", "http") {
		t.Fatalf("want --transport http, got %v", args)
	}
	if !containsPair(args, "-s", "user") {
		t.Fatalf("want -s user (user scope for resume), got %v", args)
	}
	if args[len(args)-2] != "jevonsmcp" || args[len(args)-1] != "http://127.0.0.1:13705/mcp" {
		t.Fatalf("want name+url trailing, got %v", args)
	}
	for _, a := range args {
		if strings.Contains(a, "grok") {
			t.Fatalf("claude args must not mention grok: %v", args)
		}
	}
}

func TestGrokMCPAddArgs(t *testing.T) {
	args := grokMCPAddArgs("jevonsmcp", "http://127.0.0.1:13705/mcp")
	if !containsPair(args, "--transport", "http") {
		t.Fatalf("want --transport http, got %v", args)
	}
	if containsPair(args, "-s", "user") {
		t.Fatalf("grok mcp add has no -s user; got %v", args)
	}
	if args[len(args)-2] != "jevonsmcp" || args[len(args)-1] != "http://127.0.0.1:13705/mcp" {
		t.Fatalf("name+url: %v", args)
	}
}

// Shared name/URL spec for Grok and Claude ensure paths (🎯T212).
func TestOverseerMCPServerSpec(t *testing.T) {
	cfg := config.Config{MCPServerName: "jevonsmcp", Port: 13705}
	name, url := overseerMCPServerSpec(cfg, "127.0.0.1")
	if name != "jevonsmcp" || url != "http://127.0.0.1:13705/mcp" {
		t.Fatalf("spec = (%q,%q)", name, url)
	}
	if strings.Contains(url, "localhost") {
		t.Fatalf("must not use localhost: %q", url)
	}
	n2, u2 := grokMCPServerSpec(cfg, "127.0.0.1")
	if n2 != name || u2 != url {
		t.Fatalf("grokMCPServerSpec diverged from overseerMCPServerSpec")
	}
}

func TestClaudeBinEndsWithClaude(t *testing.T) {
	if got := claudeBin(); !strings.HasSuffix(got, "claude") {
		t.Errorf("claudeBin() = %q, want suffix claude", got)
	}
}

func containsPair(args []string, k, v string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == k && args[i+1] == v {
			return true
		}
	}
	return false
}

// 🎯T214 J4: diagnostics must not mislabel Claude/other as a Grok CLI failure
// solely because Grok connect fields/binaries are missing.
func TestDiagnoseOverseerUnavailableProviderAware(t *testing.T) {
	// Claude + missing binary: must not mention Grok install as the fix.
	got := diagnoseOverseerUnavailable(claudia.ProviderClaude, false, "")
	if !strings.Contains(got, "claude") && !strings.Contains(strings.ToLower(got), "claude") {
		t.Fatalf("claude missing: want Claude-facing text, got %q", got)
	}
	if strings.Contains(got, "install Grok") || strings.Contains(got, "grok login") {
		t.Fatalf("claude missing must not blame Grok CLI: %q", got)
	}
	if !strings.Contains(got, "not a Grok CLI issue") {
		t.Fatalf("claude missing should disclaim Grok: %q", got)
	}

	// Claude + binary present: still Claude-facing, not Grok login.
	got = diagnoseOverseerUnavailable(claudia.ProviderClaude, true, "")
	if strings.Contains(got, "grok login") || strings.Contains(got, "XAI_API_KEY") {
		t.Fatalf("claude present must not use Grok auth copy: %q", got)
	}

	// Codex missing.
	got = diagnoseOverseerUnavailable(claudia.ProviderCodex, false, "")
	if strings.Contains(got, "install Grok") {
		t.Fatalf("codex must not blame Grok: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "codex") {
		t.Fatalf("codex text: %q", got)
	}

	// Bedrock: credentials, not Grok.
	got = diagnoseOverseerUnavailable(claudia.ProviderBedrock, false, "")
	if strings.Contains(got, "install Grok") || strings.Contains(got, "grok login") {
		t.Fatalf("bedrock must not blame Grok: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "bedrock") {
		t.Fatalf("bedrock text: %q", got)
	}

	// Grok missing: historical first-run copy still mentions Grok install.
	got = diagnoseOverseerUnavailable(claudia.ProviderGrok, false, "")
	if !strings.Contains(got, "Grok CLI is not installed") {
		t.Fatalf("grok missing: %q", got)
	}

	// Grok with off-PATH candidate.
	got = diagnoseOverseerUnavailable(claudia.ProviderGrok, false, "/opt/homebrew/bin/grok")
	if !strings.Contains(got, "/opt/homebrew/bin/grok") {
		t.Fatalf("grok candidate path: %q", got)
	}
}
