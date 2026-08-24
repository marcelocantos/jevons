// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"net"
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
	name, url := overseerMCPServerSpec(cfg, "127.0.0.1", 13705)
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
	name2, url2 := overseerMCPServerSpec(cfg2, "10.0.0.5", 9999)
	if name2 != "custom" || url2 != "http://10.0.0.5:9999/mcp" {
		t.Errorf("spec = (%q,%q), want (custom, http://10.0.0.5:9999/mcp)", name2, url2)
	}
}

// 🎯T379 acceptance 3: the advertised MCP URL is derived from the port the
// daemon actually serves, so a registration cannot drift from the daemon.
//
// The live failure this pins: cfg asks for one port, the bind lands on
// another (ephemeral cfg.Port == 0) or would have failed outright. Writing
// the registration from cfg.Port in that situation advertises an endpoint
// nothing is behind — a dead registration manufactured by the daemon itself.
func TestAdvertisedMCPURLFollowsTheServedPort(t *testing.T) {
	// A real bind on an ephemeral port: cfg says 0, the kernel says
	// something else, and the registration must name what the kernel said.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	served := servedPort(ln.Addr())
	if served == 0 {
		t.Fatalf("servedPort did not read the bound port from %v", ln.Addr())
	}

	cfg := config.Config{MCPServerName: "jevonsmcp", Port: 0}
	_, url := overseerMCPServerSpec(cfg, "127.0.0.1", served)

	want := fmt.Sprintf("http://127.0.0.1:%d/mcp", served)
	if url != want {
		t.Errorf("advertised %q, want %q (the port actually served)", url, want)
	}
	if strings.Contains(url, ":0/") {
		t.Errorf("advertised the configured placeholder port, not the served one: %q", url)
	}

	// And the drift case proper: a config port that is NOT the served port
	// must never appear in the registration.
	drifted := config.Config{MCPServerName: "jevonsmcp", Port: 13715}
	_, url2 := overseerMCPServerSpec(drifted, "127.0.0.1", served)
	if strings.Contains(url2, "13715") {
		t.Errorf("advertised the configured port 13715 over the served port %d: %q", served, url2)
	}
	if url2 != want {
		t.Errorf("advertised %q, want %q", url2, want)
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

// Shared name/URL spec (🎯T58 / 🎯T379).
func TestOverseerMCPServerSpec(t *testing.T) {
	cfg := config.Config{MCPServerName: "jevonsmcp", Port: 13705}
	name, url := overseerMCPServerSpec(cfg, "127.0.0.1", 13705)
	if name != "jevonsmcp" || url != "http://127.0.0.1:13705/mcp" {
		t.Fatalf("spec = (%q,%q)", name, url)
	}
	if strings.Contains(url, "localhost") {
		t.Fatalf("must not use localhost: %q", url)
	}
}

func TestClaudeBinEndsWithClaude(t *testing.T) {
	if got := claudeBin(); !strings.HasSuffix(got, "claude") {
		t.Errorf("claudeBin() = %q, want suffix claude", got)
	}
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

	// Cursor must not fall through to the Grok default (🎯T545 / 🎯T214).
	got = diagnoseOverseerUnavailable(claudia.ProviderCursor, true, "")
	if strings.Contains(got, "grok login") || strings.Contains(got, "XAI_API_KEY") || strings.Contains(got, "install Grok") {
		t.Fatalf("cursor present must not blame Grok: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "cursor") {
		t.Fatalf("cursor text: %q", got)
	}
	if !strings.Contains(got, "not a Grok CLI issue") {
		t.Fatalf("cursor should disclaim Grok: %q", got)
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

func TestOverseerDownReasonPrefersResumeDenied(t *testing.T) {
	loadErr := errors.New("acp session/load 0beb2254: connection closed (existing conversation; refusing to mint a replacement session)")
	got := overseerDownReason(claudia.ProviderCursor, loadErr)
	if !strings.Contains(got, "refusing to mint a replacement") {
		t.Fatalf("want fail-loud load copy, got %q", got)
	}
	if !strings.Contains(got, "0beb2254") {
		t.Fatalf("want the session id in the down reason, got %q", got)
	}
	if strings.Contains(got, "grok login") || strings.Contains(got, "XAI_API_KEY") {
		t.Fatalf("resume-denied must not blame Grok: %q", got)
	}
	fallback := overseerDownReason(claudia.ProviderCursor, nil)
	if strings.Contains(fallback, "grok login") || strings.Contains(fallback, "Grok CLI is not installed") {
		t.Fatalf("cursor fallback must not be Grok copy: %q", fallback)
	}
}
