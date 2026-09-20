// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnrate

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// DeferredError is returned when AllowTurn does not run the turn.
// SessionID is the request's session — the spend path did not remint.
type DeferredError struct {
	Decision Decision
}

func (e *DeferredError) Error() string {
	if e == nil {
		return "turn deferred"
	}
	d := e.Decision
	return fmt.Sprintf("turn %s for %q: %s", d.Verdict, d.Agent, d.Reason)
}

// IsDeferred reports whether err is a turn-rate deferral (delay, pause, refuse).
func IsDeferred(err error) bool {
	var d *DeferredError
	return errors.As(err, &d)
}

// GovernorArgs constructs a Governor. Policy and Burn may be nil.
type GovernorArgs struct {
	Policy func() Policy
	Burn   func(agent string) Burn
	Now    func() time.Time
}

// Governor is the stateful half: last-turn times plus the live burn
// snapshot. AllowTurn is the fleet Send/Deliver seam.
type Governor struct {
	policy func() Policy
	burn   func(agent string) Burn
	now    func() time.Time

	mu       sync.Mutex
	lastTurn map[string]time.Time
}

// NewGovernor constructs a Governor. Nil Policy uses DefaultPolicy; nil
// Burn is a zero snapshot (admit); nil Now uses time.Now.
func NewGovernor(args GovernorArgs) *Governor {
	g := &Governor{
		policy:   args.Policy,
		burn:     args.Burn,
		now:      args.Now,
		lastTurn: map[string]time.Time{},
	}
	if g.policy == nil {
		g.policy = DefaultPolicy
	}
	if g.burn == nil {
		g.burn = func(string) Burn { return Burn{} }
	}
	if g.now == nil {
		g.now = time.Now
	}
	return g
}

// AllowTurn is the host seam: nil error means run this turn now.
// A DeferredError means delay, pause, or refuse — session unchanged.
func (g *Governor) AllowTurn(agent, sessionID string) error {
	if g == nil {
		return nil
	}
	now := g.now()
	burn := g.burn(agent)
	pol := g.policy()
	g.mu.Lock()
	d := pol.Admit(Request{
		Agent:     agent,
		SessionID: sessionID,
		LastTurn:  g.lastTurn[agent],
		Now:       now,
	}, burn)
	if d.Admitted() {
		g.lastTurn[agent] = now
	}
	g.mu.Unlock()
	if d.Admitted() {
		return nil
	}
	return &DeferredError{Decision: d}
}

// LastTurn reports when agent last ran an admitted turn (zero if never).
func (g *Governor) LastTurn(agent string) time.Time {
	if g == nil {
		return time.Time{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastTurn[agent]
}
