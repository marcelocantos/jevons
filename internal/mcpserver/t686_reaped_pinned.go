// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T686 — a reaped seat must not keep raising a PINNED fleet-health alert
// the owner cannot act on.
//
// 2026-09-20: cl-t81-grok-billing took a Cursor ACP prompt in flight, sendq
// 3a62e454aeb3 went uncertain (broker-wrapped "prompt already in flight"),
// and the seat was reaped at 00:51. SweepSendBacklogs runs every ~30s and
// treated Uncertain before the 🎯T401 reaped-address path, so fleet health
// kept composing PINNED for a name that was no longer registered.
// jevons_agent_kill is already a no-op on a reaped name and is not the
// remedy: 🎯T623 forbids discarding an uncertain attempt automatically, and
// 🎯T401 keeps the hold so the closed address stays recoverable.
//
// PINNED (🎯T599) is a live-seat word. After reap the same hold is a closed
// address: say so once, or stay silent — never a bare PINNED.

// FormatReapedUncertainHoldLine is the operator-facing account of an
// unresolved sendq attempt whose seat has already left the registry.
func FormatReapedUncertainHoldLine(name string, pin SendqPin, rec fleetintent.Record) string {
	closed := "finished-and-reaped"
	if d := rec.Describe(); d != "" {
		closed = d
	}
	return fmt.Sprintf(
		"reaped-with-reason %s: sendq message %s has an unresolved %s attempt %s (%s). "+
			"The seat is %s — a recoverable closed address, not a live PINNED seat. "+
			"The payload remains held; a start will not retry it. jevons_agent_kill is a no-op here (🎯T401/🎯T686). "+
			"Resolve the attempt with jevons_sendq_reconcile name=%[1]q entry_id=%[2]q attempt_id=%[4]q (🎯T726).",
		name, pin.EntryID, pin.State, pin.AttemptID, pin.Reason, closed)
}

// noticeUncertainAttempt delivers one fleet-health line per unresolved
// attempt id. The sweep is a timer; without the guard the same hold
// becomes a repeating alarm (🎯T582 / 🎯T686).
func (s *Server) noticeUncertainAttempt(name string, pin SendqPin, line string) {
	if s == nil || strings.TrimSpace(name) == "" || line == "" {
		return
	}
	s.mu.Lock()
	if s.sendqAttemptNoticed == nil {
		s.sendqAttemptNoticed = map[string]string{}
	}
	noticed := s.sendqAttemptNoticed[name] == pin.AttemptID
	s.sendqAttemptNoticed[name] = pin.AttemptID
	s.mu.Unlock()
	if !noticed {
		s.notifyFleetHealth(pin.AttemptID, line)
	}
}
