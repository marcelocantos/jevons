// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/poauditor"
	"github.com/marcelocantos/jevons/internal/roles"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/staffops"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// AgentStatusPOAuditorMutualWait is the typed fault on agent_list and
// GET /api/agents when 🎯T1192 fires (per 🎯T713 reachability).
const AgentStatusPOAuditorMutualWait = "po_auditor_mutual_wait"

// poAuditorFaultSnap is the latest stall reading for one PO, served on list/API.
type poAuditorFaultSnap struct {
	PO       string
	Auditor  string
	Handoffs []poauditor.Handoff
	Detail   string
	At       time.Time
}

// poAuditorFaultCache holds the newest sample per PO for agent_list readers.
type poAuditorFaultCache struct {
	mu    sync.RWMutex
	byPO  map[string]poAuditorFaultSnap
	clear map[string]struct{}
}

func (c *poAuditorFaultCache) set(snap poAuditorFaultSnap) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.byPO == nil {
		c.byPO = make(map[string]poAuditorFaultSnap)
	}
	c.byPO[snap.PO] = snap
	delete(c.clear, snap.PO)
	c.mu.Unlock()
}

func (c *poAuditorFaultCache) clearPO(name string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.byPO != nil {
		delete(c.byPO, name)
	}
	if c.clear == nil {
		c.clear = make(map[string]struct{})
	}
	c.clear[name] = struct{}{}
	c.mu.Unlock()
}

func (c *poAuditorFaultCache) line(poName string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.RLock()
	snap, ok := c.byPO[poName]
	c.mu.RUnlock()
	if !ok || len(snap.Handoffs) == 0 {
		return "", false
	}
	list := poauditor.FormatHandoffList(snap.Handoffs)
	if list == "" {
		return "", false
	}
	return fmt.Sprintf("po_auditor_mutual_wait: auditor %s idle with empty queue; pending handoffs: %s",
		snap.Auditor, list), true
}

func poAuditorMutualWaitSymptom(poName string) string {
	return "po_auditor_mutual_wait:" + strings.TrimSpace(poName)
}

func (s *Server) ensurePOAuditorFaultCache() *poAuditorFaultCache {
	if s == nil {
		return nil
	}
	if s.poAuditorFaults == nil {
		s.poAuditorFaults = &poAuditorFaultCache{}
	}
	return s.poAuditorFaults
}

// POAuditorMutualWaitFaultLine is the /api/agents and agent_list annotation.
func (s *Server) POAuditorMutualWaitFaultLine(poName string) (string, bool) {
	c := s.ensurePOAuditorFaultCache()
	if c == nil {
		return "", false
	}
	return c.line(poName)
}

// samplePOAuditorMutualWait reads every product owner with an auditor child and
// pending child finish-reports on open targets (🎯T1192).
func (s *Server) samplePOAuditorMutualWait(rt *sentinelRuntime, overseer, workdir string, now time.Time, grace time.Duration, running func(string) bool) []staffops.POAuditorMutualWaitObs {
	if s == nil || s.registry == nil || rt == nil {
		return nil
	}
	if running == nil {
		running = func(name string) bool {
			proc := s.registry.Get(name)
			return proc != nil && s.seatState(name).Alive == seatstate.Yes
		}
	}
	stateDir := s.agentReportStateDir()
	defs := s.registry.List()
	wantLedger := targetfile.LedgerKey(workdir)

	var out []staffops.POAuditorMutualWaitObs
	for _, d := range defs {
		poName := strings.TrimSpace(d.Name)
		if poName == "" || poName == overseer || !purposeIsProductOwner(s.seatRoleOf(d)) {
			continue
		}
		if !targetfile.SameLedger(wantLedger, targetfile.LedgerKey(d.WorkDir)) {
			continue
		}
		auditor := findAuditorChild(defs, poName, s.seatRoleOf)
		if auditor == "" {
			continue
		}
		handoffs := pendingPOHandoffs(stateDir, workdir, defs, poName, running)
		poPhase := s.activityPhaseForStall(poName)
		audPhase := s.activityPhaseForStall(auditor)
		queueDepth := sendQueueDepth(s, auditor)
		graceElapsed := trackPOAuditorGrace(rt, poName, poPhase, running(poName), now, grace)
		verdict, detail := poauditor.Classify(poauditor.POObs{
			Name: poName, Alive: running(poName), Phase: poPhase, GraceElapsed: graceElapsed,
		}, poauditor.AuditorObs{
			Name: auditor, Alive: running(auditor), Phase: audPhase, QueueDepth: queueDepth,
		}, handoffs)
		if poauditor.IsFault(verdict) {
			obs := staffops.POAuditorMutualWaitObs{
				PO: poName, Auditor: auditor, Handoffs: handoffObs(handoffs), Detail: detail,
			}
			out = append(out, obs)
			s.ensurePOAuditorFaultCache().set(poAuditorFaultSnap{
				PO: poName, Auditor: auditor, Handoffs: handoffs, Detail: detail, At: now,
			})
			s.logLifecycle(compSentinel, "po_auditor_mutual_wait", "error", map[string]any{
				"po": poName, "auditor": auditor, "handoffs": len(handoffs), "detail": detail,
			})
		} else if c := s.ensurePOAuditorFaultCache(); c != nil {
			c.clearPO(poName)
		}
	}
	return out
}

