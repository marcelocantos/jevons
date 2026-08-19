// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/marcelocantos/claudia"
)

// 🎯T530 — Killing or reminting a parent must not abandon held sendq on
// descendants that were mid-drain.
//
// The 2026-08-19 remint of jevons-po killed drain seats
// (jv-t417-ceiling / jv-t427-sha-stable / jv-t466-stop-attrib) mid-drain.
// Removal stamped them reaped again while sendq still held gate feedback,
// so fleet-health reaped_held regenerated from the parent kill alone.
// Earlier the same day, a PO killed jv-t401-reaped-addr before DRAINED/EMPTY
// — the kill-before-drain loop. Standing ops briefs were not enough; the
// product path refuses that leaf kill and auto-restarts held descendants
// after a parent kill.

// RemintGraceWindow is how long after a drain-restart the recovery path may
// take before a reaped_held older than that window is considered a regenerated
// failure (live probe / fleet-health).
const RemintGraceWindow = 2 * time.Minute

// HeldSeatSnapshot is a registry row that still had daemon sendq when a kill
// was about to remove it. Enough to re-mint a drain seat under a surviving
// parent without inventing identity.
type HeldSeatSnapshot struct {
	Name     string
	WorkDir  string
	Parent   string
	Purpose  string
	Provider string
	Model    string
	TargetID string
	Depth    int
}

// ClassifyKillHeldSendq decides the 🎯T530 posture for a kill.
//
//   - Leaf / recovery seat with held sendq → refuse (kill-before-drain).
//     A parent remint names a different target and must not be blocked
//     by the parent's own queue — that queue stays keyed by name for
//     the remint start (the 2026-08-19 unworkable-PO path).
//   - Target has descendants → allow the kill, then restart descendant
//     names that still hold sendq under the surviving parent.
func ClassifyKillHeldSendq(target string, descendants []string, depthOf func(string) int) (refuse bool, reason string, restart []string) {
	if depthOf == nil {
		return false, "", nil
	}
	target = strings.TrimSpace(target)
	var held []string
	for _, name := range descendants {
		name = strings.TrimSpace(name)
		if name == "" || name == target {
			continue
		}
		if depthOf(name) > 0 {
			held = append(held, name)
		}
	}
	if len(descendants) > 0 {
		return false, "", held
	}
	if target != "" && depthOf(target) > 0 {
		n := depthOf(target)
		return true, fmt.Sprintf(
			"refusing to kill %q — daemon sendq still holds %d message(s) (recovery seat before DRAINED/EMPTY; 🎯T530). "+
				"Wait for the drain to finish, or jevons_agent_send will keep holding until a start drains it.",
			target, n), nil
	}
	return false, "", nil
}

// ReapedHeldOlderThanGrace returns agent names whose held backlog is older
// than grace and that are not inside an active drain-restart window. Used by
// the live probe: a deliberate PO remint must leave this set empty.
func ReapedHeldOlderThanGrace(backlogs []struct {
	Agent  string
	Oldest time.Time
}, restartedAt map[string]time.Time, now time.Time, grace time.Duration) []string {
	if grace <= 0 {
		grace = RemintGraceWindow
	}
	var out []string
	for _, b := range backlogs {
		agent := strings.TrimSpace(b.Agent)
		if agent == "" || b.Oldest.IsZero() {
			continue
		}
		if now.Sub(b.Oldest) <= grace {
			continue
		}
		if at, ok := restartedAt[agent]; ok && !at.IsZero() && now.Sub(at) <= grace {
			continue // mid-recovery after parent kill
		}
		out = append(out, agent)
	}
	return out
}

// snapshotHeldSeats captures registry identity for names that still hold
// sendq. Call before the kill removes the rows.
func (s *Server) snapshotHeldSeats(names []string) []HeldSeatSnapshot {
	if s == nil || s.registry == nil || len(names) == 0 {
		return nil
	}
	var out []HeldSeatSnapshot
	for _, name := range names {
		depth := s.pendingAgentSends(name)
		if depth <= 0 {
			continue
		}
		snap := HeldSeatSnapshot{Name: name, Depth: depth}
		if def := s.registry.Def(name); def != nil {
			snap.WorkDir = def.WorkDir
			snap.Parent = def.Parent
			snap.Purpose = def.Purpose
			snap.Provider = string(def.Provider)
			snap.Model = def.Model
			snap.TargetID = def.TargetID
		}
		out = append(out, snap)
	}
	return out
}

// surviveDrainParent picks who owns restart-to-drain seats after killing
// root. Prefer root's parent while it still exists; else the kill actor;
// else the overseer.
func (s *Server) surviveDrainParent(root, actor string) string {
	if s != nil && s.registry != nil {
		if def := s.registry.Def(root); def != nil {
			p := strings.TrimSpace(def.Parent)
			if p != "" && s.registry.Def(p) != nil {
				return p
			}
		}
	}
	actor = strings.TrimSpace(actor)
	if actor != "" {
		if s != nil && (s.isOverseerAgent(actor) || (s.registry != nil && s.registry.Def(actor) != nil)) {
			return actor
		}
		if s == nil {
			return actor
		}
	}
	if s == nil {
		return "jevons"
	}
	return s.overseerName()
}

