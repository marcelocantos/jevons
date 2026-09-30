// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
)

// IsStoredTerminalReportKind is true when text is a typed finish-report or
// scout-report — the terminal shapes the daemon stores before delivery
// (🎯T388 / 🎯T761).
func IsStoredTerminalReportKind(text string) bool {
	// Parse retains the claimed kind when required evidence slots are missing.
	// That report still ends the turn: evidence validation belongs to the
	// independent completion gate, not to the decision to nudge the worker.
	m, _ := envelope.Parse(text)
	if m == nil {
		return false
	}
	switch m.Kind {
	case envelope.KindFinishReport, envelope.KindScoutReport:
		return true
	default:
		return false
	}
}

// NewReportStoreLooksSatisfied returns the LooksSatisfied hook that reads the
// durable agent-report store (🎯T761). Empty stateDir yields a hook that always
// returns "".
func NewReportStoreLooksSatisfied(stateDir string) func(name string) string {
	stateDir = strings.TrimSpace(stateDir)
	return func(name string) string {
		name = strings.TrimSpace(name)
		if stateDir == "" || name == "" {
			return ""
		}
		rec, err := agentreport.Latest(stateDir, name)
		if err != nil || strings.TrimSpace(rec.Text) == "" {
			return ""
		}
		return rec.Text
	}
}

// AmbiguousFinishedVsStuck reports when a stored report exists but is not a
// typed terminal envelope, so the classifier cannot distinguish finished from
// stuck (🎯T761 clause 3).
func AmbiguousFinishedVsStuck(report string) (ambiguous bool, detail string) {
	report = strings.TrimSpace(report)
	if report == "" {
		return false, ""
	}
	if IsStoredTerminalReportKind(report) {
		return false, ""
	}
	return true, "stored report is not a typed finish-report or scout-report — cannot distinguish finished from stuck"
}

func storedTerminalFromReport(reportText string) (hasStoredTerminal, looksFinished bool) {
	if !IsStoredTerminalReportKind(reportText) {
		return false, false
	}
	return true, LooksLikeFinishedWorkReport(reportText)
}

// workerIdleSuppressReason is the 🎯T761 gate for worker-idle emission: the
// send-path flight ledger and the durable report store outrank ACP phase=idle.
func (s *Server) workerIdleSuppressReason(name string) (suppress bool, reason string) {
	if s == nil || strings.TrimSpace(name) == "" {
		return false, ""
	}
	if s.flightState(name) == FlightInFlight {
		return true, "turn_in_flight"
	}
	report := s.latestStoredReport(name)
	// 🎯T938: the parent already holds a declared blocker; once an
	// owner/parent message cleared it, the seat is ordinary open-mission work
	// again and its next idle is news to the parent.
	if _, _, blocker := s.storedReportFor(name, report); blocker != "" {
		return true, IdleSkipBlockedOnOwner
	}
	if _, blocked := envelope.BlockedOn(report); blocked {
		return false, ""
	}
	if IsStoredTerminalReportKind(report) {
		return true, "stored_terminal_report"
	}
	if amb, _ := AmbiguousFinishedVsStuck(report); amb {
		return true, "ambiguous_finished_vs_stuck"
	}
	return false, ""
}
