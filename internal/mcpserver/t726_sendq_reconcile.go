// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T726 — THE LEGAL MOVE A PINNED SEAT DID NOT HAVE.
//
// 2026-09-20: claudia-po sat PINNED on sendq message e81b80f752e0 with
// unresolved attempt 4da672f24905 from the first fleet listing of the session
// to near its end. The notice told it to "reconcile delivery before another
// send" and named no tool for reconciling. When the same hold landed on
// jv-t718-gate-dirty-warn, a worker could not hear its own PO, and
// jevons_agent_kill refused with the one escape a product owner had: ask the
// overseer to throw the messages away. The other thing that was asked for that
// night was an editor on ~/.jevons/sendq/ge-t191-ndk-discovery.json, under a
// running daemon, to mark one entry confirmed and fold two newer ones — a
// write the daemon's own atomic rename would have eaten or been eaten by.
//
// The evidence for that edit was sound (session f1a44c7f: queue-operation
// enqueue 15:25:32Z, remove 15:26:12Z, queued_command attachment 9388). The
// FACT was established; only the write-back was missing. This tool is the
// write-back, and it keeps the two halves of 🎯T416 that made the deadlock —
// no replay on an unknown outcome, no claim of non-delivery without evidence —
// by refusing to be an automatic policy: a human (or an agent that went and
// looked) supplies the verdict and the evidence, and both are recorded.
//
// jevons_agent_start with no prompt remains the non-destructive drain for
// PENDING entries. It is named here and in the pin line so it stops being
// folklore, but it is not the answer to an uncertain one: starting does not
// retry a held attempt, by design.

// registerSendqReconcileTool wires the reconcile verb. Registered next to the
// other late tools in New; a Server with no MCP transport (hermetic tests)
// still gets the handler, which is what the oracles call.
func (s *Server) registerSendqReconcileTool() {
	if s == nil || s.mcpSrv == nil {
		return
	}
	s.addTool(
		mcp.NewTool("jevons_sendq_reconcile",
			mcp.WithDescription("Resolve a held daemon sendq entry by operator judgement (🎯T726) — the named path out of PINNED. NEVER edit ~/.jevons/sendq/*.json while the daemon is running: that write races the daemon's atomic rename and the loser is silent. Call with only name= to SEE the queue (entry ids, delivery state, attempt ids, ages, payload previews) before deciding anything. Then: action=confirmed when you established the receiver HAS it, action=requeue when you established it never landed (the only outcome that permits a resend — 🎯T416), action=drop to abandon it deliberately and on the record, action=consolidate to fold superseded messages into one authoritative message so a seat that fell behind does not act on the stalest. Every mutating action requires actor= and evidence=; the disposition is logged with both. 🎯T416's three instruments that work: payload-match at user-message level in the receiver's JSONL, the receiver's own queue-operation/queued_command records, and transcript-file absence. The three that passed while WRONG: transcript growth, a raw grep of the session file, and the receiver's behaviour. For merely PENDING entries nothing needs deciding and nothing is discarded: action=drain offers the backlog to the live seat now. That is the non-destructive path, and it is an operation, not folklore — jevons_agent_start name=<seat> with no prompt remains the way to get a live process when there is none."),
			mcp.WithString("name", mcp.Required(), mcp.Description("The addressee agent whose queue is held, e.g. claudia-po")),
			mcp.WithString("action", mcp.Description("show (default) | drain | confirmed | requeue | drop | consolidate")),
			mcp.WithString("entry_id", mcp.Description("Queue entry to reconcile, as shown by action=show or named in the PINNED notice")),
			mcp.WithString("attempt_id", mcp.Description("The entry's unresolved attempt id; required for a non-pending entry so a disposition cannot land on a stale view of the queue")),
			mcp.WithString("actor", mcp.Description("Who established this (your agent name, or owner). Recorded with the disposition.")),
			mcp.WithString("evidence", mcp.Description("What you observed, specifically — session id and the queue-operation/payload-match records you read. Recorded verbatim.")),
			mcp.WithString("keep", mcp.Description("consolidate: entry id that survives as the authoritative message (default: the newest)")),
			mcp.WithString("text", mcp.Description("consolidate: replace the survivor's payload with this authoritative message (default: keep what was accepted)")),
		),
		s.handleSendqReconcile,
	)
}

