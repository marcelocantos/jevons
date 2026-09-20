// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T726 — the product half: a PINNED seat whose only blocker is an uncertain
// entry returns to service through a tool call, and the disposition is on the
// record. The fixture is the shape claudia-po was in all of 2026-09-20.

func reconcileRequest(args map[string]any) mcp.CallToolRequest {
	var req mcp.CallToolRequest
	req.Params.Name = "jevons_sendq_reconcile"
	req.Params.Arguments = args
	return req
}

// pinnedDaemon is a daemon holding one message for name whose delivery outcome
// nobody knows: the attempt is on disk, no drain owns it, and the seat reads
// PINNED to agent_list and fleet health.
func pinnedDaemon(t *testing.T, name, payload string) (*Server, *recordingSender, *upward, sendq.Entry) {
	t.Helper()
	s, sender, up := t418Daemon(t, t.TempDir())
	if _, _, err := s.sendQueue().Append(name, payload, time.Now().Add(-90*time.Minute)); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := s.sendQueue().ClaimFront(name)
	if err != nil || !ok {
		t.Fatalf("claim: %v ok=%v", err, ok)
	}
	if err := s.sendQueue().Resolve(name, attempt, sendq.Unverified, "broker protocol: agent_failed"); err != nil {
		t.Fatal(err)
	}
	pin, pinned := s.sendqPinFor(name)
	if !pinned || pin.EntryID != attempt.ID {
		t.Fatalf("fixture is not pinned: %+v pinned=%v", pin, pinned)
	}
	return s, sender, up, attempt
}

func TestT726PinnedSeatReturnsToServiceThroughTheTool(t *testing.T) {
	const name, payload = "claudia-po", "PO: bounce and re-read HEAD"
	s, _, up, held := pinnedDaemon(t, name, payload)

	var events []map[string]any
	s.SetEventLogger(func(component, decision string, fields map[string]any) {
		if decision == "sendq_reconcile" {
			events = append(events, fields)
		}
	})

	// 1. The read path exists, so nobody has to open the JSON to find the ids.
	show := resultText(t, reconcile(t, s, map[string]any{"name": name}))
	for _, want := range []string{held.ID, held.AttemptID, "uncertain", "jevons_sendq_reconcile"} {
		if !strings.Contains(show, want) {
			t.Fatalf("show omits %q, so the operator still needs the file:\n%s", want, show)
		}
	}

	// 2. The disposition, with what was actually observed.
	const evidence = "session f1a44c7f: queue-operation enqueue 15:25:32Z, remove 15:26:12Z, queued_command attachment 9388"
	res := reconcile(t, s, map[string]any{
		"name": name, "action": "confirmed", "entry_id": held.ID, "attempt_id": held.AttemptID,
		"actor": "jevons-po", "evidence": evidence,
	})
	if res.IsError {
		t.Fatalf("reconcile refused: %s", resultText(t, res))
	}

	// 3. The seat is back in service, and nothing was hand-edited to get here.
	if pin, pinned := s.sendqPinFor(name); pinned {
		t.Fatalf("seat still PINNED after reconciliation: %+v", pin)
	}
	entries, err := s.sendQueue().Snapshot(name)
	if err != nil || len(entries) != 0 {
		t.Fatalf("queue still held: %+v %v", entries, err)
	}

	// 4. Who reconciled it, on what evidence, and what the payload was.
	if len(events) != 1 {
		t.Fatalf("disposition not recorded: %+v", events)
	}
	ev := events[0]
	if ev["actor"] != "jevons-po" || ev["evidence"] != evidence || ev["outcome"] != string(sendq.ReconcileConfirmed) {
		t.Fatalf("record lost the attribution: %+v", ev)
	}
	removed, _ := ev["removed"].([]map[string]any)
	if len(removed) != 1 || removed[0]["text"] != payload || removed[0]["entry_id"] != held.ID {
		t.Fatalf("record does not carry the payload it removed: %+v", ev["removed"])
	}
	if !containsLine(up.all(), "reconciled by jevons-po") {
		t.Fatalf("overseer was not told the hold was resolved: %v", up.all())
	}
}

