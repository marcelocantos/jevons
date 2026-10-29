// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/jevons/internal/spool"
)

// observeSidecarSpoolTurnEnd folds a sidecar seat's newest dated turn_end spool
// record into the same turn boundary agentEventSink writes on IsTerminalStop
// (🎯T1186). OMP sidecars publish turn_end to the spool but may not emit a
// terminal Claudia stop on the wired sink; non-terminal motion events still pin
// InFlight=Yes on the authority until this runs.
func (s *Server) observeSidecarSpoolTurnEnd(d claudia.AgentDef) {
	if s == nil || strings.TrimSpace(d.Name) == "" || !seatprovider.Resolve(d.Provider).Sidecar {
		return
	}
	if !spool.SeatHasHistory(spool.Dir(), d.Name) {
		return
	}
	rec, ok, err := spool.LatestPhaseRecord(spool.Dir(), d.Name)
	if err != nil || !ok || rec.Type != "turn_end" {
		return
	}
	at := spoolRecordTime(rec)
	if at.IsZero() {
		return
	}
	s.boundTurnFromSidecarSpool(d.Name, at)
}

// boundTurnFromSidecarSpool is the shared second half: authority idle + flight
// ledger idle + any stored done report may reap. Idempotent per spool timestamp.
func (s *Server) boundTurnFromSidecarSpool(name string, at time.Time) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	if !s.markSidecarTurnEndObserved(name, at) {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	s.Seats().FromTurnEvent(name, true, at)
	s.noteTurnEnded(name)
}

func spoolRecordTime(rec spool.Record) time.Time {
	if !rec.TS.IsZero() {
		return rec.TS
	}
	if rec.RawTS == "" {
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339Nano, rec.RawTS)
	if err != nil {
		at, _ = time.Parse(time.RFC3339, rec.RawTS)
	}
	return at
}

func (s *Server) markSidecarTurnEndObserved(name string, at time.Time) bool {
	if s == nil || strings.TrimSpace(name) == "" || at.IsZero() {
		return false
	}
	s.mu.Lock()
	if s.sidecarTurnEndAt == nil {
		s.sidecarTurnEndAt = map[string]time.Time{}
	}
	last := s.sidecarTurnEndAt[name]
	if !at.After(last) {
		s.mu.Unlock()
		return false
	}
	s.sidecarTurnEndAt[name] = at
	s.mu.Unlock()
	return true
}
