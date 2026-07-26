// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

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
