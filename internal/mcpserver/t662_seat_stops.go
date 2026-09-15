// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstop"
)

// 🎯T662 — every seat that stops records why; a mass stop raises one alert.
//
// The daemon's own stops (jevons_agent_stop / kill, the finished-work reap,
// a plan-policy park, an undelivered opening brief) record their reason at
// the site that performs them. A stop the daemon merely discovers — a
// handle whose process is no longer alive at the next sweep — records
// seatstop.Unknown: the harness exposes no exit status or signal, and an
// honest "unknown" beats the silence the PO had on 2026-09-15. Every record
// is one eventlog line (agent_lifecycle.seat_stop). When three or more
// seats stop within a minute and the daemon did not restart inside that
// window, agent_list, /api/agents and the overseer get one alert naming the
// shared reason, and a later send to such a seat says "rehydrated after
// <reason>" instead of "dead/stopped process".

// seatStops returns the ledger, minting it on first use.
func (s *Server) seatStops() *seatstop.Ledger {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seatStopLedger == nil {
		s.seatStopLedger = seatstop.New()
	}
	return s.seatStopLedger
}

// noteSeatStop records one stop and journals it.
func (s *Server) noteSeatStop(name string, source seatstop.Source, reason, actor, detail string) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	rec := s.seatStops().Note(seatstop.Record{
		Seat: name, Source: source, Reason: strings.TrimSpace(reason),
		Actor: strings.TrimSpace(actor), Detail: strings.TrimSpace(detail),
	})
	fields := map[string]any{
		"name": name, "source": string(rec.Source), "reason": rec.Reason, "at": rec.At.UTC().Format(time.RFC3339Nano),
	}
	if rec.Actor != "" {
		fields["actor"] = rec.Actor
	}
	if rec.Detail != "" {
		fields["detail"] = rec.Detail
	}
	s.logLifecycle(compAgentLifecycle, "seat_stop", "ok", fields)
}

// noteDeadSeats records the seats a sweep found not alive that nothing had
// already accounted for in the last few minutes. A seat the daemon stopped
// on purpose a moment ago keeps that reason; a seat that simply died gets
// Unknown.
func (s *Server) noteDeadSeats(reps []DeadAgentReport) {
	if s == nil || len(reps) == 0 {
		return
	}
	now := time.Now()
	for _, r := range reps {
		if last, ok := s.seatStops().Last(r.Name); ok && now.Sub(last.At) < 2*time.Minute {
			continue
		}
		detail := "found not alive by the fleet-health sweep"
		switch {
		case r.Recovered:
			detail += "; re-launched (AutoStart)"
		case r.Removed:
			detail += "; dead work seat removed (🎯T544)"
		case r.Declined != "":
			detail += "; left down per intent: " + r.Declined
		}
		s.noteSeatStop(r.Name, seatstop.SourceExit, seatstop.Unknown, "", detail)
	}
}

// sweepDeadAccounted is SweepDeadAgents with the 🎯T662 record kept.
func (s *Server) sweepDeadAccounted() []DeadAgentReport {
	if s == nil {
		return nil
	}
	return s.sweepDeadAccountedWith(s.overseerName(), s.fleetIntent())
}

func (s *Server) sweepDeadAccountedWith(overseer string, intent fleetintent.Snapshot) []DeadAgentReport {
	if s == nil {
		return nil
	}
	reps := SweepDeadAgents(s.registry, s.RemovalAccount(), overseer, intent)
	s.noteDeadSeats(reps)
	return reps
}

// massStopLookback bounds how far back a mass-stop reading looks. Wider
// than the burst window so the alert outlives the minute it describes.
const massStopLookback = 10 * time.Minute

// massStop is the current mass-stop reading, if any.
func (s *Server) massStop() (seatstop.Alert, bool) {
	if s == nil {
		return seatstop.Alert{}, false
	}
	now := time.Now()
	s.mu.Lock()
	boot := s.bootAt
	s.mu.Unlock()
	return seatstop.MassStop(s.seatStops().Recent(now, massStopLookback), seatstop.DefaultWindow, seatstop.DefaultMinSeats, boot)
}

// MassStopLine is the alert for agent_list, /api/agents and the RHS; empty
// when there is no burst. It also delivers the alert to the overseer once
// per burst.
func (s *Server) MassStopLine() string {
	a, ok := s.massStop()
	if !ok {
		return ""
	}
	line := seatstop.FormatAlert(a)
	s.mu.Lock()
	seen := s.massStopNotified == a.Key()
	if !seen {
		s.massStopNotified = a.Key()
	}
	s.mu.Unlock()
	if !seen {
		s.notifyFleetHealth(line)
	}
	return line
}

// withMassStop prepends the mass-stop alert to a tool body.
func (s *Server) withMassStop(body string) string {
	line := s.MassStopLine()
	if line == "" {
		return body
	}
	if strings.TrimSpace(body) == "" {
		return line
	}
	return line + "\n" + body
}

// SeatStopReason answers the HTTP /api/agents decoration: why name last
// stopped, and when.
func (s *Server) SeatStopReason(name string) (reason string, at time.Time, ok bool) {
	if s == nil {
		return "", time.Time{}, false
	}
	rec, ok := s.seatStops().Last(name)
	if !ok {
		return "", time.Time{}, false
	}
	return rec.Reason, rec.At, true
}

// rehydratedAfter is the send-result suffix for a seat the send had to
// relaunch: the recorded reason, or the old wording when nothing recorded one.
func (s *Server) rehydratedAfter(name string) string {
	if rec, ok := s.seatStops().Last(name); ok && s != nil {
		return fmt.Sprintf(" (rehydrated after %s)", rec.Reason)
	}
	return " (rehydrated after dead/stopped process)"
}
