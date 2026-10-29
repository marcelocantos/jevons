// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/poidlenudge"
	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T1266: a product owner must not sit idle while its child workers are
// stopped holding deliverable messages nobody ever read. Specimens:
// 2026-10-28, pigeon-po sat idle ~5h with three T66.x workers stopped and
// messages queued ~5h; claudia-po left its only T234 worker stopped after
// the owner's GO. Both blocked owner-visible jevons work until the owner
// session happened to notice. This file is the sentinel's reading: it
// projects registry + seat-queue state into [poidlenudge.POObs] and hands
// the classification to the pure package; the daemon only acts on the
// verdict (nudge the PO once, then one owner-visible fault if unresolved).

const (
	// poIdleNudgeOwnerSubject names the owner notice this feature raises.
	poIdleNudgeOwnerSubject = "po-idle"
	// poIdleNudgeOwnerKind is the NotifyOwner kind for 🎯T1266.
	poIdleNudgeOwnerKind = "po_idle_stopped_workers"
)

// poIdleNudgeStateCache holds cross-cycle poidlenudge.State per PO, keyed by
// PO name. Separate from sentinelRuntime.firstSeen because this symptom
// needs the two-timestamp State shape, not a single first-seen instant.
type poIdleNudgeStateCache struct {
	mu sync.Mutex
	by map[string]poidlenudge.State
}

