// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/thread"
)

// SweepPlanPolicy migrates or parks running non-overseer seats whose
// weekly window is hot or exhausted (🎯T390.1.5). Dest empty → park.
// Product owners (parent = overseer) are exempt (🎯T517).
func (s *Server) SweepPlanPolicy() []planusage.PlanAction {
	if s == nil {
		return nil
	}
	snap, _, now, th, ok := s.planPolicyInputs()
	if !ok {
		return nil
	}
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
	s.dropExemptHandovers(agents)
	acts := planusage.PlanActions(snap, agents, now, th)
	for _, a := range acts {
		if a.To != "" && s.migrator != nil {
			pending, err := s.migrator.PrepareMigration(a.Name, claudia.Provider(a.To), true)
			if err != nil {
				slog.Warn("plan policy migrate prepare failed", "name", a.Name, "to", a.To, "err", err)
				s.MarkAgentParked(a.Name, "jevons", a.Reason+": migrate failed, parked")
				continue
			}
			if _, err := s.migrator.CompleteThinBrief(pending); err != nil {
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
		if s.registry != nil {
			s.registry.Stop(a.Name)
		}
		slog.Info("plan policy parked", "name", a.Name, "from", a.From)
	}
	return acts
}

// dropExemptHandovers clears a pending claude→codex (or any) seed aimed
// at a control-plane seat so T418 does not keep retrying a migrate the
// policy is no longer allowed to start (🎯T517).
func (s *Server) dropExemptHandovers(agents []planusage.AgentRef) {
	if s == nil {
		return
	}
	led, ok := s.migrator.(handoverLedger)
	if !ok || led == nil {
		return
	}
	pending, err := led.PendingHandovers()
	if err != nil && len(pending) == 0 {
		return
	}
	overseers := planusage.OverseerNames(agents)
	byName := map[string]planusage.AgentRef{}
	for _, a := range agents {
		byName[a.Name] = a
	}
	for _, p := range pending {
		ref, known := byName[p.Agent]
		if !known {
			continue
		}
		if !planusage.PlanMigrateExempt(ref, overseers) {
			continue
		}
		if err := led.ClearHandover(p.Agent); err != nil {
			slog.Warn("plan policy could not drop control-plane handover", "name", p.Agent, "err", err)
			continue
		}
		slog.Info("plan policy dropped control-plane handover", "name", p.Agent)
	}
}
