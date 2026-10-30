// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T1257 — never park or reap a seat with undelivered owner messages.
//
// Specimen: 2026-10-28 13:38, jevons-po parked jv-t1244-widget-implement as
// "all code/docs landed and gated" while two owner notes had waited an hour
// in its queue, neither of them acted on. The owner found it by inspection
// and had to restart the seat itself. jevons_agent_stop (park or the
// finished-work auto-reap path) must not need an owner to catch this by
// hand: the check belongs in the tool, not in the caller's judgement.
//
// An OwnerOrigin entry still present in sendq (🎯T1257, sendq.Entry) is, by
// construction, a message the seat has not received — Resolve and
// DischargePending remove a settled entry from the file. jevons_agent_stop
// reads that list before it proceeds with either disposition and refuses,
// naming the messages, unless the caller passes an explicit override that
// is itself logged and surfaced to the owner (never silent).

// FormatOwnerMessageStopRefusal is the caller-facing refusal when a seat
// still holds undelivered owner-origin sendq entries. It names each
// message (truncated) so the refusal is diagnosable without a second call.
func FormatOwnerMessageStopRefusal(verb, name string, entries []sendq.Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "refusing to %s %q — %d owner message(s) are still undelivered to this seat (🎯T1257):\n",
		verb, name, len(entries))
	for i, e := range entries {
		text := strings.TrimSpace(e.Text)
		if len(text) > 160 {
			text = text[:160] + "…"
		}
		fmt.Fprintf(&b, "  %d. [%s, enqueued %s] %s\n", i+1, e.ID, e.EnqueuedAt.Format("2006-01-02T15:04:05Z"), text)
	}
	b.WriteString(
		"Deliver them first (jevons_agent_send, or wait for the seat's next turn boundary to drain them), " +
			"or pass override_owner_messages=true with a reason to proceed anyway — the override is logged and " +
			"surfaced to the owner, it is never silent.")
	return b.String()
}

// undeliveredOwnerMessages returns the OwnerOrigin entries still held in
// name's sendq. A nil/empty sendq or an unreadable queue is reported as "no
// messages" rather than an error: the stop guard's job is to catch a known
// held message, not to add a new reason to refuse every stop.
func (s *Server) undeliveredOwnerMessages(name string) []sendq.Entry {
	if s == nil {
		return nil
	}
	entries, err := s.sendQueue().UndeliveredOwnerOrigin(name)
	if err != nil {
		return nil
	}
	return entries
}

// FormatOwnerMessagesPendingNotice is the parent-facing notice when the
// 🎯T165/🎯T195 auto-reap sweep kept a finish-report-classified-done seat
// specifically because its sendq still holds undelivered owner-origin
// entries (🎯T1257). Unlike the manual jevons_agent_stop refusal, this
// sweep has no caller to answer a refusal to — the parent that owns the
// seat is told instead, so the hold does not sit silent.
func FormatOwnerMessagesPendingNotice(targetID, worker string, entries []sendq.Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🎯T1257: %s was NOT auto-reaped as finished work", worker)
	if t := strings.TrimSpace(targetID); t != "" {
		fmt.Fprintf(&b, " (target %s)", t)
	}
	fmt.Fprintf(&b, " — %d owner message(s) are still undelivered to it:\n", len(entries))
	for i, e := range entries {
		text := strings.TrimSpace(e.Text)
		if len(text) > 160 {
			text = text[:160] + "…"
		}
		fmt.Fprintf(&b, "  %d. [%s, enqueued %s] %s\n", i+1, e.ID, e.EnqueuedAt.Format("2006-01-02T15:04:05Z"), text)
	}
	b.WriteString(
		"Deliver them first, or wait for the seat's next turn boundary to drain them. The seat stays registered " +
			"until the queue clears or you stop it yourself with override_owner_messages=true.")
	return b.String()
}

// notifyParentPendingOwnerMessages tells worker's parent (default jevons-po)
// that the auto-reap sweep held worker open specifically for undelivered
// owner-origin sendq entries (🎯T1257), mirroring notifyParentOutstandingScope
// and notifyParentOwnerImpactMissing.
func (s *Server) notifyParentPendingOwnerMessages(parent, worker, targetID string, entries []sendq.Entry) {
	if s == nil || strings.TrimSpace(worker) == "" || len(entries) == 0 {
		return
	}
	p := strings.TrimSpace(parent)
	if p == "" {
		p = "jevons-po"
	}
	msg := FormatOwnerMessagesPendingNotice(targetID, worker, entries)
	if _, err := s.deliverByName(p, msg, OriginAgent, false); err != nil {
		slog.Warn("T1257 owner-messages-pending notice undelivered to parent; escalating to overseer",
			"parent", p, "worker", worker, "target", targetID, "err", err)
		s.notifyFleetHealth(noticeAct, worker, fmt.Sprintf("parent %s unreachable (%v) for: %s", p, err, msg))
	}
}

// HasUndeliveredOwnerMessages is the server ledger sweep guard (🎯T1340).
func (s *Server) HasUndeliveredOwnerMessages(name string) bool {
	return len(s.undeliveredOwnerMessages(name)) > 0
}
