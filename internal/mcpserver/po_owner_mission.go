// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/poownermission"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/staffops"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// AgentStatusPOOwnerMission is the typed fault on agent_list and GET /api/agents
// when 🎯T1195 fires.
const AgentStatusPOOwnerMission = "po_owner_mission"

const poOwnerMissionSymptomPrefix = "po_owner_mission:"

func poOwnerMissionSymptom(poName string) string {
	return poOwnerMissionSymptomPrefix + strings.TrimSpace(poName)
}

type poOwnerMissionFaultSnap struct {
	PO     string
	Mission poownermission.Mission
	Detail string
	At     time.Time
}

type poOwnerMissionFaultCache struct {
	mu   sync.RWMutex
	byPO map[string]poOwnerMissionFaultSnap
}

func (c *poOwnerMissionFaultCache) set(snap poOwnerMissionFaultSnap) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.byPO == nil {
		c.byPO = make(map[string]poOwnerMissionFaultSnap)
	}
	c.byPO[snap.PO] = snap
	c.mu.Unlock()
}

func (c *poOwnerMissionFaultCache) clearPO(name string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.byPO != nil {
		delete(c.byPO, name)
	}
	c.mu.Unlock()
}

func (c *poOwnerMissionFaultCache) line(poName string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.RLock()
	snap, ok := c.byPO[poName]
	c.mu.RUnlock()
	if !ok || strings.TrimSpace(snap.Mission.RequestID) == "" {
		return "", false
	}
	body := poownermission.FormatWakeBody(snap.Mission, "")
	if body == "" {
		body = snap.Detail
	}
	return fmt.Sprintf("po_owner_mission: %s", body), true
}

func (s *Server) ensurePOOwnerMissionFaultCache() *poOwnerMissionFaultCache {
	if s == nil {
		return nil
	}
	if s.poOwnerMissionFaults == nil {
		s.poOwnerMissionFaults = &poOwnerMissionFaultCache{}
	}
	return s.poOwnerMissionFaults
}

// POOwnerMissionFaultLine is the /api/agents and agent_list annotation.
func (s *Server) POOwnerMissionFaultLine(poName string) (string, bool) {
	c := s.ensurePOOwnerMissionFaultCache()
	if c == nil {
		return "", false
	}
	return c.line(poName)
}

func (s *Server) maybeRecordPOOwnerMission(poName, text, requestID string, origin SendOrigin) {
	if s == nil || origin != OriginOwner || strings.TrimSpace(poName) == "" {
		return
	}
	if s.registry == nil {
		return
	}
	def := s.registry.Def(poName)
	if def == nil || !purposeIsProductOwner(s.seatRoleOf(*def)) {
		return
	}
	br, ok := poownermission.ParseBoundedRequest(text, requestID)
	if !ok {
		return
	}
	dir := s.agentReportStateDir()
	if dir == "" {
		return
	}
	store, err := poownermission.OpenStore(dir)
	if err != nil {
		return
	}
	now := s.deliveryNow().UTC()
	if _, err := store.Open(poName, br, now); err != nil {
		return
	}
	s.logLifecycle(compAgentLifecycle, "po_owner_mission_open", "info", map[string]any{
		"po": poName, "request_id": br.RequestID, "items": len(br.Items), "item_count": br.ItemCount,
	})
}

func (s *Server) closePOOwnerMissionIfTerminal(poName, text string) {
	if s == nil || !poownermission.ClosesMission(text) {
		return
	}
	dir := s.agentReportStateDir()
	if dir == "" {
		return
	}
	store, err := poownermission.OpenStore(dir)
	if err != nil {
		return
	}
	if err := store.Close(poName); err != nil {
		return
	}
	s.ensurePOOwnerMissionFaultCache().clearPO(poName)
	s.logLifecycle(compAgentLifecycle, "po_owner_mission_close", "info", map[string]any{"po": poName})
}

