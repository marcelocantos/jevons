// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T599 — an undeliverable sendq message never makes a seat unkillable, and
// a pinned seat says so instead of looking busy.
//
// 2026-08-31: jv-t592-chatlog-turns was stranded on a Codex seat with a
// read-only sandbox. Its parent's kills were refused by the 🎯T530 guard
// ("daemon sendq still holds 1 message(s)"), and the one documented drain — a
// start — would have delivered exactly the message the overseer had ruled
// must not be delivered. The overseer's kill went through; that is the
// behaviour pinned here. Meanwhile agent_list showed an ordinary running
// seat: nothing said it could not be moved.
//
// Three parts:
//  1. An overseer kill always succeeds: the T530 hold yields to overseer
//     authority and the held messages are discarded, with one eventlog
//     record naming the count and the reason.
//  2. A parent kill still respects the guard, but the refusal names the
//     drain path and the explicit override instead of leaving the caller
//     with no way forward.
//  3. A seat whose sendq holds a message that cannot be delivered — the
//     provider cannot accept it, or delivery has failed
//     SendqPinFailureThreshold times — is reported by jevons_agent_list and
//     fleet health as PINNED with the blocking message named.

// SendqPinFailureThreshold is how many failed deliveries of the same queue
// entry make the seat pinned. One failure is weather (a pane mid-rotation);
// the same message failing repeatedly is a seat that cannot be moved by the
// documented drain.
const SendqPinFailureThreshold = 3

// SendqPin names the message that is blocking a seat: which entry, why, and
// how many delivery attempts have failed.
type SendqPin struct {
	EntryID string
	Reason  string
	Fails   int
	At      time.Time
}

// FormatSendqPinLine is the operator-facing account of a pinned seat, shared
// by agent_list and the fleet-health notice so the two never drift.
func FormatSendqPinLine(name string, pin SendqPin) string {
	fails := ""
	if pin.Fails > 0 {
		fails = fmt.Sprintf("; %d failed deliveries", pin.Fails)
	}
	return fmt.Sprintf(
		"PINNED %s: sendq message %s cannot be delivered (%s%s). "+
			"A start would deliver it; an overseer kill discards it (🎯T599).",
		name, pin.EntryID, pin.Reason, fails)
}

// sendqPinFor answers whether a seat is pinned and by what.
func (s *Server) sendqPinFor(name string) (SendqPin, bool) {
	if s == nil || strings.TrimSpace(name) == "" {
		return SendqPin{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pin, ok := s.sendqPin[name]
	return pin, ok
}

// clearSendqPin forgets pin state for a seat: a delivered message, a drained
// queue, or a removed seat is no longer blocking anything.
func (s *Server) clearSendqPin(name string) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	s.mu.Lock()
	delete(s.sendqPin, name)
	delete(s.sendqPinFails, name)
	s.mu.Unlock()
}

// MarkSendqPinned pins a seat directly: the caller knows the head message
// cannot be delivered at all (the provider cannot accept it). Notifies fleet
// health once per pin.
func (s *Server) MarkSendqPinned(name string, e sendq.Entry, reason string) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	pin := SendqPin{EntryID: e.ID, Reason: strings.TrimSpace(reason), At: time.Now()}
	s.mu.Lock()
	if s.sendqPinFails != nil {
		pin.Fails = s.sendqPinFails[name].fails
	}
	already := false
	if s.sendqPin == nil {
		s.sendqPin = map[string]SendqPin{}
	} else {
		_, already = s.sendqPin[name]
	}
	s.sendqPin[name] = pin
	s.mu.Unlock()
	if !already {
		s.notifyFleetHealth(FormatSendqPinLine(name, pin))
	}
}

// sendqEntryFails tracks repeated delivery failure of one queue entry.
type sendqEntryFails struct {
	entryID string
	fails   int
}

// noteSendqDeliveryFailure counts a failed delivery of a still-held entry
// and pins the seat when the same entry has failed SendqPinFailureThreshold
// times. A different entry resets the count: the queue moved, so the seat is
// not stuck on one message.
func (s *Server) noteSendqDeliveryFailure(name string, e sendq.Entry, reason string) {
	if s == nil || strings.TrimSpace(name) == "" || e.ID == "" {
		return
	}
	s.mu.Lock()
	if s.sendqPinFails == nil {
		s.sendqPinFails = map[string]sendqEntryFails{}
	}
	rec := s.sendqPinFails[name]
	if rec.entryID != e.ID {
		rec = sendqEntryFails{entryID: e.ID}
	}
	rec.fails++
	s.sendqPinFails[name] = rec
	s.mu.Unlock()
	if rec.fails >= SendqPinFailureThreshold {
		s.MarkSendqPinned(name, e, fmt.Sprintf("delivery failed: %s", strings.TrimSpace(reason)))
	}
}

// discardHeldSendqForOverseerKill drops a seat's whole held queue because the
// overseer is killing it — the T530 hold yields to overseer authority. One
// eventlog record names the count and the reason; the alternative was a seat
// that could not be moved off a broken provider because the only drain would
// deliver a message the overseer had ruled must not be delivered.
func (s *Server) discardHeldSendqForOverseerKill(name, actor string) (int, error) {
	count := s.pendingAgentSends(name)
	if err := s.sendQueue().Clear(name); err != nil {
		return 0, err
	}
	s.clearSendqPin(name)
	s.logLifecycle(compAgentLifecycle, "kill_discard_sendq", "ok", map[string]any{
		"name":      name,
		"actor":     actor,
		"discarded": count,
		"reason": "overseer kill overrides the 🎯T530 hold: held messages are " +
			"discarded undelivered rather than pinning the seat (🎯T599)",
	})
	return count, nil
}
