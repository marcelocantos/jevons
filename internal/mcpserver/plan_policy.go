// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/seatstop"
	"github.com/marcelocantos/jevons/internal/thread"
)

// SweepPlanPolicy migrates or parks every running seat whose own
// provider is weekly-hot or exhausted (🎯T390.1.5, 🎯T850). The
// overseer and stratum-1 product owners are included on that same
// rule. A host park ban defers Claudia's park verdict; an incomplete feed
// also defers it.
// Aside seats stay out (🎯T543).
func (s *Server) SweepPlanPolicy() []planusage.PlanAction {
	if s == nil {
		return nil
	}
	// A failed destination launch has already changed Claudia's registry row.
	// Retry its persisted handover before reading plan usage: the old hot
	// provider is no longer on that row, and a stale feed must not strand it.
	resuming := s.resumeClaudiaMigrations()
	snap, cands, now, th, ok := s.planPolicyInputs()
	if !ok {
		s.rememberPlanResults(resuming)
		return resuming
	}
	stayed := map[string]bool{}
	pending := s.pendingPlanHandovers()
	acts := resuming
	resumingNames := make(map[string]bool, len(resuming))
	for _, action := range resuming {
		resumingNames[action.Name] = true
	}
	for _, action := range planusage.PlanActions(snap, s.planPolicyAgents(), now, th, cands...) {
		if !resumingNames[action.Name] {
			acts = append(acts, action)
		}
	}
	for i := range acts {
		a := &acts[i]
		if resumingNames[a.Name] {
			continue // Claudia already retried this destination in this sweep.
		}
		if a.To != "" {
			if p, ok := pending[a.Name]; ok && p.Usable() {
				a.Execution = "pending"
				continue
			}
		}
		if reason := s.planHostDeferral(*a); reason != "" {
			a.Execution, a.Failure = "deferred", reason
			continue
		}
		if a.To != "" {
			if s.migrator == nil {
				slog.Warn("plan policy migration unavailable", "name", a.Name, "to", a.To, "reason", "migrator not configured")
				a.Execution, a.Failure = "deferred", "migrator not configured"
				continue
			}
			// PrepareMigration persists the handover before CompleteThinBrief.
			// If launch then fails, the next policy tick must leave that durable
			// handover alone: completing it again can mint another throwaway
			// compact session every refresh (🎯T543). A COLD leftover is not
			// "already pending" — reap it rather than skip (🎯T542).
			if p, ok := pending[a.Name]; ok {
				if !p.Usable() {
					// A COLD leftover is not a handover to retry. Drop it
					// and prepare once under the seat's current host policy.
					s.clearPlanHandover(a.Name)
				} else {
					slog.Info("plan policy migration already pending", "name", a.Name, "to", a.To)
					a.Execution = "pending"
					continue
				}
			}
			var prepared handover.Pending
			var err error
			allowInterrupt := false
			if def := s.registry.Def(a.Name); def != nil {
				allowInterrupt = def.HostMayInterrupt
			}
			if p, ok := s.migrator.(migratePinner); ok {
				prepared, err = p.PrepareMigrationPinned(a.Name, claudia.Provider(a.To), a.Model, allowInterrupt)
			} else {
				prepared, err = s.migrator.PrepareMigration(a.Name, claudia.Provider(a.To), allowInterrupt)
			}
			if err != nil {
				if !allowInterrupt && isPromptInFlight(err) {
					a.Execution, a.Failure = "deferred", "turn in flight; Jevons host policy forbids interruption"
					continue
				}
				if def := s.registry.Def(a.Name); def != nil && def.MigrationSeed != "" {
					// Claudia has already saved the destination and brief. The
					// next sweep retries that state; this is not a failed move.
					a.Execution, a.Failure = "pending", err.Error()
					continue
				}
				slog.Warn("plan policy migrate prepare failed", "name", a.Name, "to", a.To, "err", err)
				a.Execution, a.Failure = "failed", err.Error()
				// The old seat is still the only working seat when preparation
				// fails. Leave it eligible and retry on the next policy tick;
				// parking here turns a transient transfer failure into an outage.
				continue
			}
			if prepared.Remap == handover.RemapClaudiaMigrate {
				// Claudia has already moved this live seat and delivered its inert
				// continuation. Delivered=true makes Usable false; treating that
				// as a cold rotate would launch a second session on the dest.
				s.MarkAgentWorking(a.Name, "jevons", a.Reason+": Claudia migration complete")
				a.Execution = "migrated"
				stayed[a.Name] = true
				continue
			}
			if !prepared.Usable() {
				// The row is already on the destination. There is nothing
				// to seed. Parking here is what left the product owners
				// stopped after a hot-week move (2026-09-22).
				if err := s.finishColdPlanMigrate(*a); err != nil {
					a.Execution, a.Failure = "pending", err.Error()
				} else {
					a.Execution = "migrated"
				}
				stayed[a.Name] = true
				continue
			}
			if _, err := s.migrator.CompleteThinBrief(prepared); err != nil {
				slog.Warn("plan policy migrate brief failed", "name", a.Name, "err", err)
			}
			if err := s.migrator.Launch(&thread.Thread{ID: a.Name}); err != nil {
				slog.Warn("plan policy migrate launch failed; handover pending", "name", a.Name, "err", err)
				a.Execution, a.Failure = "pending", err.Error()
			} else if _, seeded, serr := s.migrator.SeedSuccessor(a.Name); serr != nil {
				slog.Warn("plan policy migrate seed failed", "name", a.Name, "err", serr)
				a.Execution, a.Failure = "pending", serr.Error()
			} else if !seeded {
				a.Execution, a.Failure = "pending", "successor seed not confirmed"
			} else {
				a.Execution = "migrated"
			}
			slog.Info("plan policy migration step", "name", a.Name, "from", a.From, "to", a.To,
				"execution", a.Execution, "failure", a.Failure)
			continue
		}
		s.MarkAgentParked(a.Name, "jevons", a.Reason)
		a.Execution = "parked"
		s.noteSeatStop(a.Name, seatstop.SourcePlanPolicy, "plan policy parked: "+(a.Reason), "jevons", "")
		if s.registry != nil {
			s.registry.Stop(a.Name)
		}
		slog.Info("plan policy parked", "name", a.Name, "from", a.From)
	}
	s.releaseColdSwitched(hotNames(acts), stayed)
	s.rememberPlanResults(acts)
	return acts
}

