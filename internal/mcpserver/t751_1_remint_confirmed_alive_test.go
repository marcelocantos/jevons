// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

// 🎯T751.1 — a remint issued to replace a dead seat is confirmed alive
// before it counts as a recovery.
//
// THE SPECIMEN. 2026-09-21 04:25-04:40: the overseer routed a dead
// jevons-po (🎯T751) through a remint. By 04:37 the row had a FRESH
// transcript (age 432s) and looked, on GET /api/agents, exactly like a
// healthy seat that had just come back. It was not: POST
// /api/agents/jevons-po/send returned failure_class: startup_stall — the
// remint had produced a born-stuck seat, and nothing reported that, so the
// recovery read as success and the product stayed unrefilled for a further
// fifteen minutes.
//
// This target's own acceptance is now carried by two mechanisms that
// landed after it was filed: 🎯T729 (a stalled opening brief is retried,
// then released under its own class rather than reported as an ordinary
// spawn success) and 🎯T890 (a stall that survives the retry gets one
// fallback-provider attempt before the leaf is reported lost). Both are
// exercised on the jevons_agent_start path a remint (kill then start) goes
// through, for ANY role including a product owner — there is no PO
// exception in that code, so the "applies with force to a PO seat"
// acceptance clause is structural, not a special case that could regress
// on its own. This file adds the regression these two guarantee but no
// existing test states directly for T751.1's own words: a remint whose
// CLI never presents a composer, on EVERY candidate provider (so there is
// nowhere left to fall back to), is answered to the orderer as a startup
// stall — never as a live replacement — and the row is not left registered
// as though the recovery succeeded.

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

// t751NeverComposerSender never presents a composer on ANY provider — the
// specimen shape where the CLI itself is wedged, not one backend that is
// down. No fallback provider can rescue this one.
type t751NeverComposerSender struct{}

func (t751NeverComposerSender) Alive() bool      { return true }
func (t751NeverComposerSender) Interrupt() error { return nil }
func (t751NeverComposerSender) Send(string) error {
	return errors.New("claude not ready (splash): last frame: " +
		"▐▛███▛█   Claude Code v2.1.278\n  ▝▝ ▝▝    ~/work\n")
}

// TestT751_1_BornStuckReplacementForPOIsReportedNotRecovered is the
// hermetic acceptance-4 scenario, run against jevons-po specifically
// (acceptance 3: applies with force to a product-owner seat). A remint
// whose CLI never draws a composer, on the stalled provider AND every
// eligible fallback rung, must:
//
//	(a) be answered to the orderer as the startup stall, not a success
//	    (mcp result IsError, text names startup_stall/never became ready —
//	    not "prompt_delivered=true" and not a silent ok); and
//	(b) leave the seat NOT registered as a live replacement — the row this
//	    call minted is released, not left on the panel with a fresh
//	    transcript timestamp masquerading as recovered (the exact
//	    specimen inversion: a born-stuck seat's age resets, so registry
//	    presence is not the confirmation).
func TestT751_1_BornStuckReplacementForPOIsReportedNotRecovered(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.launchAgentFn = func(context.Context, string) (*claudia.Agent, error) { return nil, nil }

	const name = "jevons-po"
	sender := t751NeverComposerSender{}
	s.SetSenderResolver(func(n string) (agentSender, bool, error) {
		if n != name {
			return nil, false, errors.New("unknown seat")
		}
		return sender, false, nil
	})
	// No transcript is ever created on any provider: the CLI never draws a
	// composer regardless of which rung it is minted on, so nothing ever
	// witnesses a turn (the same TranscriptAbsent shape T729/T890 use).
	s.SetTurnWitness(func(string, string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, TranscriptAbsent: true,
				Detail: "no transcript was ever created (the CLI never drew a composer)"}
		}
	})
	grace := time.Duration(0)
	s.startStallGrace = &grace
	retries := 0 // 🎯T729's same-provider grace retry exhausted immediately
	s.startStallRetries = &retries

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":      name,
		"workdir":   dir,
		"provider":  string(claudia.ProviderClaude),
		"role":      "product-owner",
		"purpose":   "work",
		"target_id": "T751",
		"prompt":    "Continue as jevons-po (remint after dead-seat detection, 🎯T751).",
	}
	res, callErr := s.handleAgentStart(t.Context(), req)
	if callErr != nil {
		t.Fatal(callErr)
	}
	text := toolText(res)

	// (a) the orderer sees the stall, never a success.
	if res == nil || !res.IsError {
		t.Fatalf("a remint whose CLI never presents a composer on any provider must be "+
			"reported as a failure, not a recovery; got IsError=false text=%s",
			toolTextOrNil(res))
	}
	if !strings.Contains(text, "startup_stall") && !strings.Contains(text, "ready") {
		t.Fatalf("result must name the startup-stall class so the orderer does not read a bare "+
			"generic failure as something else: %s", text)
	}
	if strings.Contains(text, "prompt_delivered") {
		t.Fatalf("must never claim prompt_delivered on a stalled remint: %s", text)
	}

	// (b) the row is not left registered as a live replacement. A fresh
	// transcript timestamp / registry presence is exactly the false
	// evidence the specimen describes — the release is the confirmation
	// that recovery did not happen.
	if d := reg.Def(name); d != nil {
		t.Fatalf("a born-stuck replacement must not stay registered looking like a live "+
			"recovery; row still present: %+v", d)
	}
}

func toolTextOrNil(res *mcp.CallToolResult) string {
	if res == nil {
		return "<nil result>"
	}
	return toolText(res)
}
