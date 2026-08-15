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
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/thread"
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

// TestAgentMigrateRefusesTheOverseer: the overseer's chat attachment lives
// in the HTTP server, not the registry, so rotating it here would leave the
// owner talking to nothing (🎯T285 residual).
func TestAgentMigrateRefusesTheOverseer(t *testing.T) {
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
	s := &Server{registry: reg, migrator: refusingMigrator{t: t}}

	res, err := s.handleAgentMigrate(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "jevons", "provider": "claude",
		}},
	})
	if err != nil {
		t.Fatalf("handleAgentMigrate: %v", err)
	}
	if !res.IsError {
		t.Fatal("migrating the overseer was accepted")
	}
}

// refusingMigrator fails the test if migration is attempted at all.
type refusingMigrator struct{ t *testing.T }

func (m refusingMigrator) PrepareMigration(string, claudia.Provider, bool) (handover.Pending, error) {
	m.t.Fatal("PrepareMigration called for the overseer")
	return handover.Pending{}, nil
}
func (m refusingMigrator) CompleteThinBrief(p handover.Pending) (handover.Pending, error) {
	return p, nil
}
func (m refusingMigrator) SeedSuccessor(string) (handover.Pending, bool, error) {
	return handover.Pending{}, false, nil
}
func (m refusingMigrator) Launch(*thread.Thread) error { return nil }
