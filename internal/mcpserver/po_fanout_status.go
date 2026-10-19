// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/pofanout"
	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T713: the sentinel's PO-fanout reading is reachable over HTTP, not only
// delivered into the overseer's chat. T380/T586 already decide; this file
// projects every product owner's verdict (fault and non-fault) into a snapshot
// a reader with daemon HTTP reach — and no filesystem or chat access — can ask.

// FleetHealthStaleAfter is how old a cached snapshot may be before it is
// marked stale. Two sentinel intervals: a single missed tick is still fresh,
// a longer silence means the reader must not treat the last verdict as current.
const FleetHealthStaleAfter = 2 * DefaultSentinelInterval

// FleetHealthSnapshot is GET /api/fleet-health JSON (🎯T713).
type FleetHealthSnapshot struct {
	// ComputedAt is when this snapshot was produced (RFC3339 UTC).
	ComputedAt string `json:"computed_at"`
	// Stale is true when the snapshot is older than FleetHealthStaleAfter, or
	// when no sample has ever run. A stale snapshot is never "healthy".
	Stale bool `json:"stale"`
	// AgeSeconds is how long ago ComputedAt was (0 when never computed).
	AgeSeconds float64 `json:"age_seconds"`
	// Fault is true when any PO carries a fault verdict (stalled / turn_no_fanout).
	Fault bool `json:"fault"`
	// POs is every product owner scoped to the sampled ledger, with the
	// sentinel's own verdict and T586 reason vocabulary.
	POs []POHealth `json:"pos"`
}

// POHealth is one product owner's fan-out reading on the HTTP surface.
type POHealth struct {
	Name string `json:"name"`
	// Verdict is the pofanout verdict string (stalled, turn_no_fanout,
	// sleep_ok, capacity_refused, provider_exhausted, answered, working,
	// within_grace, absent).
	Verdict string `json:"verdict"`
	// Reason is the stable code behind the verdict (idle_on_ready_leaves,
	// turn_ended_zero_new_children, all_gated / empty_frontier via decision
	// reason, host_refuses_new_pane, provider_refuses_work, …).
	Reason string `json:"reason"`
	// Blocker names the true obstacle when silence is correct:
	// all_gated | capacity_refused | provider_exhausted, or empty.
	Blocker string `json:"blocker,omitempty"`
	// Fault is true for stalled / turn_no_fanout.
	Fault bool `json:"fault"`
	// ReadyIDs are the stranded ready leaves (empty unless any).
	ReadyIDs []string `json:"ready_ids,omitempty"`
	// ReadyCount is len(ReadyIDs).
	ReadyCount int `json:"ready_count"`
	// Detail is the compact evidence line.
	Detail string `json:"detail,omitempty"`
	// LiveWorkChildren is how many live work children this PO currently parents.
	LiveWorkChildren int `json:"live_work_children"`
}

