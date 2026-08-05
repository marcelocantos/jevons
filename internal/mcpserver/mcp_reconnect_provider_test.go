// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// TestMCPReconnectUnsupportedByProvider: `grok mcp disable/enable` is a
// Grok control plane. With another overseer provider selected the tool
// must say so — reporting "reconnected" after cycling a config the caller
// never reads would be a silent lie (🎯T282).
func TestMCPReconnectUnsupportedByProvider(t *testing.T) {
	for _, tc := range []struct {
		provider claudia.Provider
		allowed  bool
		mentions string
	}{
		{claudia.ProviderGrok, true, ""},
		{"", true, ""},
		{claudia.ProviderClaude, false, "claude"},
		{claudia.ProviderCodex, false, "codex"},
	} {
		msg, ok := mcpReconnectUnsupported(tc.provider)
		if ok != tc.allowed {
			t.Errorf("provider %q: allowed = %v, want %v", tc.provider, ok, tc.allowed)
			continue
		}
		if tc.allowed {
			continue
		}
		low := strings.ToLower(msg)
		if !strings.Contains(low, "grok control plane") {
			t.Errorf("provider %q: message does not name the Grok control plane: %s", tc.provider, msg)
		}
		if !strings.Contains(low, tc.mentions) {
			t.Errorf("provider %q: message does not name the selected provider: %s", tc.provider, msg)
		}
	}
}

// TestReconnectRefusesForClaudeOverseer wires the oracle to the tool path:
// a Claude-default server never shells out to the Grok CLI.
func TestReconnectRefusesForClaudeOverseer(t *testing.T) {
	fake := &fakeGrokCLI{listJSON: `[{"name":"github","enabled":true}]`}
	s := &Server{grokRun: fake.run}
	s.SetDefaultProvider(string(claudia.ProviderClaude))

	report, err := s.reconnectMCPServers(context.Background(), "github")
	if err == nil {
		t.Fatalf("reconnect under a Claude overseer succeeded: %q", report)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "grok control plane") {
		t.Fatalf("error does not explain the provider mismatch: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("Grok CLI was called for a Claude overseer: %v", fake.calls)
	}
}