func (s *Server) handleSendqReconcile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name := strings.TrimSpace(str(args["name"]))
	if name == "" {
		return mcp.NewToolResultError("name is required (the agent whose queue is held)"), nil
	}
	action := strings.ToLower(strings.TrimSpace(str(args["action"])))
	if action == "" {
		action = "show"
	}
	actor := strings.TrimSpace(str(args["actor"]))
	if actor == "" && s.transcript != nil {
		actor = s.overseerName()
	}
	evidence := strings.TrimSpace(str(args["evidence"]))
	entryID := strings.TrimSpace(str(args["entry_id"]))
	attemptID := strings.TrimSpace(str(args["attempt_id"]))
	now := s.sweepClock()

	if action == "show" {
		return mcp.NewToolResultText(s.describeSendqForReconcile(name, now)), nil
	}
	if action == "drain" {
		return mcp.NewToolResultText(s.drainHeldSendqOnRequest(name, now)), nil
	}

	var (
		rec sendq.Receipt
		err error
	)
	switch action {
	case string(sendq.ReconcileConfirmed), string(sendq.ReconcileRequeue), string(sendq.ReconcileDrop):
		rec, err = s.sendQueue().Reconcile(name, entryID, attemptID, sendq.ReconcileOutcome(action), actor, evidence, now)
	case string(sendq.ReconcileConsolidate):
		rec, err = s.sendQueue().Consolidate(name, strings.TrimSpace(str(args["keep"])),
			strings.TrimSpace(str(args["text"])), actor, evidence, now)
	default:
		return mcp.NewToolResultError(fmt.Sprintf(
			"unknown action %q (want show, drain, confirmed, requeue, drop or consolidate)", action)), nil
	}
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(s.recordSendqReconciliation(name, rec, now)), nil
}

// recordSendqReconciliation is the half that makes a disposition a RECORD
// rather than a mutation: the removed payloads are logged in full before the
// seat is unpinned, the overseer is told once, and a queue that still has
// pending work is kicked so reconciliation actually returns the seat to
// service instead of leaving it quietly unblocked.
func (s *Server) recordSendqReconciliation(name string, rec sendq.Receipt, now time.Time) string {
	removed := make([]map[string]any, 0, len(rec.Removed))
	for _, e := range rec.Removed {
		removed = append(removed, map[string]any{
			"entry_id": e.ID, "attempt_id": e.AttemptID, "state": string(e.State),
			"bytes": len(e.Text), "enqueued_at": e.EnqueuedAt.Format(time.RFC3339),
			// The payload itself, not a description of it: an operator who
			// drops an unconfirmed message must leave behind the message.
			"text": e.Text,
		})
	}
	s.LogEvent("agent_send", "sendq_reconcile", map[string]any{
		"agent": name, "actor": rec.By.Actor, "outcome": string(rec.By.Outcome),
		"evidence": rec.By.Evidence, "removed": removed,
		"removed_bytes": rec.RemovedBytes(), "depth_after": rec.Depth,
	})

	// The pin is the seat's blocker, and the blocker is gone. Clearing it here
	// (rather than waiting for the next sweep to notice) is what "returns to
	// service by that path" means from the caller's side.
	s.clearSendqPin(name)

	var b strings.Builder
	fmt.Fprintf(&b, "Reconciled %s queue for %q: %s.\n", rec.By.Outcome, name, rec.By.Describe())
	for _, e := range rec.Removed {
		fmt.Fprintf(&b, "  removed %s (%s, %d bytes, waiting %s) — recorded in the eventlog with its payload\n",
			e.ID, stateWord(e.State), len(e.Text), e.Age(now).Round(time.Second))
	}
	for _, e := range rec.Kept {
		fmt.Fprintf(&b, "  kept %s (%s, %d bytes, waiting %s)\n",
			e.ID, stateWord(e.State), len(e.Text), e.Age(now).Round(time.Second))
	}
	if pin, pinned := s.sendqPinFor(name); pinned {
		fmt.Fprintf(&b, "Still PINNED on %s: %s\n", pin.EntryID, FormatSendqPinLine(name, pin))
		s.notifyFleetHealth(rec.By.At.Format(time.RFC3339Nano)+":"+name, fmt.Sprintf(
			"sendq reconciled for %q (%s), but the queue is still blocked by entry %s (🎯T726).",
			name, rec.By.Describe(), pin.EntryID))
		return b.String()
	}

	fmt.Fprintf(&b, "%q is no longer blocked by an unresolved attempt; %d message(s) remain queued.\n", name, rec.Depth)
	s.notifyFleetHealth(rec.By.At.Format(time.RFC3339Nano)+":"+name, fmt.Sprintf(
		"sendq for %q reconciled by %s: %s (%s). %d removed (%d bytes, payloads in the eventlog), %d still queued. "+
			"The seat is no longer held by an unresolved delivery attempt (🎯T726).",
		name, rec.By.Actor, rec.By.Outcome, rec.By.Evidence, len(rec.Removed), rec.RemovedBytes(), rec.Depth))

	if rec.Depth > 0 {
		if _, live := s.liveSender(name); live {
			b.WriteString("Kicking a drain now: the remaining messages are pending and the seat is live.\n")
			go s.drainAgentSendQueue(name)
		} else {
			fmt.Fprintf(&b, "No live process to deliver to; the pending messages drain at the next sweep, "+
				"or immediately after jevons_agent_start name=%q with no prompt.\n", name)
		}
	}
	return b.String()
}

