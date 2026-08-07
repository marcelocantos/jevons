// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T287: the RHS fleet prefix (company icon + condensed model) needs the
// agent's provider and the model it actually ran. Provider comes from the
// registry def; the model is learned from assistant frames and stays sticky
// so idle frames never blank the chrome.

func TestModelFromEvent(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"claude message.model", `{"type":"assistant","message":{"model":"claude-opus-4-8","content":[]}}`, "claude-opus-4-8"},
		{"top-level model", `{"type":"assistant","model":"grok-4.5"}`, "grok-4.5"},
		{"message wins over top level", `{"model":"stale","message":{"model":"claude-sonnet-4-5"}}`, "claude-sonnet-4-5"},
		{"no model named", `{"type":"assistant","message":{"content":[]}}`, ""},
		{"not json", `not json at all`, ""},
		{"empty raw", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := modelFromEvent(claudia.Event{Raw: []byte(tc.raw)})
			if got != tc.want {
				t.Fatalf("modelFromEvent(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestAgentProgressLearnsModelAndKeepsIt(t *testing.T) {
	hub := NewAgentProgressHub()

	if !hub.Observe("w", claudia.Event{
		Type: "assistant",
		Raw:  []byte(`{"type":"assistant","message":{"model":"claude-opus-4-8"}}`),
	}) {
		t.Fatal("first assistant frame should change the snapshot")
	}
	if got := hub.Get("w").Model; got != "claude-opus-4-8" {
		t.Fatalf("model=%q want claude-opus-4-8", got)
	}

	// A tool frame names no model — the learned one must survive.
	hub.Observe("w", claudia.Event{
		Type:         "progress",
		ProgressType: "tool_use",
		Raw:          []byte(`{"update":{"sessionUpdate":"tool_call","title":"Bash: go test"}}`),
	})
	if got := hub.Get("w").Model; got != "claude-opus-4-8" {
		t.Fatalf("after tool frame model=%q, want it sticky", got)
	}

	// Process liveness baseline must not blank it either.
	hub.SetStatus("w", "stopped")
	if got := hub.Get("w").Model; got != "claude-opus-4-8" {
		t.Fatalf("after SetStatus model=%q, want it sticky", got)
	}
}

func TestAgentProgressModelChangePushesRefresh(t *testing.T) {
	hub := NewAgentProgressHub()
	// Idle frame: same phase/summary twice is normally a no-change...
	hub.Observe("w", claudia.Event{Type: "assistant", Raw: []byte(`{"message":{"model":"grok-4.5"}}`)})
	if hub.Observe("w", claudia.Event{Type: "assistant", Raw: []byte(`{"message":{"model":"grok-4.5"}}`)}) {
		t.Fatal("identical frame should not report a change")
	}
	// ...but a new model must, so the RHS repaints the prefix after migrate.
	if !hub.Observe("w", claudia.Event{Type: "assistant", Raw: []byte(`{"message":{"model":"claude-opus-4-8"}}`)}) {
		t.Fatal("model change should report a change")
	}
	if got := hub.Get("w").Model; got != "claude-opus-4-8" {
		t.Fatalf("model=%q want claude-opus-4-8", got)
	}
}

func TestListFleetAgentsCarriesProviderAndModel(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []claudia.AgentDef{
		{Name: "grokker", WorkDir: dir, SessionID: "1", Provider: claudia.ProviderGrok},
		{Name: "pinned", WorkDir: dir, SessionID: "2", Provider: claudia.ProviderClaude, Model: "opus"},
		{Name: "observed", WorkDir: dir, SessionID: "3", Provider: claudia.ProviderClaude, Model: "opus"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	hub := NewAgentProgressHub()
	hub.Observe("observed", claudia.Event{
		Type: "assistant",
		Raw:  []byte(`{"message":{"model":"claude-opus-4-8"}}`),
	})

	byName := map[string]agentInfo{}
	for _, a := range listFleetAgentsNotifying(reg, nil, hub) {
		byName[a.Name] = a
	}

	if got := byName["grokker"].Provider; got != "grok" {
		t.Fatalf("grokker provider=%q want grok", got)
	}
	// No model reported anywhere → empty, so the UI paints the icon alone
	// rather than inventing a version.
	if got := byName["grokker"].Model; got != "" {
		t.Fatalf("grokker model=%q want empty", got)
	}
	if got := byName["pinned"].Model; got != "opus" {
		t.Fatalf("pinned model=%q want the def override", got)
	}
	// Observed beats the def override: it is what the agent actually ran.
	if got := byName["observed"].Model; got != "claude-opus-4-8" {
		t.Fatalf("observed model=%q want claude-opus-4-8", got)
	}
}
