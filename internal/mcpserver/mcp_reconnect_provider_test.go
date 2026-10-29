// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

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

// TestAgentSwitchRefusesTheOverseer: when the overseer itself makes the call
// (its seat header names it), a switch would refuse or interrupt the very
// turn asking; the owner switches it from the fleet tree. Another caller is
// not that turn and may switch it (an owner-session client, 2026-10-27).
func TestAgentSwitchRefusesTheOverseer(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: t.TempDir(), SessionID: "s-overseer",
		Provider: claudia.ProviderGrok, Purpose: claudia.PurposeOverseer,
	}); err != nil {
		t.Fatal(err)
	}
	// Assign the fields directly: SetRegistry also registers tools on the
	// MCP server, which this bare fixture does not have.
	sw := &fakeSwitcher{}
	s := &Server{registry: reg, switcher: sw}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "jevons", "provider": "claude", "owner_asked": true,
		}},
	}
	self := withMCPOrigin(context.Background(), mcpOrigin{Seat: "jevons"})
	res, err := s.handleAgentSwitchModel(self, req)
	if err != nil {
		t.Fatalf("handleAgentSwitchModel: %v", err)
	}
	if !res.IsError {
		t.Fatal("the overseer switching itself from inside its own call was accepted")
	}
	if switches, _ := sw.calls(); len(switches) != 0 {
		t.Fatalf("the overseer was switched: %v", switches)
	}

	other := withMCPOrigin(context.Background(), mcpOrigin{Client: "curl"})
	res, err = s.handleAgentSwitchModel(other, req)
	if err != nil {
		t.Fatalf("handleAgentSwitchModel: %v", err)
	}
	if res.IsError {
		t.Fatalf("an owner-session caller was told it is the overseer: %s", toolText(res))
	}
	if switches, _ := sw.calls(); len(switches) != 1 {
		t.Fatalf("owner-session switch of the overseer did not reach the switcher: %v", switches)
	}
}
