// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/roles"
)

// capacityRestoreActor is the stable "by" code for the fleet-wide intent
// transition NoteCapacity makes on a plan restoration (🎯T977). Distinct
// from hardBlockClear (🎯T406), which only fires on a successful provider
// call — this one fires on the plan-usage reading itself.
const capacityRestoreActor = "product:capacity_restored"

// NoteCapacity folds the current plan readings into the capacity watch and,
// when a plan has just become admissible again, tells the overseer and every
// running product owner so work paused for capacity resumes (🎯T977). It
// returns the agents told.
//
// The notice starts no process, so no fleet intent withholds it; under a
// provider hard block the turn it prompts is also the provider call whose
// success clears the block (🎯T406). Without it, a fleet that stood down for
// capacity waited for an accidental call: on 2026-10-01 it sat quiet for six
// hours with headroom on the plan.
//
// 🎯T977 (2026-10-06 recurrence): telling the overseer and POs is not enough
// on its own when the hold that stood work down is itself a FLEET-WIDE
// Parked intent recorded for capacity. planIntentDeferral checks fleet-wide
// intent first and defers every per-agent resume while it is not Working, so
// SweepPlanPolicy's own resume machinery for individually-parked seats never
// runs at all while that fleet-wide hold stands — and a fleet-wide Parked
// intent, by design (🎯T414), is never lifted by a timer or by an
// agent that happens to still be alive. This is exactly the 2026-10-05
// incident: fleet intent sat Parked for a capacity reason for almost 30
// hours after the plan it was parked for had headroom again, because
// nothing in the fleet is willing to touch a fleet-wide Parked intent
// except the owner.
//
// So, in the open, at this call site: a restoration directly clears a
// fleet-wide Parked intent IFF (a) its reason reads as capacity-related
// (fleetintent.CapacityRelatedReason) and (b) it was not recorded by the
// owner (🎯T969 — an intent the owner recorded is never reversed by product
// evidence alone, only by an owner instruction). A fleet-wide BlockedProvider
// intent is left to its existing, more conservative clearance (🎯T406:
// a successful provider call, never a reading) since that hold answers a
// different question (is the provider itself refusing, e.g. a revoked key or
// spend wall) that a restored plan reading does not settle.
func (s *Server) NoteCapacity() []string {
	if s == nil {
		return nil
	}
	snap, _, now, th, ok := s.planPolicyInputs()
	if !ok {
		return nil
	}
	restored := s.capacityWatch.Observe(snap, now, th)
	if len(restored) == 0 {
		return nil
	}
	parts := make([]string, 0, len(restored))
	for _, be := range restored {
		parts = append(parts, be.Provider+capacityHeadroom(be))
	}
	s.autoClearCapacityPark(strings.Join(parts, ", "))
	notice := "[Capacity restored] Plan headroom is back: " + strings.Join(parts, ", ") +
		". If you paused work, stood a worker down, or said you would continue once capacity allows, resume it now: re-engage the workers that were waiting. (🎯T977)"
	deliver := s.capacityDeliver
	if deliver == nil {
		deliver = func(name, text string) error {
			_, err := s.deliverByName(name, text, OriginAgent, false)
			return err
		}
	}
	told := []string{}
	for _, name := range s.capacityNoticeRecipients() {
		if err := deliver(name, notice); err != nil {
			slog.Warn("capacity-restored notice undelivered", "agent", name, "err", err)
			continue
		}
		told = append(told, name)
	}
	slog.Info("capacity restored; agents told to resume", "plans", parts, "told", told)
	return told
}

// autoClearCapacityPark lifts a fleet-wide Parked intent that was recorded
// for capacity, now that a restoration has evidenced the plan is admissible
// again. It does nothing to BlockedProvider (left to 🎯T406's own successful-
// call clearance) and does nothing to an owner-recorded Parked intent
// (🎯T969). A no-op fleet intent store (s.intent == nil) is a no-op here too.
func (s *Server) autoClearCapacityPark(plans string) {
	snap := s.fleetIntent()
	fleet := snap.Fleet
	if fleet.State != fleetintent.Parked {
		return
	}
	if strings.EqualFold(strings.TrimSpace(fleet.By), "owner") {
		return
	}
	if !fleetintent.CapacityRelatedReason(fleet.Reason) {
		return
	}
	reason := fmt.Sprintf("plan headroom restored (%s); lifting the capacity-reasoned fleet-wide park automatically recorded by %q: %s",
		plans, fleet.By, fleet.Reason)
	if err := s.SetFleetIntent(fleetintent.Working, capacityRestoreActor, reason); err != nil {
		slog.Warn("🎯T977 capacity-restored fleet-wide park clear failed", "err", err)
		return
	}
	slog.Info("🎯T977 capacity-restored fleet-wide park lifted",
		"component", "hard_block",
		"previous_by", fleet.By,
		"previous_reason", fleet.Reason,
		"plans", plans,
	)
}

// capacityHeadroom is " (session 97% left, weekly 58% left)" for a plan's
// windows that report remaining, or "".
func capacityHeadroom(be planusage.Backend) string {
	var out []string
	for _, w := range be.Windows {
		if w.RemainingPercent != nil {
			out = append(out, fmt.Sprintf("%s %.0f%% left", w.Name, *w.RemainingPercent))
		}
	}
	if len(out) == 0 {
		return ""
	}
	return " (" + strings.Join(out, ", ") + ")"
}

// capacityNoticeRecipients is the overseer and every running product owner.
// Workers are left to their product owner, who knows which were waiting.
func (s *Server) capacityNoticeRecipients() []string {
	out := []string{s.overseerName()}
	if s.registry == nil {
		return out
	}
	for _, d := range s.registry.List() {
		if d.Name == "" || d.Name == s.overseerName() {
			continue
		}
		if roles.Normalize(s.agentRole(d.Name, d.Purpose)) != roles.ProductOwner {
			continue
		}
		if p := s.registry.Get(d.Name); p == nil || !(s.seatState(d.Name).Alive == seatstate.Yes) {
			continue
		}
		out = append(out, d.Name)
	}
	return out
}
