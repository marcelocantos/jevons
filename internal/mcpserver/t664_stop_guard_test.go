// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T664 — the pure rule.
func TestT664StopGuardRule(t *testing.T) {
	now := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	if refuse, why := StopGuard(FlightInFlight, nil, now); !refuse || !strings.Contains(why, "in flight") {
		t.Fatalf("in-flight turn must refuse: %v %q", refuse, why)
	}
	fresh := &unconfirmedSend{At: now.Add(-time.Minute), Payload: "please continue from the checkpoint"}
	if refuse, why := StopGuard(FlightIdle, fresh, now); !refuse || !strings.Contains(why, "delivered_unconfirmed") {
		t.Fatalf("undecided delivery must refuse: %v %q", refuse, why)
	}
	stale := &unconfirmedSend{At: now.Add(-unconfirmedSendTTL - time.Minute), Payload: "old"}
	if refuse, _ := StopGuard(FlightIdle, stale, now); refuse {
		t.Fatal("a verdict older than the TTL is stale evidence, not a guard")
	}
	if refuse, _ := StopGuard(FlightUnknown, nil, now); refuse {
		t.Fatal("nothing known, nothing refused")
	}
}

func t664Call(name, actor string, force bool) mcp.CallToolRequest {
	args := map[string]any{"name": name, "actor": actor, "reason": "test"}
	if force {
		args["force"] = true
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// 🎯T664 acceptance: a stop on a seat whose last delivery came back
// delivered_unconfirmed is refused with the check named; the seat stays
// registered. force=true stops it. Once the turn boundary is observed the
// plain stop goes through.
func TestT664StopRefusedWhileDeliveryUndecided(t *testing.T) {
	const name = "jv-t664-undecided"
	reg := t439Registry(t, name)
	s := &Server{registry: reg}
	s.noteUnconfirmedSend(name, "resume from the checkpoint and commit")

	res, err := s.handleAgentStop(context.Background(), t664Call(name, "jevons", false))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("stop on an undecided delivery went through: %s", toolText(res))
	}
	text := toolText(res)
	for _, want := range []string{"refusing to stop", "delivered_unconfirmed", "jevons_transcript_read", "force=true"} {
		if !strings.Contains(text, want) {
			t.Fatalf("refusal missing %q: %s", want, text)
		}
	}
	if reg.Def(name) == nil {
		t.Fatal("the refused stop deregistered the seat")
	}

	res, err = s.handleAgentStop(context.Background(), t664Call(name, "jevons", true))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || !strings.Contains(toolText(res), "stopped and parked") {
		t.Fatalf("force=true must stop: %s", toolText(res))
	}

	// A second seat: the turn boundary arrives, the question is answered,
	// and an ordinary stop is an ordinary stop again.
	const other = "jv-t664-decided"
	if err := reg.Register(claudia.AgentDef{
		Name: other, WorkDir: t.TempDir(), SessionID: "s-w2",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s.noteUnconfirmedSend(other, "later")
	s.noteTurnEnded(other)
	res, err = s.handleAgentStop(context.Background(), t664Call(other, "jevons", false))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("plain stop after the boundary was refused: %s", toolText(res))
	}
}

// 🎯T664: the same guard on kill, keyed on a turn in flight.
func TestT664KillRefusedWhileTurnInFlight(t *testing.T) {
	const name = "jv-t664-inflight"
	reg := t439Registry(t, name)
	s := &Server{registry: reg}
	s.noteTurnInFlight(name)

	res, err := s.handleAgentKill(context.Background(), t664Call(name, "jevons", false))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolText(res), "refusing to kill") {
		t.Fatalf("kill during a turn in flight went through: %s", toolText(res))
	}
	if reg.Def(name) == nil {
		t.Fatal("the refused kill deregistered the seat")
	}

	res, err = s.handleAgentKill(context.Background(), t664Call(name, "jevons", true))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("force=true kill refused: %s", toolText(res))
	}
	if reg.Def(name) != nil {
		t.Fatal("force=true kill left the seat registered")
	}
}
