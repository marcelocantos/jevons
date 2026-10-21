// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"log/slog"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/seatstop"
	"github.com/marcelocantos/jevons/internal/thread"
)

// SweepPlanPolicy moves or parks every running seat whose own provider is
// weekly-hot or exhausted (🎯T390.1.5, 🎯T850). A move is an in-place model
// switch (claudia 🎯T195): the seat keeps its name, session and
// conversation. The overseer and stratum-1 product owners are included on
// that same rule. A host park ban defers Claudia's park verdict; an
// incomplete feed also defers it. Aside seats stay out (🎯T543).
func (s *Server) SweepPlanPolicy() []planusage.PlanAction {
	if s == nil {
		return nil
	}
	s.planSweepMu.Lock()
	defer s.planSweepMu.Unlock()
	snap, cands, now, th, ok := s.planPolicyInputs()
	if !ok {
		s.rememberPlanResults(nil)
		return nil
	}
	acts := planusage.PlanActions(snap, s.planPolicyAgents(), now, th, cands...)
	for i := range acts {
		a := &acts[i]
		if reason := s.planIntentDeferral(a.Name); reason != "" {
			a.Execution, a.Failure = "deferred", reason
			continue
		}
		if reason := s.planHostDeferral(*a); reason != "" {
			a.Execution, a.Failure = "deferred", reason
			continue
		}
		if a.To != "" {
			s.planSwitch(a)
			continue
		}
		s.MarkAgentParked(a.Name, planPolicyActor, a.Reason)
		a.Execution = "parked"
		// Already plan-parked and not running: do not re-note a seat_stop.
		// Re-noting stamps a fresh At every SweepPlanPolicy tick, which makes
		// T662 treat each sweep as a new MASS STOP burst and re-notify the
		// overseer under token depletion (same five POs every ~5m).
		if s.alreadyPlanParked(a.Name) {
			continue
		}
		s.noteSeatStop(a.Name, seatstop.SourcePlanPolicy, "plan policy parked: "+a.Reason, planPolicyActor, "")
		if s.registry != nil {
			s.registry.Stop(a.Name)
			seatstate.ObserveStopped(s.registry, a.Name)
		}
		slog.Info("plan policy parked", "name", a.Name, "from", a.From)
	}
	s.rememberPlanResults(acts)
	return acts
}

// planSwitch acts on a move: the seat switches to the destination in place
// and, when it was not running (a policy park lifting), launches there.
func (s *Server) planSwitch(a *planusage.PlanAction) {
	if s.switcher == nil {
		slog.Warn("plan policy switch unavailable", "name", a.Name, "to", a.To, "reason", "model switcher not configured")
		a.Execution, a.Failure = "deferred", "model switcher not configured"
		return
	}
	// A policy-parked seat may now win on its original provider: resume it.
	if a.From == a.To {
		if err := s.switcher.Launch(&thread.Thread{ID: a.Name}); err != nil {
			a.Execution, a.Failure = "pending", err.Error()
		} else if err := s.SetAgentIntent(a.Name, fleetintent.Working, planPolicyActor, a.Reason); err != nil {
			a.Execution, a.Failure = "pending", err.Error()
		} else {
			a.Execution = "resumed"
		}
		return
	}
	allowInterrupt := s.seatPlans.Get(a.Name).HostMayInterrupt
	sw, err := s.switcher.SwitchModel(a.Name, claudia.Provider(a.To), a.Model, allowInterrupt)
	switch {
	case err == nil:
	case !allowInterrupt && turnInFlight(err):
		a.Execution, a.Failure = "deferred", "turn in flight; Jevons host policy forbids interruption"
		return
	case errors.Is(err, claudia.ErrSeatNotAttached):
		// A sweep right after a daemon restart: the broker runs the seat but
		// this host has not re-attached it. The next sweep switches it live.
		a.Execution, a.Failure = "deferred", "seat is live in Claudia but not yet attached to this host; retrying next sweep"
		return
	default:
		// The seat is still where it was and still working; the next tick
		// retries. Parking here would turn a refused switch into an outage.
		slog.Warn("plan policy switch failed", "name", a.Name, "to", a.To, "err", err)
		a.Execution = "failed"
		a.Failure = err.Error()
		a.FailureCause = agenterr.ProviderCause(err)
		return
	}
	if !sw.Live {
		if err := s.switcher.Launch(&thread.Thread{ID: a.Name}); err != nil {
			a.Execution, a.Failure = "pending", err.Error()
			return
		}
	}
	if err := s.SetAgentIntent(a.Name, fleetintent.Working, planPolicyActor, a.Reason); err != nil {
		a.Execution, a.Failure = "pending", err.Error()
		return
	}
	a.Execution = "switched"
	slog.Info("plan policy switched", "name", a.Name, "from", a.From, "to", a.To, "model", sw.Model, "live", sw.Live)
}

// turnInFlight reports a switch refused because the error's typed cause is
// a turn still running. A sentence that quotes that fact is not the cause.
func turnInFlight(err error) bool {
	return isPromptInFlight(err)
}

