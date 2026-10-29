// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func t1257StopCall(name, actor string, overrideOwnerMessages bool) mcp.CallToolRequest {
	args := map[string]any{"name": name, "actor": actor, "reason": "nothing left for this worker"}
	if overrideOwnerMessages {
		args["override_owner_messages"] = true
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// 🎯T1257: a seat holding an undelivered owner message is never parked —
// jevons_agent_stop refuses, naming the message, and only proceeds with an
// explicit override that is logged and surfaced to the owner.
func TestT1257StopRefusedWithUndeliveredOwnerMessage(t *testing.T) {
	const name = "jv-t1257-busy"
	reg := t439Registry(t, name)
	s := &Server{registry: reg}

	// Queue an owner message exactly the way the real deliver path does for
	// a busy seat: origin=owner, enqueued because a turn is in flight.
	if _, err := s.enqueueAgentSendOriginWithRequestID(name, "owner: please re-check the acceptance criteria", "", true); err != nil {
		t.Fatalf("enqueue owner message: %v", err)
	}

	res, err := s.handleAgentStop(context.Background(), t1257StopCall(name, "jevons-po", false))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("stop went through with an undelivered owner message still queued: %s", toolText(res))
	}
	text := toolText(res)
	for _, want := range []string{"owner message", "undelivered", "please re-check the acceptance criteria", "override_owner_messages"} {
		if !strings.Contains(text, want) {
			t.Fatalf("refusal missing %q: %s", want, text)
		}
	}
	if reg.Def(name) == nil {
		t.Fatal("the refused stop deregistered the seat")
	}

	// An agent-origin message (an idle nudge, a worker report) does not
	// guard the stop — only owner-origin does.
	const other = "jv-t1257-agent-origin"
	reg2 := t439Registry(t, other)
	s2 := &Server{registry: reg2}
	if _, err := s2.enqueueAgentSendOriginWithRequestID(other, "agent: routine status ping", "", false); err != nil {
		t.Fatalf("enqueue agent message: %v", err)
	}
	res2, err := s2.handleAgentStop(context.Background(), t1257StopCall(other, "jevons-po", false))
	if err != nil {
		t.Fatal(err)
	}
	if res2.IsError {
		t.Fatalf("agent-origin queued message must not guard the stop: %s", toolText(res2))
	}

	// override_owner_messages=true proceeds, and is recorded on the lifecycle
	// log plus surfaced to the overseer (never silent).
	var notified []string
	s.notifyJevon = func(text string) { notified = append(notified, text) }
	res3, err := s.handleAgentStop(context.Background(), t1257StopCall(name, "jevons-po", true))
	if err != nil {
		t.Fatal(err)
	}
	if res3.IsError {
		t.Fatalf("override must proceed: %s", toolText(res3))
	}
	found := false
	for _, n := range notified {
		if strings.Contains(n, "🎯T1257") && strings.Contains(n, "jevons-po") {
			found = true
		}
	}
	if !found {
		t.Fatalf("override was not surfaced to the overseer: %q", notified)
	}
}

// 🎯T1257 — the auto-reap sweep (maybeReapDoneWorkAgent → ShouldAutoReapDoneWorkAgent
// → killSubtree) must not bypass the same undelivered-owner-message guard
// that jevons_agent_stop enforces. An auditor found this gap: the manual
// stop path was wired but the finished-work auto-reap path went straight to
// killSubtree, parking/reaping through a held owner message in exactly the
// shape the 2026-10-28 jv-t1244-widget-implement specimen named.
func TestT1257AutoReapKeepsSeatWithUndeliveredOwnerMessage(t *testing.T) {
	const name = "jv-t1257-autoreap"
	reg := t439Registry(t, name)
	s := &Server{registry: reg}

	if _, err := s.enqueueAgentSendOriginWithRequestID(name, "owner: hold on, re-check this before you finish", "", true); err != nil {
		t.Fatalf("enqueue owner message: %v", err)
	}

	finish := declaredFinishReport("T1257", "all landed and gated")
	s.maybeReapDoneWorkAgent(name, finish)

	if reg.Def(name) == nil {
		t.Fatal("auto-reap removed the seat despite an undelivered owner message (🎯T1257 gap)")
	}

	// Once the owner message is gone, the same finish-report reaps normally.
	if err := s.sendQueue().Clear(name); err != nil {
		t.Fatalf("clear sendq: %v", err)
	}
	s.maybeReapDoneWorkAgent(name, finish)
	if reg.Def(name) != nil {
		t.Fatal("auto-reap did not proceed once the owner message queue was clear")
	}
}