func (c *poIdleNudgeStateCache) get(name string) poidlenudge.State {
	if c == nil {
		return poidlenudge.State{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.by[name]
}

func (c *poIdleNudgeStateCache) set(name string, st poidlenudge.State) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.by == nil {
		c.by = make(map[string]poidlenudge.State)
	}
	c.by[name] = st
	c.mu.Unlock()
}

func (s *Server) ensurePOIdleNudgeState() *poIdleNudgeStateCache {
	if s == nil {
		return nil
	}
	if s.poIdleNudgeState == nil {
		s.poIdleNudgeState = &poIdleNudgeStateCache{}
	}
	return s.poIdleNudgeState
}

// poIdleNudgeCapacityCritical reads the capacity governor's pressure
// assessment (🎯T1266 acceptance: respect capacity pressure — deferred, not
// dropped, under critical). A missing governor is not critical.
func (s *Server) poIdleNudgeCapacityCritical() bool {
	gov := s.CapacityGovernor()
	if gov == nil {
		return false
	}
	return gov.Status().Assessment.Pressure >= capacity.PressureCritical
}

// samplePOIdleNudgeStoppedWorkers builds the per-PO stopped-worker
// observation (the queued-message half of 🎯T1266) from the registry and
// seat queue state: this PO's work children that are stopped (not alive)
// while still holding an undelivered message.
func (s *Server) poIdleNudgeStoppedWorkers(poName, workdir string) []poidlenudge.StoppedWorkerObs {
	if s == nil || s.registry == nil {
		return nil
	}
	wantLedger := targetfile.LedgerKey(workdir)
	var out []poidlenudge.StoppedWorkerObs
	now := s.deliveryNow().UTC()
	for _, seat := range s.childSeatsForPO(poName, workdir) {
		def := s.registry.Def(seat.agent)
		if def == nil {
			continue
		}
		if workdir != "" && !targetfile.SameLedger(wantLedger, targetfile.LedgerKey(def.WorkDir)) {
			continue
		}
		st := s.seatState(seat.agent)
		if st.Alive == seatstate.Yes || st.QueueDepth <= 0 {
			continue
		}
		out = append(out, poidlenudge.StoppedWorkerObs{
			Agent: seat.agent, Target: seat.target, QueuedAge: st.QueueOldestAge(now),
		})
	}
	return out
}


// poIdleNudgeOpenQuestionTargets returns the set of target IDs on this
// ledger carrying an open owner question (🎯T1266 acceptance: never fire
// for a target waiting on the owner). Best-effort: a store error yields an
// empty set rather than blocking the rest of the cycle.
func (s *Server) poIdleNudgeOpenQuestionTargets(workdir string) map[string]bool {
	out := map[string]bool{}
	st, err := s.ownerQuestionStore()
	if err != nil || st == nil {
		return out
	}
	rows, err := st.List(true)
	if err != nil {
		return out
	}
	wantLedger := targetfile.LedgerKey(workdir)
	for _, q := range rows {
		if q.Identity.Repo != "" && workdir != "" &&
			!targetfile.SameLedger(wantLedger, targetfile.LedgerKey(q.Identity.Repo)) {
			continue
		}
		if t := strings.TrimSpace(q.Identity.Target); t != "" {
			out[t] = true
		}
	}
	return out
}

// poIdleNudgeStalledTargets returns the open, unblocked, unparked frontier
// leaves on this ledger with nobody engaged (the first 🎯T1266 OR-branch).
//
// Readiness is decided by [poproactive.ClassifyLeaf] against the exact same
// [poproactive.LeafObs] frontier_consume.go builds via frontierLeafObs — not
// a hand-rolled subset of its exclusions. That single source of truth
// already covers ParkedAncestors (🎯T262.5 parked-for-design ancestor),
// ActiveChildren (🎯T338 umbrella parent with open children), SetAsideDeps,
// OwnedBy (🎯T449 owner taste gate), high-infra/mobile/deferred tags, and
// engagement — a target this sentinel treats as "stalled" is exactly one
// consume itself would spawn against. Only LeafReady counts; every other
// LeafKind (including LeafSkipEngaged) is excluded. An open owner question
// is not a bullseye-tracked field, so it is excluded separately.
func (s *Server) poIdleNudgeStalledTargets(workdir string) []poidlenudge.TargetObs {
	if s == nil || s.registry == nil || workdir == "" {
		return nil
	}
	leaves, err := loadFrontierLeaves(workdir)
	if err != nil {
		return nil
	}
	openQuestions := s.poIdleNudgeOpenQuestionTargets(workdir)
	out := make([]poidlenudge.TargetObs, 0, len(leaves))
	for _, leaf := range leaves {
		engaged := len(workAgentsBoundOnTarget(s.registry, leaf.ID, workdir, "")) > 0
		obs := frontierLeafObs(leaf, engaged)
		if poproactive.ClassifyLeaf(obs) != poproactive.LeafReady {
			// Blocked, parked ancestor, active children, owner-assigned,
			// set-aside dep, high-infra/deferred, or already engaged — not
			// a stall this sentinel should narrate.
			continue
		}
		out = append(out, poidlenudge.TargetObs{
			ID:            leaf.ID,
			OwnerQuestion: openQuestions[leaf.ID],
		})
	}
	return out
}

// tickPOIdleNudge classifies and acts on 🎯T1266 for every product owner on
// this ledger: nudge once, escalate to one owner-visible fault if the
// symptom is still live after the fault grace.
func (s *Server) tickPOIdleNudge(overseer, workdir string, now time.Time, queueThreshold, faultGrace time.Duration, dryRun bool, running func(string) bool) {
	if s == nil || s.registry == nil || dryRun {
		return
	}
	if running == nil {
		running = func(name string) bool {
			proc := s.registry.Get(name)
			return proc != nil && s.seatState(name).Alive == seatstate.Yes
		}
	}
	cache := s.ensurePOIdleNudgeState()
	critical := s.poIdleNudgeCapacityCritical()
	wantLedger := targetfile.LedgerKey(workdir)
	stalledTargets := s.poIdleNudgeStalledTargets(workdir)

	for _, d := range s.registry.List() {
		poName := strings.TrimSpace(d.Name)
		if poName == "" || poName == overseer || !purposeIsProductOwner(s.seatRoleOf(d)) {
			continue
		}
		if workdir != "" && !targetfile.SameLedger(wantLedger, targetfile.LedgerKey(d.WorkDir)) {
			continue
		}
		alive := running(poName)
		idle := poFanoutPhaseIdle(s.activityPhaseForStall(poName))
		stopped := s.poIdleNudgeStoppedWorkers(poName, workdir)
		obs := poidlenudge.POObs{
			Name: poName, Alive: alive, Idle: idle,
			Targets:        stalledTargets,
			StoppedWorkers: stopped, CapacityCritical: critical,
		}
		st := cache.get(poName)
		res, newSt := poidlenudge.Classify(obs, st, now, queueThreshold, faultGrace)
		cache.set(poName, newSt)

		switch res.Verdict {
		case poidlenudge.VerdictNudge:
			msg := fmt.Sprintf(
				"[daemon wake 🎯T1266] Idle with open work unengaged — resume, reroute, or reap:\n%s",
				poIdleNudgeBody(res))
			if err := s.deliverSentinel(poName, "sentinel", msg); err != nil {
				s.logLifecycle(compSentinel, "po_idle_nudge", "error", map[string]any{
					"po": poName, "err": err.Error(),
				})
			} else {
				s.logLifecycle(compSentinel, "po_idle_nudge", "ok", map[string]any{
					"po": poName, "stopped": res.StoppedAgents,
				})
			}
		case poidlenudge.VerdictFault:
			if s.ownerNotifier != nil {
				text := fmt.Sprintf(
					"⚠️ %s has been idle while open work sits unengaged: %s. Nudged once; unresolved.",
					poName, poIdleNudgeBody(res))
				s.ownerNotifier.NotifyOwner(poIdleNudgeOwnerSubject, poIdleNudgeOwnerKind, text)
			}
			s.logLifecycle(compSentinel, "po_idle_nudge", "error", map[string]any{
				"po": poName, "stopped": res.StoppedAgents, "escalated": true,
			})
		}
	}
}

// poIdleNudgeBody renders the stalled target and stopped-worker lists for
// the nudge / fault text.
func poIdleNudgeBody(res poidlenudge.Result) string {
	var parts []string
	if len(res.StalledTargets) > 0 {
		parts = append(parts, "open unblocked targets with nobody engaged: "+strings.Join(res.StalledTargets, ", "))
	}
	if len(res.StoppedAgents) > 0 {
		parts = append(parts, "stopped workers holding undelivered messages: "+strings.Join(res.StoppedAgents, ", "))
	}
	return strings.Join(parts, "; ")
}
