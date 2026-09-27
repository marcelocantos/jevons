// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
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
// rule. Claudia's park verdict stops a seat; an incomplete feed defers it.
// Aside seats stay out (🎯T543).
func (s *Server) SweepPlanPolicy() []planusage.PlanAction {
	if s == nil {
		return nil
	}
	snap, cands, now, th, ok := s.planPolicyInputs()
	if !ok {
		return nil
	}
	stayed := map[string]bool{}
	pending := s.pendingPlanHandovers()
	acts := planusage.PlanActions(snap, s.planPolicyAgents(), now, th, cands...)
	for _, a := range acts {
		if a.To != "" {
			if s.migrator == nil {
				slog.Warn("plan policy migration unavailable", "name", a.Name, "to", a.To, "reason", "migrator not configured")
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
					// and prepare once: force-rotate already mints a fresh
					// session when there is no transcript.
					s.clearPlanHandover(a.Name)
				} else {
					slog.Info("plan policy migration already pending", "name", a.Name, "to", a.To)
					continue
				}
			}
			var prepared handover.Pending
			var err error
			if p, ok := s.migrator.(migratePinner); ok {
				prepared, err = p.PrepareMigrationPinned(a.Name, claudia.Provider(a.To), a.Model, true)
			} else {
				prepared, err = s.migrator.PrepareMigration(a.Name, claudia.Provider(a.To), true)
			}
			if err != nil {
				slog.Warn("plan policy migrate prepare failed", "name", a.Name, "to", a.To, "err", err)
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
				stayed[a.Name] = true
				continue
			}
			if !prepared.Usable() {
				// The row is already on the destination. There is nothing
				// to seed. Parking here is what left the product owners
				// stopped after a hot-week move (2026-09-22).
				s.finishColdPlanMigrate(a)
				stayed[a.Name] = true
				continue
			}
			if _, err := s.migrator.CompleteThinBrief(prepared); err != nil {
				slog.Warn("plan policy migrate brief failed", "name", a.Name, "err", err)
			}
			if err := s.migrator.Launch(&thread.Thread{ID: a.Name}); err != nil {
				slog.Warn("plan policy migrate launch failed; handover pending", "name", a.Name, "err", err)
			} else if _, _, serr := s.migrator.SeedSuccessor(a.Name); serr != nil {
				slog.Warn("plan policy migrate seed failed", "name", a.Name, "err", serr)
			}
			slog.Info("plan policy migrated", "name", a.Name, "from", a.From, "to", a.To)
			continue
		}
		s.MarkAgentParked(a.Name, "jevons", a.Reason)
		s.noteSeatStop(a.Name, seatstop.SourcePlanPolicy, "plan policy parked: "+(a.Reason), "jevons", "")
		if s.registry != nil {
			s.registry.Stop(a.Name)
		}
		slog.Info("plan policy parked", "name", a.Name, "from", a.From)
	}
	s.releaseColdSwitched(hotNames(acts), stayed)
	return acts
}

// PlanPolicyDecisions is the read-only placement picture, including stays and
// deferrals that SweepPlanPolicy must never mistake for a park.
func (s *Server) PlanPolicyDecisions() []planusage.PlanAction {
	if s == nil {
		return nil
	}
	snap, cands, now, th, ok := s.planPolicyInputs()
	if !ok {
		return nil
	}
	return planusage.PlanDecisions(snap, s.planPolicyAgents(), now, th, cands...)
}

func (s *Server) planPolicyAgents() []planusage.AgentRef {
	if s == nil || s.registry == nil {
		return nil
	}
	var agents []planusage.AgentRef
	for _, d := range s.registry.List() {
		agents = append(agents, planusage.AgentRef{
			Name: d.Name, Provider: string(d.Provider), Purpose: d.Purpose, Parent: d.Parent,
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
func (s *Server) finishColdPlanMigrate(a planusage.PlanAction) {
	s.clearPlanHandover(a.Name)
	slog.Info("plan policy cold switch stays", "name", a.Name, "from", a.From, "to", a.To)
	s.MarkAgentWorking(a.Name, "jevons", a.Reason+": cold switch, no predecessor, seat stays")
	if s.migrator == nil {
		return
	}
	if err := s.migrator.Launch(&thread.Thread{ID: a.Name}); err != nil {
		slog.Warn("plan policy cold switch launch failed", "name", a.Name, "err", err)
	}
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