// describeSendqForReconcile is the read path. It exists because the only way
// to see this queue was to open the JSON, and an operator who has opened the
// JSON is one keystroke from editing it.
func (s *Server) describeSendqForReconcile(name string, now time.Time) string {
	entries, err := s.sendQueue().Snapshot(name)
	if err != nil {
		return fmt.Sprintf("sendq for %q is unreadable: %v\nDo NOT repair it by hand while the daemon is running.", name, err)
	}
	var b strings.Builder
	if len(entries) == 0 {
		fmt.Fprintf(&b, "sendq for %q is empty: nothing is held.\n", name)
	} else {
		fmt.Fprintf(&b, "sendq for %q holds %d message(s), oldest first:\n", name, len(entries))
	}
	for _, e := range entries {
		fmt.Fprintf(&b, "  %s  %-10s  %5d bytes  waiting %s\n",
			e.ID, stateWord(e.State), len(e.Text), e.Age(now).Round(time.Second))
		if e.AttemptID != "" {
			fmt.Fprintf(&b, "      attempt %s%s\n", e.AttemptID, detailSuffix(e.Detail))
		}
		if e.Reconciled != nil {
			fmt.Fprintf(&b, "      reconciled %s\n", e.Reconciled.Describe())
		}
		fmt.Fprintf(&b, "      %s\n", previewPayload(e.Text))
	}
	if pin, pinned := s.sendqPinFor(name); pinned {
		fmt.Fprintf(&b, "\n%s\n", FormatSendqPinLine(name, pin))
		fmt.Fprintf(&b, "\nNext call, once you have LOOKED (🎯T416: payload-match at user-message level, the receiver's own "+
			"queue-operation records, transcript-file absence):\n"+
			"  jevons_sendq_reconcile name=%q action=confirmed|requeue|drop entry_id=%q attempt_id=%q actor=<you> evidence=<what you read>\n",
			name, pin.EntryID, pin.AttemptID)
	} else if len(entries) > 0 {
		fmt.Fprintf(&b, "\nNothing is unresolved, so nothing needs deciding. Offer the head to the seat now:\n"+
			"  jevons_sendq_reconcile name=%q action=drain\n", name)
		if len(entries) > 1 {
			fmt.Fprintf(&b, "Or fold superseded messages into one authoritative message first:\n"+
				"  jevons_sendq_reconcile name=%q action=consolidate keep=%q actor=<you> evidence=<why these are superseded>\n",
				name, entries[len(entries)-1].ID)
		}
	}
	return b.String()
}