// noteDrainRestart records that name was scheduled for drain recovery at at,
// so fleet-health / live probes can apply RemintGraceWindow.
func (s *Server) noteDrainRestart(name string, at time.Time) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.drainRestartAt == nil {
		s.drainRestartAt = map[string]time.Time{}
	}
	s.drainRestartAt[name] = at
}

// drainRestartTimes is a copy of the restart schedule (tests / live probe).
func (s *Server) drainRestartTimes() map[string]time.Time {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.drainRestartAt) == 0 {
		return nil
	}
	out := make(map[string]time.Time, len(s.drainRestartAt))
	for k, v := range s.drainRestartAt {
		out[k] = v
	}
	return out
}

// restartHeldSendqForDrain re-registers held seats under newParent, lifts
// reaped intent, and kicks a drain. Launch failure still leaves the row
// registered so SweepSendBacklogs does not re-enter reaped_held solely from
// the parent kill (🎯T530).
func (s *Server) restartHeldSendqForDrain(held []HeldSeatSnapshot, newParent, actor string) []string {
	if s == nil || s.registry == nil || len(held) == 0 {
		return nil
	}
	newParent = strings.TrimSpace(newParent)
	if newParent == "" {
		newParent = s.overseerName()
	}
	if actor == "" {
		actor = "product:t530"
	}
	var restarted []string
	now := time.Now()
	for _, h := range held {
		name := strings.TrimSpace(h.Name)
		if name == "" {
			continue
		}
		workdir := strings.TrimSpace(h.WorkDir)
		if workdir == "" {
			workdir = "."
		}
		purpose := strings.TrimSpace(h.Purpose)
		if purpose == "" {
			purpose = claudia.PurposeWork
		}
		provider := strings.TrimSpace(h.Provider)
		if provider == "" {
			provider = string(claudia.ProviderGrok)
		}

		s.MarkAgentWorking(name, actor, "🎯T530 restart-to-drain after parent kill")

		if existing := s.registry.Def(name); existing != nil {
			// Race: row already back. Reparent in place (Def is the live pointer).
			existing.Parent = newParent
		} else {
			def := claudia.AgentDef{
				Name:         name,
				WorkDir:      workdir,
				SessionID:    uuid.New().String(),
				Parent:       newParent,
				Purpose:      purpose,
				Provider:     claudia.Provider(provider),
				Model:        h.Model,
				TargetID:     h.TargetID,
				Materialized: false,
				AutoStart:    true,
			}
			if err := s.registry.Register(def); err != nil {
				slog.Error("🎯T530 restart-to-drain register failed",
					"component", "agent_kill", "name", name, "err", err)
				continue
			}
		}

		s.noteDrainRestart(name, now)
		restarted = append(restarted, name)

		proc, err := s.launchForDrain(name)
		if err != nil {
			slog.Warn("🎯T530 restart-to-drain launch deferred",
				"component", "agent_kill", "name", name, "err", err,
				"detail", "seat is registered and working; sweep/start will drain")
		} else {
			s.wireAgentEvents(name, proc)
			go s.drainAgentSendQueue(name)
		}
	}
	if len(restarted) > 0 {
		s.notifyFleetHealth(fmt.Sprintf(
			"🎯T530 parent kill: restarted %d held-sendq seat(s) for drain under %q: %s. "+
				"reaped_held must not regenerate solely from this kill.",
			len(restarted), newParent, strings.Join(restarted, ", ")))
	}
	return restarted
}

// launchForDrain starts a restarted drain seat. Prefer the test seam when set.
func (s *Server) launchForDrain(name string) (*claudia.Agent, error) {
	if s != nil && s.drainLaunch != nil {
		return s.drainLaunch(name)
	}
	if s == nil || s.registry == nil {
		return nil, fmt.Errorf("agent registry not available")
	}
	return s.registry.Launch(name)
}

// ProbeReapedHeldOlderThanGrace is the 🎯T530 live-probe surface: names whose
// sendq is still held for a reaped (unregistered) seat, older than
// RemintGraceWindow, and not inside an active drain-restart. A deliberate
// PO remint must leave this empty.
func (s *Server) ProbeReapedHeldOlderThanGrace(now time.Time) []string {
	if s == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	backlogs, err := s.sendQueue().Backlogs()
	if err != nil || len(backlogs) == 0 {
		return nil
	}
	var aged []struct {
		Agent  string
		Oldest time.Time
	}
	for _, b := range backlogs {
		if s.agentIsRegistered(b.Agent) {
			continue
		}
		if _, ok := LookupReapedRecord(s.fleetIntent(), b.Agent); !ok {
			continue
		}
		aged = append(aged, struct {
			Agent  string
			Oldest time.Time
		}{Agent: b.Agent, Oldest: b.Oldest})
	}
	return ReapedHeldOlderThanGrace(aged, s.drainRestartTimes(), now, RemintGraceWindow)
}

// suppressHeldReapedDuringDrainRestart is true when reportHeldReapedBacklog
// should stay quiet because a parent-kill recovery for this name is still
// inside RemintGraceWindow.
func (s *Server) suppressHeldReapedDuringDrainRestart(name string, now time.Time) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	at, ok := s.drainRestartAt[name]
	s.mu.Unlock()
	if !ok || at.IsZero() {
		return false
	}
	return now.Sub(at) <= RemintGraceWindow
}

