// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"time"
)

// 🎯T664 — an uncertain delivery verdict never stops a seat.
//
// The original stopped-worker shape: a send came back delivered_unconfirmed
// (🎯T429 — the instrument could not decide), and the overseer, not knowing
// what to do next, stopped the seat. The verdict text already said "do not
// re-send" and named the transcript read; it never said "do not stop", and
// jevons_agent_stop parked any seat on request (🎯T414). So the daemon now
// remembers the undecided delivery, and stop / kill refuse a seat while a
// turn is in flight or a delivery is still undecided, naming the check that
// decides it. force=true, with a reason, is the override.

// unconfirmedSend is a delivery the daemon handed over and did not see land.
type unconfirmedSend struct {
	At      time.Time
	Payload string
}

// unconfirmedSendTTL bounds how long an undecided delivery guards a seat. A
// verdict older than this is stale evidence: the turn boundary that would
// have cleared it has either passed unobserved (a restart) or the seat is
// genuinely wedged, and either way the operator may act.
const unconfirmedSendTTL = 15 * time.Minute

// noteUnconfirmedSend records a delivered_unconfirmed verdict for name.
func (s *Server) noteUnconfirmedSend(name, payload string) {
	s.noteUnconfirmedSendAt(name, payload, time.Now())
}

func (s *Server) noteUnconfirmedSendAt(name, payload string, at time.Time) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unconfirmedSends == nil {
		s.unconfirmedSends = map[string]unconfirmedSend{}
	}
	s.unconfirmedSends[name] = unconfirmedSend{At: at, Payload: payload}
}

// clearUnconfirmedSend forgets the verdict: a turn boundary was observed or
// a later delivery was confirmed, so the question the guard asked is answered.
func (s *Server) clearUnconfirmedSend(name string) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.unconfirmedSends, name)
}

// pendingUnconfirmedSend returns the undecided delivery for name, if one is
// recorded and still within the TTL.
func (s *Server) pendingUnconfirmedSend(name string, now time.Time) (unconfirmedSend, bool) {
	if s == nil {
		return unconfirmedSend{}, false
	}
	s.mu.Lock()
	u, ok := s.unconfirmedSends[name]
	s.mu.Unlock()
	if !ok || now.Sub(u.At) > unconfirmedSendTTL {
		return unconfirmedSend{}, false
	}
	return u, true
}

// StopGuard is the pure rule: may a seat be stopped or killed now? A turn in
// flight, or a delivery the daemon could not confirm and nothing has decided
// since, both say no — the answer is to look, not to stop.
func StopGuard(flight TurnFlight, pending *unconfirmedSend, now time.Time) (refuse bool, why string) {
	if flight == FlightInFlight {
		return true, "a turn is in flight"
	}
	if pending != nil && now.Sub(pending.At) <= unconfirmedSendTTL {
		snippet := strings.TrimSpace(pending.Payload)
		if len(snippet) > 80 {
			snippet = snippet[:80] + "…"
		}
		return true, fmt.Sprintf("a delivery is still undecided (delivered_unconfirmed at %s: %q)",
			pending.At.Format("15:04:05"), snippet)
	}
	return false, ""
}

// stopGuardFor applies StopGuard to the daemon's own records for name.
func (s *Server) stopGuardFor(name string) (bool, string) {
	if s == nil {
		return false, ""
	}
	now := time.Now()
	var pending *unconfirmedSend
	if u, ok := s.pendingUnconfirmedSend(name, now); ok {
		pending = &u
	}
	return StopGuard(s.flightState(name), pending, now)
}

// FormatStopRefusal is the caller-facing refusal: it names why, the
// instrument that decides it, and the override.
func FormatStopRefusal(verb, name, why string) string {
	return fmt.Sprintf(
		"refusing to %s %q — %s (🎯T664). An uncertain verdict is resolved by looking, not by stopping: "+
			"jevons_transcript_read name=%q — a user message carrying the payload means it landed — "+
			"or wait for the turn boundary. If you can state a reason to stop anyway, pass force=true with reason=….",
		verb, name, why, name)
}