// drainHeldSendqOnRequest is the non-destructive move, made first-class.
//
// On 2026-09-20 a no-prompt jevons_agent_start drained jv-t718-gate-dirty-warn's
// held queue (sendq 0b7f6383b85c) — the only escape anybody found that did not
// throw messages away, and it was folklore: nothing named it, and the seat's
// own PINNED notice offered a PO an overseer kill instead. Starting a seat is a
// heavy way to say "offer the backlog now", and it is the wrong instrument when
// a process is already there.
//
// It refuses past an unresolved attempt rather than skipping it. A start would
// not retry that entry either (🎯T623), and offering the message behind it
// would deliver the queue out of order — reconcile the head first, which is
// what the refusal says.
func (s *Server) drainHeldSendqOnRequest(name string, now time.Time) string {
	before, err := s.sendQueue().Snapshot(name)
	if err != nil {
		return fmt.Sprintf("sendq for %q is unreadable: %v\nDo NOT repair it by hand while the daemon is running.", name, err)
	}
	if len(before) == 0 {
		return fmt.Sprintf("sendq for %q is empty: nothing to drain.\n", name)
	}
	// 🎯T766.5: a held entry no longer blocks the drain. It is never resent;
	// the pending messages behind it are offered past it, as the automatic
	// drain does. The reply still names it, because it still needs a decision.
	heldNote := ""
	if e, blocked, err := s.sendQueue().BlockedHead(name); err == nil && blocked {
		heldNote = fmt.Sprintf(
			"Entry %s has an unresolved %s attempt %s and stays held — it is never resent, and the messages behind it drain past it (🎯T766.5).\n"+
				"Settle it with: jevons_sendq_reconcile name=%q action=confirmed|requeue|drop entry_id=%[1]s attempt_id=%[3]s actor=… evidence=…\n",
			e.ID, e.State, e.AttemptID, name)
	}
	if _, live := s.liveSender(name); !live {
		return fmt.Sprintf(
			"%q holds %d pending message(s), oldest waiting %s, but has no live process to deliver to.\n"+
				"Give it one and the queue drains at the first turn boundary: jevons_agent_start name=%[1]q (no prompt).\n",
			name, len(before), before[0].Age(now).Round(time.Second))
	}

	s.drainAgentSendQueue(name)

	after, err := s.sendQueue().Snapshot(name)
	if err != nil {
		return fmt.Sprintf("drained %q, but the queue is now unreadable: %v\n", name, err)
	}
	delivered := len(before) - len(after)
	s.LogEvent("agent_send", "sendq_drain_on_request", map[string]any{
		"agent": name, "depth_before": len(before), "depth_after": len(after), "delivered": delivered,
	})
	var b strings.Builder
	fmt.Fprintf(&b, "Drained %q: %d of %d held message(s) delivered, %d still queued.\n",
		name, delivered, len(before), len(after))
	b.WriteString(heldNote)
	switch {
	case delivered == 0:
		b.WriteString("Nothing moved — the seat is most likely mid-turn, and the backlog is offered again at its next boundary.\n")
	case len(after) > 0:
		// One message per turn boundary is deliberate: handing a pane several
		// at once is what 🎯T416 stopped doing, because the provider's own
		// queue merges them silently. The rest are not stuck.
		b.WriteString("The remainder follows one per turn boundary — that cadence is deliberate (🎯T416), not a stall.\n")
	}
	if pin, pinned := s.sendqPinFor(name); pinned {
		fmt.Fprintf(&b, "%s\n", FormatSendqPinLine(name, pin))
	}
	return b.String()
}

// stateWord names a delivery state for an operator. Pending's empty string is
// the legacy representation and reads as a missing column in a listing.
func stateWord(st sendq.DeliveryState) string {
	if st == sendq.Pending {
		return "pending"
	}
	return string(st)
}

func detailSuffix(detail string) string {
	if strings.TrimSpace(detail) == "" {
		return ""
	}
	return " (" + strings.TrimSpace(detail) + ")"
}

// previewPayload shows enough of a message to recognise it without pasting a
// whole brief into a listing.
func previewPayload(text string) string {
	line := strings.TrimSpace(text)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	const max = 100
	if len(line) > max {
		return line[:max] + "…"
	}
	if line == "" {
		return "(empty payload)"
	}
	return line
}
