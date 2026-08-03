// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
)

// 🎯T148: agent_start provider selection — override, keep stored, empty→default.
func TestSelectAgentProviderViaServerDefault(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.SetDefaultProvider("grok")

	// Empty override + empty stored → daemon default.
	got := cli.SelectAgentProvider("", "", s.resolvedDefaultProvider())
	if got != claudia.ProviderGrok {
		t.Fatalf("default: got %q want grok", got)
	}

	// Configured default claude.
	s.SetDefaultProvider("claude")
	got = cli.SelectAgentProvider("", "", s.resolvedDefaultProvider())
	if got != claudia.ProviderClaude {
		t.Fatalf("cfg default: got %q want claude", got)
	}

	// Stored not clobbered when override empty.
	got = cli.SelectAgentProvider("", claudia.Provider("bedrock"), s.resolvedDefaultProvider())
	if got != claudia.Provider("bedrock") {
		t.Fatalf("stored bedrock clobbered to %q", got)
	}

	// Per-start override wins.
	got = cli.SelectAgentProvider("codex", claudia.ProviderGrok, s.resolvedDefaultProvider())
	if got != claudia.ProviderCodex {
		t.Fatalf("override: got %q want codex", got)
	}
}

// Hermetic: register agent with non-Grok provider, re-apply Select as agent_start
// would on resume with empty override — registry value must stay.
func TestAgentStartProviderNotClobberedOnResumeLogic(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "worker", WorkDir: t.TempDir(), SessionID: "s1",
		Provider: claudia.ProviderClaude, AutoStart: true, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))

	def := s.registry.Def("worker")
	if def == nil {
		t.Fatal("missing worker")
	}
	// Same assignment agent_start uses after EnsureAgent.
	def.Provider = cli.SelectAgentProvider("", def.Provider, s.resolvedDefaultProvider())
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("resume clobbered provider to %q", def.Provider)
	}
	if err := s.registry.Register(*def); err != nil {
		t.Fatal(err)
	}
	if got := s.registry.Def("worker").Provider; got != claudia.ProviderClaude {
		t.Fatalf("persisted provider = %q", got)
	}

	// Empty override on new agent uses default.
	newProv := cli.SelectAgentProvider("", "", s.resolvedDefaultProvider())
	if newProv != claudia.ProviderGrok {
		t.Fatalf("new agent default = %q want grok", newProv)
	}
}