func (s *Server) rememberPlanResults(actions []planusage.PlanAction) {
	s.planDecisionMu.Lock()
	s.planLastResults = make(map[string]planusage.PlanAction, len(actions))
	for _, action := range actions {
		s.planLastResults[action.Name] = action
	}
	s.planDecisionMu.Unlock()
}

// MarkPlanRetryAfterReauth records that the owner has just repaired the
// destination login for provider: a remembered failed move to it is now a
// pending retry, not a destination auth failure still waiting on the owner.
// The next sweep replaces it with its own result.
func (s *Server) MarkPlanRetryAfterReauth(provider claudia.Provider) {
	if s == nil {
		return
	}
	plan := seatprovider.Resolve(provider).Plan
	s.planDecisionMu.Lock()
	defer s.planDecisionMu.Unlock()
	for name, last := range s.planLastResults {
		if last.Execution == "failed" && seatprovider.Resolve(claudia.Provider(last.To)).Plan == plan {
			last.Execution = "pending"
			last.Failure = "destination login recovered; retrying"
			last.FailureCause = ""
			s.planLastResults[name] = last
		}
	}
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
			d.Execution, d.Failure, d.FailureCause = last.Execution, last.Failure, last.FailureCause
		}
	}
	s.planDecisionMu.RUnlock()
	for i := range decisions {
		d := &decisions[i]
		if reason := s.planHostDeferral(*d); reason != "" && d.Execution == "" {
			d.Execution, d.Failure = "deferred", reason
		}
	}
	return decisions
}

// Claudia authors the placement verdict. Jevons decides whether acting on it
// may interrupt or park this particular seat, and reports a host deferral
// instead of silently changing the provider decision.

// alreadyPlanParked reports that name is already stopped by plan policy, so a
// second SweepPlanPolicy tick must not mint another seat_stop (and another
// MASS STOP window). The stop source is the decision. The recorded reason
// sentence is not a classifier.
func (s *Server) alreadyPlanParked(name string) bool {
	if s == nil || name == "" {
		return false
	}
	if s.registry != nil {
		if proc := s.registry.Get(name); proc != nil && (s.seatState(name).Alive == seatstate.Yes) {
			return false // still running — this park must stop it
		}
	}
	last, ok := s.seatStops().Last(name)
	return ok && last.Source == seatstop.SourcePlanPolicy
}

func (s *Server) planHostDeferral(action planusage.PlanAction) string {
	if s == nil || s.registry == nil {
		return ""
	}
	if s.registry.Def(action.Name) == nil {
		return "agent is no longer registered"
	}
	if reason := s.planIntentDeferral(action.Name); reason != "" {
		return reason
	}
	inFlight := false
	if action.Action == planusage.SeatMigrate {
		inFlight = s.seatInFlight(action.Name) == seatstate.Yes
	}
	return hostPlanDeferral(action, s.seatPlans.Get(action.Name), inFlight)
}

// A policy park is temporary placement state, not permission to reverse
// someone else's deliberate stop. Fleet-wide holds always take precedence.
func (s *Server) planIntentDeferral(name string) string {
	snap := s.fleetIntent()
	if !fleetintent.Runnable(snap.Fleet.State) {
		return "fleet intent: " + snap.Fleet.Describe()
	}
	rec := snap.Agents[name]
	if !fleetintent.Runnable(rec.State) && !planPolicyPark(rec) {
		return "agent intent: " + rec.Describe()
	}
	return ""
}

func planPolicyPark(rec fleetintent.Record) bool {
	return rec.State == fleetintent.Parked && rec.By == planPolicyActor
}

func hostPlanDeferral(action planusage.PlanAction, st claudia.SeatPolicy, inFlight bool) string {
	if action.Action == planusage.SeatPark && st.HostNeverPark {
		return "Jevons host policy forbids parking this seat"
	}
	if action.Action == planusage.SeatMigrate && inFlight && !st.HostMayInterrupt {
		return "turn in flight; Jevons host policy forbids interruption"
	}
	return ""
}

// SetSeatPlan attaches the sidecar store for placement fields the
// published AgentDef does not carry.
func (s *Server) SetSeatPlan(st *claudia.SeatPolicyStore) {
	if s == nil {
		return
	}
	s.seatPlans = st
}

func (s *Server) planPolicyAgents() []planusage.AgentRef {
	if s == nil || s.registry == nil {
		return nil
	}
	var agents []planusage.AgentRef
	intent := s.fleetIntent()
	for _, d := range s.registry.List() {
		st := s.seatPlans.Get(d.Name)
		allowed, restricted := st.Allowed()
		ref := planusage.AgentRef{
			Name: d.Name, Provider: string(d.Provider), Purpose: d.Purpose, Parent: d.Parent,
			PreferProvider: st.PreferProvider, ExcludeProviders: st.ExcludeProviders,
			RecoverPlanPark: planPolicyPark(intent.Agents[d.Name]),
		}
		if restricted {
			ref.AllowedProviders = allowed
			ref.AllowNone = len(allowed) == 0
		}
		agents = append(agents, ref)
	}
	return agents
}
