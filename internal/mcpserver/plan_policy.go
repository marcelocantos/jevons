// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"github.com/marcelocantos/jevons/internal/seatstop"
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/thread"
)

// SweepPlanPolicy migrates or parks every running seat whose own
// provider is weekly-hot or exhausted (🎯T390.1.5, 🎯T850). The
// overseer and stratum-1 product owners are included on that same
// rule. Dest empty → park. Aside seats stay out (🎯T543).
func (s *Server) SweepPlanPolicy() []planusage.PlanAction {
	if s == nil {
		return nil
	}
	snap, _, now, th, ok := s.planPolicyInputs()
	if !ok {
		return nil
	}
	stayed := map[string]bool{}
	var agents []planusage.AgentRef
	if s.registry != nil {
		for _, d := range s.registry.List() {
			agents = append(agents, planusage.AgentRef{
				Name:     d.Name,
				Provider: string(d.Provider),
				Purpose:  d.Purpose,
				Parent:   d.Parent,
			})
		}
	}
	pending := s.pendingPlanHandovers()
	acts := planusage.PlanActions(snap, agents, now, th)
	for i, a := range acts {
		if a.To != "" && !s.planDestAllowed(a.To) {
			slog.Info("🎯T542 plan dest refused by owner-provider pin",
				"name", a.Name, "to", a.To, "pin", s.resolvedDefaultProvider())
			a.To = ""
			a.Reason += "; dest refused by owner-provider pin — park"
			acts[i] = a
		}
		if a.To != "" && s.migrator != nil {
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
			prepared, err := s.migrator.PrepareMigration(a.Name, claudia.Provider(a.To), true)
			if err != nil {
				slog.Warn("plan policy migrate prepare failed", "name", a.Name, "to", a.To, "err", err)
				s.MarkAgentParked(a.Name, "jevons", a.Reason+": migrate failed, parked")
				s.noteSeatStop(a.Name, seatstop.SourcePlanPolicy, "plan policy parked: "+(a.Reason+": migrate failed, parked"), "jevons", "")
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

func hotNames(acts []planusage.PlanAction) map[string]bool {
	hot := map[string]bool{}
	for _, a := range acts {
		hot[a.Name] = true
	}
	return hot
}

// planDestAllowed reports whether plan-usage migrate may land on dest.
// A standing no-Claude / owner-provider pin (config.yaml provider /
// SetDefaultProvider) is not overwritten by picking Claude just because
// another backend is hot (🎯T542).
func (s *Server) planDestAllowed(dest string) bool {
	d := strings.ToLower(strings.TrimSpace(dest))
	if d == "" || d != "claude" {
		return true
	}
	pin := strings.ToLower(string(s.resolvedDefaultProvider()))
	return pin == "claude"
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