func TestT726ToolRefusesAnUnevidencedOrAnonymousDisposition(t *testing.T) {
	const name, payload = "claudia-po", "the message whose fate is unknown"
	for _, tc := range []struct{ label, actor, evidence string }{
		{"no evidence", "jevons-po", ""},
		{"no actor", "", "I read the transcript"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			s, _, _, held := pinnedDaemon(t, name, payload)
			res := reconcile(t, s, map[string]any{
				"name": name, "action": "drop", "entry_id": held.ID, "attempt_id": held.AttemptID,
				"actor": tc.actor, "evidence": tc.evidence,
			})
			if !res.IsError {
				t.Fatalf("unattributed drop accepted: %s", resultText(t, res))
			}
			entries, err := s.sendQueue().Snapshot(name)
			if err != nil || len(entries) != 1 || entries[0].Text != payload || entries[0].State != sendq.Uncertain {
				t.Fatalf("refused drop disturbed the payload: %+v %v", entries, err)
			}
		})
	}
}

// Clause 3: superseded entries for one addressee consolidate, so the receiver
// gets ONE authoritative message rather than three versions of an instruction
// and a reason to act on the stalest.
func TestT726SupersededMessagesReachTheReceiverAsOne(t *testing.T) {
	const name = "ge-t191-ndk-discovery"
	s, sender, _, held := pinnedDaemon(t, name, "the uncertain head")

	for _, text := range []string{"do A", "actually do B"} {
		if _, _, err := s.sendQueue().Append(name, text, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	// Consolidation refuses while the head's outcome is unknown: folding it
	// away would claim non-delivery on no evidence (🎯T416).
	res := reconcile(t, s, map[string]any{
		"name": name, "action": "consolidate", "actor": "jevons-po", "evidence": "later messages supersede",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), held.ID) {
		t.Fatalf("consolidation folded away an unresolved attempt: %s", resultText(t, res))
	}

	// Reconcile first, then fold — the order the ge-t191 queue needed.
	if res := reconcile(t, s, map[string]any{
		"name": name, "action": "confirmed", "entry_id": held.ID, "attempt_id": held.AttemptID,
		"actor": "jevons-po", "evidence": "receiver's JSONL has the payload as a user message",
	}); res.IsError {
		t.Fatalf("reconcile refused: %s", resultText(t, res))
	}
	if res := reconcile(t, s, map[string]any{
		"name": name, "action": "consolidate", "text": "authoritative: do B",
		"actor": "jevons-po", "evidence": "do A is superseded by actually do B",
	}); res.IsError {
		t.Fatalf("consolidate refused: %s", resultText(t, res))
	}

	s.drainAgentSendQueue(name)
	if got := sender.delivered(); len(got) != 1 || got[0] != "authoritative: do B" {
		t.Fatalf("receiver got %v; want exactly one authoritative message", got)
	}
	if depth := s.pendingAgentSends(name); depth != 0 {
		t.Fatalf("queue depth after delivery = %d", depth)
	}
}

// The notices that named no tool are the reason this target exists. Each one
// that tells an agent to "reconcile" now says what to call.
func TestT726HoldNoticesNameTheReconcilePath(t *testing.T) {
	pin := SendqPin{EntryID: "e81b80f752e0", AttemptID: "4da672f24905",
		State: sendq.Uncertain, Reason: "broker protocol: agent_failed"}

	for label, line := range map[string]string{
		"pinned seat": FormatSendqPinLine("claudia-po", pin),
		"reaped hold": FormatReapedUncertainHoldLine("claudia-po", pin, fleetintent.Record{}),
	} {
		if !strings.Contains(line, "jevons_sendq_reconcile") {
			t.Errorf("%s notice still names no tool for reconciling: %s", label, line)
		}
		if !strings.Contains(line, pin.EntryID) || !strings.Contains(line, pin.AttemptID) {
			t.Errorf("%s notice omits the ids the call needs: %s", label, line)
		}
	}

	refuse, reason, _ := ClassifyKillHeldSendq("jv-t718-gate-dirty-warn", nil, func(string) int { return 3 })
	if !refuse {
		t.Fatal("kill guard no longer refuses a held leaf")
	}
	if !strings.Contains(reason, "jevons_sendq_reconcile") {
		t.Errorf("a PO holding this refusal still has only the overseer's discard: %s", reason)
	}
}

// reconcile is one jevons_sendq_reconcile call through the registered
// handler, so the oracles exercise the tool an agent would actually reach for
// rather than the store method underneath it.
func reconcile(t *testing.T, s *Server, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := s.handleSendqReconcile(context.Background(), reconcileRequest(args))
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}
	if res == nil {
		t.Fatal("nil tool result")
	}
	return res
}
