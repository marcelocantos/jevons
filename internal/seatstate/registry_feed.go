// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatstate

import (
	"context"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
)

// Registry bindings connect legacy lifecycle adapters to the same injected
// authority. They contain no observations; ReadRegistry is just Get, never a
// provider probe. A missing binding is unknown, not an implicit new authority.
var registryAuthorities sync.Map // *claudia.Registry -> *Authority

func (a *Authority) BindRegistry(reg *claudia.Registry) {
	if a != nil && reg != nil {
		registryAuthorities.Store(reg, a)
	}
}

// RegistryAuthority supplies the shared authority when constructing adapters
// outside the daemon composition root (e.g. an isolate). It performs no I/O.
func RegistryAuthority(reg *claudia.Registry) *Authority {
	if reg == nil {
		return New(Args{})
	}
	a, _ := registryAuthorities.LoadOrStore(reg, New(Args{}))
	return a.(*Authority)
}

func ReadRegistry(reg *claudia.Registry, name string) State {
	if a, ok := registryAuthorities.Load(reg); ok {
		s, _ := a.(*Authority).Get(name)
		return s
	}
	return State{Name: name, QueueDepth: QueueUnknown, OwnerQueueDepth: QueueUnknown}
}

// ObserveRegistrySeat is a feed at lifecycle boundaries, not a control read.
// Missing handles do not establish death (the broker can still own the seat).
func ObserveRegistrySeat(reg *claudia.Registry, name string) {
	bound, ok := registryAuthorities.Load(reg)
	if !ok || reg == nil {
		return
	}
	a := bound.(*Authority)
	at := time.Now()
	d := reg.Def(name)
	if d == nil {
		a.Forget(name)
		return
	}
	rep := SeatReport{Name: name, SessionID: d.SessionID}
	if p := reg.Get(name); p != nil && p.SessionID() == d.SessionID {
		rep.Known = true
		rep.Provider = string(p.Provider())
		rep.Model = p.Model()
		rep.Alive = p.Alive()
		rep.PromptInFlight = p.PromptInFlight() || p.TurnPhase() == claudia.TurnInTurn
	}
	if current := reg.Def(name); current == nil || current.SessionID != d.SessionID {
		return
	}
	a.FromClaudia(rep, at)
	a.ObserveResumeEvidence(d)
}

func (a *Authority) ObserveRegistry(reg *claudia.Registry) {
	if a == nil || reg == nil {
		return
	}
	a.BindRegistry(reg)
	for _, d := range reg.List() {
		ObserveRegistrySeat(reg, d.Name)
	}
}

// FollowRegistry is an independent feed so Get remains a constant-time read
// even when no list, send or recovery control is running.
func (a *Authority) FollowRegistry(ctx context.Context, reg *claudia.Registry) {
	a.ObserveRegistry(reg)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer registryAuthorities.CompareAndDelete(reg, a)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.ObserveRegistry(reg)
		}
	}
}

// ObserveProcess feeds a newly attached handle, including an overseer attached
// before a registry has been installed. Controls never call its raw methods.
func (a *Authority) ObserveProcess(name string, p *claudia.Agent) {
	if a == nil || p == nil || name == "" {
		return
	}
	a.FromClaudia(SeatReport{Name: name, SessionID: p.SessionID(), Provider: string(p.Provider()), Model: p.Model(), Alive: p.Alive(), PromptInFlight: p.PromptInFlight() || p.TurnPhase() == claudia.TurnInTurn, Known: true}, time.Now())
}

// ObserveStopped records the outcome of this registry's completed Stop.
func ObserveStopped(reg *claudia.Registry, name string) {
	bound, ok := registryAuthorities.Load(reg)
	if !ok {
		return
	}
	bound.(*Authority).Observe(Observation{Name: name, Alive: No, LocalAlive: No, BrokerAlive: No, InFlight: No, Status: "stopped", QueueDepth: QueueUnknown, Source: "registry.stop"})
}

// ObserveResumeEvidence reports whether the located conversation required by a
// Claude resume is absent. Other providers and failed lookups stay unknown.
func (a *Authority) ObserveResumeEvidence(d *claudia.AgentDef) {
	if d == nil || d.Name == "" {
		return
	}
	lost := Unknown
	at := time.Now()
	if !d.Materialized || d.SessionID == "" {
		lost = No
	} else if d.Provider == "" || d.Provider == claudia.ProviderClaude {
		if exists, err := claudia.SessionExists(d.SessionID, d.WorkDir); err == nil {
			lost = TriOf(!exists)
		}
	}
	a.Observe(Observation{Name: d.Name, ForSession: d.SessionID, ResumeLost: lost,
		QueueDepth: QueueUnknown, Source: "resume.evidence", At: at})
}