func trackPOOwnerMissionGrace(rt *sentinelRuntime, poName, phase string, alive bool, now time.Time, grace time.Duration) bool {
	sym := poOwnerMissionSymptom(poName)
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

func (s *Server) samplePOOwnerMissions(rt *sentinelRuntime, overseer, workdir string, now time.Time, grace time.Duration, running func(string) bool) []staffops.POOwnerMissionObs {
	if s == nil || s.registry == nil || rt == nil {
		return nil
	}
	dir := s.agentReportStateDir()
	if dir == "" {
		return nil
	}
	store, err := poownermission.OpenStore(dir)
	if err != nil {
		return nil
	}
	open, err := store.ListOpen()
	if err != nil || len(open) == 0 {
		return nil
	}
	if running == nil {
		running = func(name string) bool {
			proc := s.registry.Get(name)
			return proc != nil && s.seatState(name).Alive == seatstate.Yes
		}
	}
	wantLedger := targetfile.LedgerKey(workdir)
	var out []staffops.POOwnerMissionObs
	for _, m := range open {
		poName := strings.TrimSpace(m.PO)
		if poName == "" || poName == overseer {
			continue
		}
		def := s.registry.Def(poName)
		if def == nil || !purposeIsProductOwner(s.seatRoleOf(*def)) {
			continue
		}
		if workdir != "" && !targetfile.SameLedger(wantLedger, targetfile.LedgerKey(def.WorkDir)) {
			continue
		}
		alive := running(poName)
		phase := s.activityPhaseForStall(poName)
		graceElapsed := trackPOOwnerMissionGrace(rt, poName, phase, alive, now, grace)
		engaged := CountEngagedWorkChildren(s.registry.List(), poName, s.activityPhaseForStall, nil)
		poObs := poownermission.POObs{
			Name: poName, Alive: alive, Phase: phase, GraceElapsed: graceElapsed,
			EngagedChild: engaged, LastTurnEnded: s.poLastTurnEnded(poName),
		}
		mission := m
		verdict, detail := poownermission.Classify(poObs, &mission, workdir)
		if poownermission.IsFault(verdict) && !ownerMissionWakeDue(mission, now) {
			verdict = poownermission.VerdictWithinGrace
		}
		if poownermission.IsFault(verdict) {
			obs := staffops.POOwnerMissionObs{
				PO: poName, RequestID: mission.RequestID,
				Remaining: poownermission.RemainingCount(workdir, mission),
				Total:     len(mission.Items),
				ItemCount: mission.ItemCount,
				Detail:    detail,
			}
			out = append(out, obs)
			s.ensurePOOwnerMissionFaultCache().set(poOwnerMissionFaultSnap{
				PO: poName, Mission: mission, Detail: detail, At: now,
			})
			s.logLifecycle(compSentinel, "po_owner_mission", "error", map[string]any{
				"po": poName, "request_id": mission.RequestID, "detail": detail,
			})
		} else if c := s.ensurePOOwnerMissionFaultCache(); c != nil {
			c.clearPO(poName)
		}
	}
	rt.mu.Lock()
	rt.poOwnerMission = out
	rt.mu.Unlock()
	return out
}

func ownerMissionWakeDue(m poownermission.Mission, now time.Time) bool {
	if m.LastWake.IsZero() {
		return true
	}
	return now.Sub(m.LastWake) >= staffops.DefaultCooldown
}

func (s *Server) tickPOOwnerMissionWake(rt *sentinelRuntime, overseer, workdir string, now time.Time, grace time.Duration, dryRun bool, running func(string) bool) {
	if s == nil || rt == nil || dryRun {
		return
	}
	rt.mu.Lock()
	faults := rt.poOwnerMission
	rt.mu.Unlock()
	dir := s.agentReportStateDir()
	if dir == "" {
		return
	}
	store, err := poownermission.OpenStore(dir)
	if err != nil {
		return
	}
	for _, f := range faults {
		poName := strings.TrimSpace(f.PO)
		if poName == "" {
			continue
		}
		m, ok, err := store.ForPO(poName)
		if err != nil || !ok {
			continue
		}
		if !ownerMissionWakeDue(m, now) {
			continue
		}
		body := poownermission.FormatWakeBody(m, workdir)
		if body == "" {
			continue
		}
		msg := "[daemon wake 🎯T1195] Open owner mission — continue until a typed finish or blocked report:\n" + body
		if err := s.deliverSentinel(poName, staffops.SentinelEventSource, msg); err != nil {
			s.logLifecycle(compSentinel, "po_owner_mission", "error", map[string]any{
				"po": poName, "err": err.Error(),
			})
			continue
		}
		_ = store.NoteWake(poName, now)
	}
}
