// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/poqueue"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/staffops"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// AgentStatusPOQueueBacklog is the typed fault on agent_list and GET /api/agents
// when 🎯T1182 queue-age budget is exceeded (per 🎯T713 reachability).
const AgentStatusPOQueueBacklog = "po_queue_backlog"

type poQueueBacklogFaultSnap struct {
	PO        string
	Depth     int
	OldestAge time.Duration
	Detail    string
	At        time.Time
}

type poQueueBacklogFaultCache struct {
	mu    sync.RWMutex
	byPO  map[string]poQueueBacklogFaultSnap
	clear map[string]struct{}
}

func (c *poQueueBacklogFaultCache) set(snap poQueueBacklogFaultSnap) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.byPO == nil {
		c.byPO = make(map[string]poQueueBacklogFaultSnap)
	}
	c.byPO[snap.PO] = snap
	delete(c.clear, snap.PO)
	c.mu.Unlock()
}

func (c *poQueueBacklogFaultCache) clearPO(name string) {
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

func (c *poQueueBacklogFaultCache) line(poName string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.RLock()
	snap, ok := c.byPO[poName]
	c.mu.RUnlock()
	if !ok || snap.Depth <= 0 {
		return "", false
	}
	return fmt.Sprintf("po_queue_backlog: %d queued, oldest waiting %s — delegate verification to auditor; do not run gates in PO turns (🎯T1182)",
		snap.Depth, snap.OldestAge.Round(time.Second)), true
}

func poQueueBacklogSymptom(poName string) string {
	return "po_queue_backlog:" + strings.TrimSpace(poName)
}

func (s *Server) ensurePOQueueBacklogFaultCache() *poQueueBacklogFaultCache {
	if s == nil {
		return nil
	}
	if s.poQueueBacklogFaults == nil {
		s.poQueueBacklogFaults = &poQueueBacklogFaultCache{}
	}
	return s.poQueueBacklogFaults
}

// POQueueBacklogFaultLine is the /api/agents and agent_list annotation.
func (s *Server) POQueueBacklogFaultLine(poName string) (string, bool) {
	c := s.ensurePOQueueBacklogFaultCache()
	if c == nil {
		return "", false
	}
	return c.line(poName)
}

func (s *Server) samplePOQueueBacklog(rt *sentinelRuntime, overseer, workdir string, now time.Time, grace time.Duration, running func(string) bool) []staffops.POQueueBacklogObs {
	if s == nil || s.registry == nil || rt == nil {
		return nil
	}
	if running == nil {
		running = func(name string) bool {
			proc := s.registry.Get(name)
			return proc != nil && s.seatState(name).Alive == seatstate.Yes
		}
	}
	wantLedger := targetfile.LedgerKey(workdir)
	var out []staffops.POQueueBacklogObs
	for _, d := range s.registry.List() {
		poName := strings.TrimSpace(d.Name)
		if poName == "" || poName == overseer || !purposeIsProductOwner(s.seatRoleOf(d)) {
			continue
		}
		if !targetfile.SameLedger(wantLedger, targetfile.LedgerKey(d.WorkDir)) {
			continue
		}
		st := s.seatState(poName)
		if st.QueueDepth <= 0 {
			if c := s.ensurePOQueueBacklogFaultCache(); c != nil {
				c.clearPO(poName)
			}
			continue
		}
		oldestAge := st.QueueOldestAge(now)
		inFlight := st.InFlight == seatstate.Yes
		phase := s.activityPhaseForStall(poName)
		graceElapsed := trackPOQueueBacklogGrace(rt, poName, phase, running(poName), now, grace)
		verdict, detail := poqueue.Classify(poqueue.Obs{
			Name: poName, Depth: st.QueueDepth, OldestAge: oldestAge,
			InFlight: inFlight, GraceElapsed: graceElapsed,
		})
		if poqueue.IsFault(verdict) {
			obs := staffops.POQueueBacklogObs{
				PO: poName, Depth: st.QueueDepth, OldestAge: oldestAge, Detail: detail,
			}
			out = append(out, obs)
			s.ensurePOQueueBacklogFaultCache().set(poQueueBacklogFaultSnap{
				PO: poName, Depth: st.QueueDepth, OldestAge: oldestAge, Detail: detail, At: now,
			})
			s.logLifecycle(compSentinel, "po_queue_backlog", "error", map[string]any{
				"po": poName, "depth": st.QueueDepth, "oldest": oldestAge.String(), "detail": detail,
			})
		} else if c := s.ensurePOQueueBacklogFaultCache(); c != nil {
			c.clearPO(poName)
		}
	}
	return out
}

func trackPOQueueBacklogGrace(rt *sentinelRuntime, poName, phase string, alive bool, now time.Time, grace time.Duration) bool {
	sym := poQueueBacklogSymptom(poName)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !alive || poFanoutPhaseIdle(phase) {
		delete(rt.firstSeen, sym)
		return true
	}
	first, seen := rt.firstSeen[sym]
	if !seen {
		rt.firstSeen[sym] = now
		first = now
	}
	return now.Sub(first) >= grace
}