// FleetHealth returns the last computed PO-fanout snapshot, marking it stale
// when older than FleetHealthStaleAfter. A never-sampled fleet returns a
// stale empty snapshot rather than inventing healthy.
func (s *Server) FleetHealth(now time.Time) FleetHealthSnapshot {
	if s == nil {
		return emptyFleetHealth()
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rt := s.ensureSentinelRuntime(0)
	rt.mu.Lock()
	snap := rt.fleetHealth
	rt.mu.Unlock()
	return finalizeFleetHealth(snap, now)
}

// emptyFleetHealth is the never-sampled shape: stale, no healthy claim.
func emptyFleetHealth() FleetHealthSnapshot {
	return FleetHealthSnapshot{
		Stale:      true,
		AgeSeconds: 0,
		Fault:      false,
		POs:        []POHealth{},
	}
}

// finalizeFleetHealth stamps age/stale onto a stored snapshot.
func finalizeFleetHealth(snap FleetHealthSnapshot, now time.Time) FleetHealthSnapshot {
	if snap.ComputedAt == "" {
		out := emptyFleetHealth()
		if snap.POs != nil {
			out.POs = snap.POs
		}
		return out
	}
	t, err := time.Parse(time.RFC3339Nano, snap.ComputedAt)
	if err != nil {
		t, err = time.Parse(time.RFC3339, snap.ComputedAt)
	}
	if err != nil {
		snap.Stale = true
		if snap.POs == nil {
			snap.POs = []POHealth{}
		}
		return snap
	}
	age := now.Sub(t)
	if age < 0 {
		age = 0
	}
	snap.AgeSeconds = age.Seconds()
	snap.Stale = age >= FleetHealthStaleAfter
	if snap.POs == nil {
		snap.POs = []POHealth{}
	}
	return snap
}

// storeFleetHealth records a freshly computed snapshot for the HTTP surface.
func (s *Server) storeFleetHealth(snap FleetHealthSnapshot) {
	if s == nil {
		return
	}
	rt := s.ensureSentinelRuntime(0)
	rt.mu.Lock()
	rt.fleetHealth = snap
	rt.mu.Unlock()
}

// fleetHealthFromResults projects pure pofanout results into the HTTP shape.
func fleetHealthFromResults(results []pofanout.Result, pos []pofanout.POObs, now time.Time) FleetHealthSnapshot {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	childrenBy := map[string]int{}
	for _, po := range pos {
		childrenBy[po.Name] = po.LiveWorkChildren
	}
	out := FleetHealthSnapshot{
		ComputedAt: now.UTC().Format(time.RFC3339Nano),
		Stale:      false,
		AgeSeconds: 0,
		POs:        make([]POHealth, 0, len(results)),
	}
	for _, r := range results {
		h := POHealth{
			Name:             r.Name,
			Verdict:          string(r.Verdict),
			Reason:           r.Reason,
			Blocker:          r.Blocker,
			Fault:            r.Fault(),
			ReadyIDs:         append([]string(nil), r.ReadyIDs...),
			ReadyCount:       len(r.ReadyIDs),
			Detail:           r.Detail,
			LiveWorkChildren: childrenBy[r.Name],
		}
		if h.Fault {
			out.Fault = true
		}
		out.POs = append(out.POs, h)
	}
	if out.POs == nil {
		out.POs = []POHealth{}
	}
	return out
}

// computePOFanoutHealth builds a full (fault + non-fault) reading of every
// product owner scoped to workdir against the same leaves the sentinel uses.
// running overrides process liveness (nil → registry Alive).
func (s *Server) computePOFanoutHealth(leaves []poproactive.LeafObs, overseer, workdir string, now time.Time, grace time.Duration, running func(string) bool) FleetHealthSnapshot {
	if s == nil || s.registry == nil {
		return emptyFleetHealth()
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if running == nil {
		running = func(name string) bool {
			proc := s.registry.Get(name)
			return proc != nil && (s.seatState(name).Alive == seatstate.Yes)
		}
	}
	rt := s.ensureSentinelRuntime(0)
	defs := s.registry.List()
	wantLedger := targetfile.LedgerKey(workdir)

	spawnRefused, spawnReason, spawnDetail := s.poFanoutSpawnGate()
	providerBlockedFleet, providerReason, providerDetail := s.poFanoutProviderGate()

	var pos []pofanout.POObs
	for _, d := range defs {
		name := strings.TrimSpace(d.Name)
		if name == "" || name == overseer || !isPOName(name) {
			continue
		}
		if !targetfile.SameLedger(wantLedger, targetfile.LedgerKey(d.WorkDir)) {
			continue
		}
		alive := running(name)
		phase := ""
		if s.idleActivity != nil {
			phase = s.seatState(name).Phase.String()
		}
		children := liveWorkChildren(defs, name, running)
		po := pofanout.POObs{
			Name:               name,
			Alive:              alive,
			Phase:              phase,
			LiveWorkChildren:   children,
			SpawnRefused:       spawnRefused,
			SpawnRefusedReason: spawnReason,
			SpawnRefusedDetail: spawnDetail,
			ProviderBlocked:    providerBlockedFleet,
			ProviderReason:     providerReason,
			ProviderDetail:     providerDetail,
		}
		if !po.ProviderBlocked {
			if dec := s.AllowFleetControl(name, fleetintent.ControlSpawn); !dec.Allow &&
				dec.Blocking == fleetintent.BlockedProvider {
				po.ProviderBlocked = true
				po.ProviderReason = dec.Reason
				po.ProviderDetail = fleetintent.Describe(dec.Blocking)
			}
		}
		po.TurnEnded, po.NewChildrenThisTurn, po.GraceElapsed = s.trackPOFanoutTurn(rt, name, phase, alive, children, now, grace)
		pos = append(pos, po)
	}
	return fleetHealthFromResults(pofanout.ClassifyAll(pos, leaves), pos, now)
}

// refreshFleetHealth recomputes and stores the snapshot from the current
// registry + ledger.
func (s *Server) refreshFleetHealth(workdir, overseer string, now time.Time, running func(string) bool) FleetHealthSnapshot {
	if s == nil {
		return emptyFleetHealth()
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if workdir == "" {
		if wd, err := os.Getwd(); err == nil {
			workdir = wd
		}
	}
	if overseer == "" {
		overseer = s.overseerName()
	}
	var leaves []poproactive.LeafObs
	if workdir != "" {
		if fl, err := loadFrontierLeaves(workdir); err == nil {
			for _, leaf := range fl {
				leaves = append(leaves, frontierLeafObs(leaf, len(workAgentsBoundOnTarget(s.registry, leaf.ID, workdir, "")) > 0))
			}
		}
	}
	snap := s.computePOFanoutHealth(leaves, overseer, workdir, now, DefaultSentinelMechanicalGrace, running)
	s.storeFleetHealth(snap)
	return snap
}

// FleetHealthJSON is the callback shape SetFleetHealthSource expects.
// Recomputes on each call so an HTTP poll does not wait on the next
// sentinel tick; ComputedAt still lets a reader judge staleness of a
// cached answer if a later path serves the store without refresh.
func (s *Server) FleetHealthJSON() any {
	if s == nil {
		return emptyFleetHealth()
	}
	workdir := s.workerWD
	if workdir == "" {
		if wd, err := os.Getwd(); err == nil {
			workdir = wd
		}
	}
	if workdir != "" {
		_, _ = os.Stat(filepath.Join(workdir, "bullseye.yaml"))
	}
	return s.refreshFleetHealth(workdir, s.overseerName(), time.Now().UTC(), nil)
}
