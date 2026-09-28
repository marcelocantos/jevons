// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestT890PickFallbackProviderSkipsStalledAndIneligible is the pure fork:
// never re-offer the provider that just stalled, and skip anything the
// eligibility predicate refuses.
func TestT890PickFallbackProviderSkipsStalledAndIneligible(t *testing.T) {
	t.Parallel()
	// Everyone eligible: claude stalled → anthropic is first in the ladder.
	got := pickStallFallbackProvider("claude", func(string) bool { return true })
	if got != "anthropic" {
		t.Fatalf("got %q, want anthropic", got)
	}
	// Anthropic itself ineligible → falls to the next rung.
	got = pickStallFallbackProvider("claude", func(p string) bool { return p != "anthropic" })
	if got != "grok" {
		t.Fatalf("got %q, want grok", got)
	}
	// Nothing eligible → no fallback, never invents one.
	got = pickStallFallbackProvider("claude", func(string) bool { return false })
	if got != "" {
		t.Fatalf("got %q, want empty (no eligible fallback)", got)
	}
	// The stalled provider is never re-offered even if "eligible".
	got = pickStallFallbackProvider("anthropic", func(string) bool { return true })
	if got == "anthropic" {
		t.Fatal("must not re-offer the provider that just stalled")
	}
}

// t890StallSender is claudia's ready-timeout error for any Send while the
// seat is pinned to the stalling provider; once the registry row is
// switched onto the fallback it answers cleanly.
type t890StallSender struct {
	reg      *claudia.Registry
	name     string
	stallsOn claudia.Provider
}

func (f *t890StallSender) Alive() bool      { return true }
func (f *t890StallSender) Interrupt() error { return nil }
func (f *t890StallSender) Send(string) error {
	if d := f.reg.Def(f.name); d != nil && d.Provider == f.stallsOn {
		return errors.New("claude not ready (splash): last frame: " +
			"▐▛███▛█   Claude Code v2.1.278\n  ▝▝ ▝▝    ~/work\n")
	}
	return nil
}

// ACCEPTANCE 3 — a fake claude launcher that never clears splash, with an
// eligible fallback provider, lands the leaf on a registered, briefed
// worker instead of losing it.
func TestT890SplashStallFallsBackToEligibleProvider(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.launchAgentFn = func(context.Context, string) (*claudia.Agent, error) { return nil, nil }

	const name = "jv-t890-splash"
	sender := &t890StallSender{reg: reg, name: name, stallsOn: claudia.ProviderClaude}
	s.SetSenderResolver(func(n string) (agentSender, bool, error) {
		if n != name {
			return nil, false, errors.New("unknown seat")
		}
		return sender, false, nil
	})
	// A witness that reports TranscriptAbsent while pinned to the stalling
	// provider (no composer ever drew) and PayloadSeen once the row moves
	// onto the fallback — the same shape 🎯T729's own oracle uses.
	s.SetTurnWitness(func(string, string) turnWatch {
		return func() TurnEvidence {
			if d := reg.Def(name); d != nil && d.Provider == claudia.ProviderClaude {
				return TurnEvidence{Observed: true, Durable: true, TranscriptAbsent: true,
					Detail: "no transcript was ever created (the CLI never drew a composer)"}
			}
			return TurnEvidence{Observed: true, Durable: true, PayloadSeen: true,
				Detail: "transcript gained a user message carrying this payload"}
		}
	})
	grace := time.Duration(0)
	s.startStallGrace = &grace
	retries := 0 // 🎯T729's same-provider grace retry is exhausted immediately
	s.startStallRetries = &retries

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":      name,
		"workdir":   dir,
		"provider":  string(claudia.ProviderClaude),
		"parent":    "jevons-po",
		"purpose":   "work",
		"target_id": "T890",
		"prompt":    "Execute 🎯T890.",
	}
	res, err := s.handleAgentStart(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(res)
	if res == nil || res.IsError {
		t.Fatalf("start must succeed via fallback, got error: %s", text)
	}
	d := reg.Def(name)
	if d == nil {
		t.Fatal("seat must stay registered on the fallback provider, not be released")
	}
	if d.Provider == claudia.ProviderClaude {
		t.Fatalf("seat must have landed on a fallback provider, still on %q", d.Provider)
	}
	if d.Provider != "anthropic" {
		t.Fatalf("expected the anthropic fallback rung, got %q", d.Provider)
	}
	if !strings.Contains(text, "anthropic") {
		t.Fatalf("start result must name the fallback provider it landed on: %s", text)
	}
}