func handoffObs(h []poauditor.Handoff) []staffops.POAuditorHandoffObs {
	if len(h) == 0 {
		return nil
	}
	out := make([]staffops.POAuditorHandoffObs, 0, len(h))
	for _, x := range h {
		out = append(out, staffops.POAuditorHandoffObs{Agent: x.Agent, Target: x.Target})
	}
	return out
}

func findAuditorChild(defs []claudia.AgentDef, poName string, seatRole func(claudia.AgentDef) string) string {
	for _, d := range defs {
		if strings.TrimSpace(d.Parent) != poName {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(seatRole(d)), roles.Auditor) {
			return strings.TrimSpace(d.Name)
		}
	}
	return ""
}

func sendQueueDepth(s *Server, name string) int {
	if s == nil {
		return 0
	}
	q := s.sendQueue()
	if q == nil {
		return 0
	}
	entries, err := q.Snapshot(name)
	if err != nil {
		return 0
	}
	return len(entries)
}

func pendingPOHandoffs(stateDir, workdir string, defs []claudia.AgentDef, poName string, running func(string) bool) []poauditor.Handoff {
	if strings.TrimSpace(stateDir) == "" {
		return nil
	}
	var out []poauditor.Handoff
	for _, d := range defs {
		if strings.TrimSpace(d.Parent) != poName {
			continue
		}
		purpose := strings.TrimSpace(d.Purpose)
		if purpose == "" {
			purpose = claudia.PurposeWork
		}
		if purpose != claudia.PurposeWork {
			continue
		}
		name := strings.TrimSpace(d.Name)
		if name == "" {
			continue
		}
		target := strings.TrimSpace(d.TargetID)
		if target == "" {
			continue
		}
		rec, err := agentreport.Latest(stateDir, name)
		if err != nil || strings.TrimSpace(rec.Text) == "" {
			continue
		}
		if !storedReportLooksFinished(rec.Text) {
			continue
		}
		if workdir != "" {
			if st, ok := targetfile.LoadTargetStatusFromCwd(workdir, target); ok && !targetStillOpen(st) {
				continue
			}
		}
		// Child may be idle-running or stopped after finish; both are handoffs.
		if running != nil && !running(name) {
			// Still registered with a stored finish on an open target counts.
		}
		out = append(out, poauditor.Handoff{Agent: name, Target: target})
	}
	return out
}

func targetStillOpen(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "achieved", "set_aside", "retired", "done":
		return false
	default:
		return true
	}
}

// activityPhaseForStall prefers the idle tracker when seat authority is not
// wired (hermetic sentinel fixtures) so phase=idle is visible to 🎯T1192.
func (s *Server) activityPhaseForStall(name string) string {
	if s != nil && s.idleActivity != nil {
		s.idleActivity.mu.Lock()
		a, ok := s.idleActivity.by[name]
		s.idleActivity.mu.Unlock()
		if ok {
			if p := strings.TrimSpace(a.Phase); p != "" {
				return p
			}
		}
	}
	if s == nil {
		return ""
	}
	return s.seatState(name).Phase.String()
}

func trackPOAuditorGrace(rt *sentinelRuntime, poName, phase string, alive bool, now time.Time, grace time.Duration) bool {
	sym := poAuditorMutualWaitSymptom(poName)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !alive || !poFanoutPhaseIdle(phase) {
		delete(rt.firstSeen, sym)
		return false
	}
	first, seen := rt.firstSeen[sym]
	if !seen {
		rt.firstSeen[sym] = now
		first = now
	}
	return now.Sub(first) >= grace
}
