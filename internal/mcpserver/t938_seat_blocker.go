// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T938: a work seat whose latest stored finish-report declares
// `jevons: status blocked` with a named `jevons: blocker` is waiting on
// someone else — usually the owner. Pressing it cannot unblock it: on
// 2026-09-30 jv-t935-broker-auto-return waited on an owner go-ahead for a
// daemon restart while the impatience ladder sent it
// impatience_ladder_repressure nudges every few minutes.
//
// The blocked state is read from the durable report store, so it survives a
// daemon bounce. It ends when:
//   - the owner (owner-origin send) or an ancestor (direct-down send) sends
//     the seat a message — the blocker is being answered;
//   - ClearSeatBlocker is called — the blocker was marked cleared;
//   - the seat stores a newer report — its own word supersedes the old one.
//
// A clear is keyed to the content of the report it lifted, so a later
// blocked report (even a byte-identical repeat, which storeAgentReport
// forgets the clear for) blocks again.

// IdleSkipBlockedOnOwner is the idle-nudge skip reason for a seat whose
// stored finish-report declares it blocked with a named blocker.
const IdleSkipBlockedOnOwner = "blocked_on_owner"

// blockedReportKey identifies the report a clear lifted.
func blockedReportKey(report string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(report)))
	return hex.EncodeToString(sum[:])
}

// classifyStoredReport reads one seat's latest stored report for the idle,
// impatience, and recover sweeps. A blocked finish-report that nothing has
// cleared yields its blocker and neither terminal nor finished; once cleared
// it yields nothing, so the seat is an ordinary open-mission seat again and
// pressure resumes. Every other report reads as before.
func classifyStoredReport(report string, cleared func(report string) bool) (hasStoredTerminal, looksFinished bool, blocker string) {
	if strings.TrimSpace(report) == "" {
		return false, false, ""
	}
	if b, ok := envelope.BlockedOn(report); ok {
		if cleared != nil && cleared(report) {
			return false, false, ""
		}
		return false, false, b
	}
	hasStoredTerminal, looksFinished = storedTerminalFromReport(report)
	if !hasStoredTerminal {
		looksFinished = LooksLikeFinishedWorkReport(report)
	}
	return hasStoredTerminal, looksFinished, ""
}

// clearsSeatBlocker reports whether a delivered message answers a seat's
// blocker: the owner speaking, or an ancestor (parent PO, overseer)
// directing down. Daemon-composed traffic — idle nudges, worker-idle,
// restart notes — speaks as the owner surface with agent origin and never
// clears; neither does a peer or the seat itself.
func clearsSeatBlocker(origin SendOrigin, rel DeliverRelation, text string) bool {
	if IsIdleNudgeText(text) {
		return false
	}
	if origin == OriginOwner {
		return true
	}
	return rel == RelationDirectDown
}

// latestStoredReport returns name's latest stored report text through the
// same seam the sweeps read (the LooksSatisfied hook), falling back to the
// durable store.
func (s *Server) latestStoredReport(name string) string {
	if s == nil || strings.TrimSpace(name) == "" {
		return ""
	}
	s.mu.Lock()
	hook := s.idlePressureHooks.LooksSatisfied
	s.mu.Unlock()
	if hook != nil {
		return hook(name)
	}
	if dir := s.agentReportStateDir(); dir != "" {
		if rec, err := agentreport.Latest(dir, name); err == nil {
			return rec.Text
		}
	}
	return ""
}

// ClearSeatBlocker marks name's stored blocked finish-report cleared, so the
// idle-nudge and impatience sweeps treat the seat as an ordinary open-mission
// seat again. Returns false (and records nothing) when the latest stored
// report is not a blocked finish-report.
func (s *Server) ClearSeatBlocker(name, why string) bool {
	name = strings.TrimSpace(name)
	report := s.latestStoredReport(name)
	blocker, ok := envelope.BlockedOn(report)
	if !ok {
		return false
	}
	s.mu.Lock()
	if s.seatBlockerClears == nil {
		s.seatBlockerClears = map[string]string{}
	}
	s.seatBlockerClears[name] = blockedReportKey(report)
	s.mu.Unlock()
	s.logLifecycle(compIdleNudge, "seat_blocker_cleared", "ok", map[string]any{
		"agent": name, "blocker": blocker, "why": why,
	})
	return true
}

// seatBlockerCleared reports whether report is the blocked report a clear
// already lifted for name.
func (s *Server) seatBlockerCleared(name, report string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.seatBlockerClears[strings.TrimSpace(name)]
	return ok && key == blockedReportKey(report)
}

// forgetSeatBlockerClear drops name's clear when the seat stores a newer
// report: whatever that report says is the seat's current word.
func (s *Server) forgetSeatBlockerClear(name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.seatBlockerClears, strings.TrimSpace(name))
	s.mu.Unlock()
}

// storedReportFor is classifyStoredReport over the server's clear record.
func (s *Server) storedReportFor(name, report string) (hasStoredTerminal, looksFinished bool, blocker string) {
	return classifyStoredReport(report, func(r string) bool { return s.seatBlockerCleared(name, r) })
}