func (s *Server) resumeClaudiaMigrations() []planusage.PlanAction {
	if s.registry == nil {
		return nil
	}
	var results []planusage.PlanAction
	for _, def := range s.registry.List() {
		if def.MigrationSeed == "" {
			continue
		}
		action := pendingClaudiaMigration(def)
		if s.migrator == nil {
			action.Failure = "migrator not configured"
		} else {
			_, err := s.migrator.PrepareMigration(def.Name, def.Provider, false)
			if err != nil {
				action.Failure = err.Error()
			} else if current := s.registry.Def(def.Name); current != nil && current.MigrationSeed == "" {
				action.Execution, action.Failure = "migrated", ""
			} else {
				action.Failure = "Claudia has not confirmed handover delivery"
			}
		}
		results = append(results, action)
	}
	return results
}

func pendingClaudiaMigration(def claudia.AgentDef) planusage.PlanAction {
	return planusage.PlanAction{
		Name: def.Name, From: string(def.MigrationFrom), To: string(def.Provider), Model: def.Model,
		Action: claudia.SeatMigrate, Author: claudia.DecisionAuthor,
		Reason:    "Claudia destination persisted; handover awaiting delivery",
		Execution: "pending",
	}
}

func (s *Server) rememberPlanResults(actions []planusage.PlanAction) {
	s.planDecisionMu.Lock()
	s.planLastResults = make(map[string]planusage.PlanAction, len(actions))
	for _, action := range actions {
		s.planLastResults[action.Name] = action
	}
	s.planDecisionMu.Unlock()
}

// PlanPolicyDecisions is the read-only placement picture, including stays and
// deferrals that SweepPlanPolicy must never mistake for a park.
func (s *Server) PlanPolicyDecisions() []planusage.PlanAction {
	if s == nil {
		return nil
	}
	snap, cands, now, th, ok := s.planPolicyInputs()
	var decisions []planusage.PlanAction
	if ok {
		decisions = planusage.PlanDecisions(snap, s.planPolicyAgents(), now, th, cands...)
	}
	s.planDecisionMu.RLock()
	for i := range decisions {
		d := &decisions[i]
		last, ok := s.planLastResults[d.Name]
		if ok && last.From == d.From && last.To == d.To && last.Action == d.Action {
			d.Execution, d.Failure = last.Execution, last.Failure
		}
	}
	if s.registry != nil {
		for _, def := range s.registry.List() {
			if def.MigrationSeed == "" {
				continue
			}
			pending := pendingClaudiaMigration(def)
			if last, ok := s.planLastResults[def.Name]; ok && last.Execution == "pending" {
				pending.Failure = last.Failure
			}
			replaced := false
			for i := range decisions {
				if decisions[i].Name == def.Name {
					decisions[i] = pending
					replaced = true
					break
				}
			}
			if !replaced {
				decisions = append(decisions, pending)
			}
		}
	}
	s.planDecisionMu.RUnlock()
	pending := s.pendingPlanHandovers()
	for i := range decisions {
		d := &decisions[i]
		if p, ok := pending[d.Name]; ok && p.Kind == handover.KindMigrate && !p.Delivered &&
			d.Action == claudia.SeatMigrate {
			d.Execution = "pending"
			if d.Failure == "" {
				d.Failure = "handover awaiting delivery"
			}
		}
		if reason := s.planHostDeferral(*d); reason != "" && d.Execution == "" {
			d.Execution, d.Failure = "deferred", reason
		}
	}
	return decisions
}

