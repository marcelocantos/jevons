// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// AgentStatusFaulted is a delivered, rebriefed seat which swallowed both turns.
const AgentStatusFaulted = "faulted"

// EmptyTurnFault returns the row annotation, if the empty-turn incident is
// active. A successful answer clears the streak; a rebrief alone does not.
func (s *Server) EmptyTurnFault(name string) (string, bool) {
	if s == nil || s.idleActivity == nil {
		return "", false
	}
	a := s.idleActivity.Get(name)
	if a.EmptyStreak >= 2 && a.RecoverAfterEmpty {
		return fmt.Sprintf("FAULTED %s: %d consecutive delivered turns ended terminal_empty (stopReason %s, output/context limit); rebrief produced no output", name, a.EmptyStreak, a.EmptyCause), true
	}
	return "", false
}

// emptyTurnBackoff makes the recovery refusal legible even before the second
// terminal arrives. The shared recovery ledger is the source of backoff time.
func (s *Server) EmptyTurnBackoff(name string) string {
	if s == nil || s.idleActivity == nil || s.idleNudgeLedger == nil {
		return ""
	}
	a := s.idleActivity.Get(name)
	if a.EmptyStreak == 0 || !a.RecoverAfterEmpty {
		return ""
	}
	count, last := s.idleNudgeLedger.Get(name)
	if last.IsZero() || time.Since(last) >= NextNudgeBackoff(count, DefaultFleetRecoverBackoffs) {
		return ""
	}
	return "repressure refused: backoff after terminal_empty"
}

// noteEmptyTurnFault sends one typed notice at the terminal boundary, not
// during a polling sweep. The latch is set before delivery to avoid duplicate
// notices from concurrent observation paths.
func (s *Server) noteEmptyTurnFault(name string) {
	if s == nil || s.idleActivity == nil || s.registry == nil {
		return
	}
	d := s.registry.Def(name)
	if d == nil || d.Purpose != "" && d.Purpose != claudia.PurposeWork {
		return
	}
	t := s.idleActivity
	t.mu.Lock()
	a := t.by[name]
	if a.EmptyStreak < 2 || !a.RecoverAfterEmpty || a.EmptyFaultNotified {
		t.mu.Unlock()
		return
	}
	a.EmptyFaultNotified = true
	t.by[name] = a
	t.mu.Unlock()
	line, _ := s.EmptyTurnFault(name)
	parent := strings.TrimSpace(d.Parent)
	if parent == "" {
		parent = s.overseerName()
	}
	if parent == "" || parent == name {
		return
	}
	res, err := s.deliverByName(parent, "[daemon 🎯T1238 seat-fault] "+line+". Two accepted deliveries were swallowed by the output/context limit; re-mint a fresh seat instead of rebriefing this context-exhausted session.", OriginAgent, false)
	if !noticeSubmitted(res.Status, err) {
		slog.Warn("empty-turn fault parent notice not submitted", "agent", name, "parent", parent, "err", err)
	}
}