// Claudia authors the placement verdict. Jevons decides whether acting on it
// may interrupt or park this particular seat, and reports a host deferral
// instead of silently changing the provider decision.
func (s *Server) planHostDeferral(action planusage.PlanAction) string {
	if s == nil || s.registry == nil {
		return ""
	}
	def := s.registry.Def(action.Name)
	if def == nil {
		return "agent is no longer registered"
	}
	inFlight := false
	if action.Action == claudia.SeatMigrate {
		if proc := s.registry.Get(action.Name); proc != nil {
			inFlight = proc.PromptInFlight()
		}
	}
	return hostPlanDeferral(action, def, inFlight)
}

func hostPlanDeferral(action planusage.PlanAction, def *claudia.AgentDef, inFlight bool) string {
	if def == nil {
		return "agent is no longer registered"
	}
	if action.Action == claudia.SeatPark && def.HostNeverPark {
		return "Jevons host policy forbids parking this seat"
	}
	if action.Action == claudia.SeatMigrate && inFlight && !def.HostMayInterrupt {
		return "turn in flight; Jevons host policy forbids interruption"
	}
	return ""
}

func (s *Server) planPolicyAgents() []planusage.AgentRef {
	if s == nil || s.registry == nil {
		return nil
	}
	var agents []planusage.AgentRef
	for _, d := range s.registry.List() {
		agents = append(agents, planusage.AgentRef{
			Name: d.Name, Provider: string(d.Provider), Purpose: d.Purpose, Parent: d.Parent,
			PreferProvider: d.PreferProvider, AllowedProviders: d.AllowedProviders,
			ExcludeProviders: d.ExcludeProviders,
		})
	}
	return agents
}

func hotNames(acts []planusage.PlanAction) map[string]bool {
	hot := map[string]bool{}
	for _, a := range acts {
		hot[a.Name] = true
	}
	return hot
}

// clearPlanHandover drops a COLD record so it cannot survive a restart
// as UNDELIVERED HANDOVER (🎯T542). It does not park.
func (s *Server) clearPlanHandover(name string) {
	led, ok := s.migrator.(handoverLedger)
	if !ok || led == nil {
		return
	}
	if err := led.ClearHandover(name); err != nil {
		slog.Warn("🎯T542 COLD handover clear failed", "name", name, "err", err)
	}
}

// finishColdPlanMigrate keeps a seat that force-rotated with no
// predecessor transcript. The destination is already on the registry
// row. Clear the empty handover and launch. Do not park.
func (s *Server) finishColdPlanMigrate(a planusage.PlanAction) error {
	s.clearPlanHandover(a.Name)
	slog.Info("plan policy cold switch stays", "name", a.Name, "from", a.From, "to", a.To)
	s.MarkAgentWorking(a.Name, "jevons", a.Reason+": cold switch, no predecessor, seat stays")
	if s.migrator == nil {
		return fmt.Errorf("migrator not configured")
	}
	if err := s.migrator.Launch(&thread.Thread{ID: a.Name}); err != nil {
		slog.Warn("plan policy cold switch launch failed", "name", a.Name, "err", err)
		return err
	}
	return nil
}

// releaseColdSwitched lifts a park that an earlier sweep wrote because a
// hot-week move had no transcript to hand over. The seat's provider is
// no longer hot, so the park is not protecting a handover.
func (s *Server) releaseColdSwitched(stillHot, justStayed map[string]bool) {
	if s == nil || s.registry == nil {
		return
	}
	snap := s.fleetIntent()
	for _, d := range s.registry.List() {
		if stillHot[d.Name] || justStayed[d.Name] {
			continue
		}
		rec, ok := snap.Agents[d.Name]
		if !ok || !coldSwitchStay(rec) {
			continue
		}
		if ag := s.registry.Get(d.Name); ag != nil && ag.Alive() {
			continue
		}
		slog.Info("plan policy lifting cold-switch park", "name", d.Name, "provider", d.Provider, "reason", rec.Reason)
		if rec.State != fleetintent.Working {
			s.MarkAgentWorking(d.Name, "jevons", "cold provider switch already landed; seat stays")
		}
		if s.migrator == nil {
			continue
		}
		if err := s.migrator.Launch(&thread.Thread{ID: d.Name}); err != nil {
			slog.Warn("plan policy cold-switch relaunch failed", "name", d.Name, "err", err)
		}
	}
}

func coldSwitchPark(reason string) bool {
	return strings.Contains(reason, "prepare returned COLD") || strings.Contains(reason, "pending record is COLD")
}

func coldSwitchStay(rec fleetintent.Record) bool {
	if rec.State == fleetintent.Parked && coldSwitchPark(rec.Reason) {
		return true
	}
	return rec.State == fleetintent.Working && strings.Contains(rec.Reason, "seat stays")
}

func (s *Server) pendingPlanHandovers() map[string]handover.Pending {
	byAgent := map[string]handover.Pending{}
	led, ok := s.migrator.(handoverLedger)
	if !ok || led == nil {
		return byAgent
	}
	pending, err := led.PendingHandovers()
	if err != nil {
		slog.Warn("plan policy pending handovers partially unreadable", "err", err)
	}
	for _, p := range pending {
		byAgent[p.Agent] = p
	}
	return byAgent
}
