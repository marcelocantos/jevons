// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turndepth

import "sync"

// Counter tracks how deep each agent's current turn has run.
//
// It exists for the OUTSIDE observer — the daemon, which sees an agent's
// tool calls as claudia progress events and its turn boundaries as terminal
// stops, and which owns the eventlog record and the interrupt fallback. The
// in-band ask does not use it: a hook runs inside the agent's own process
// and keeps its own count on disk, because the two counts answer to
// different lifetimes (a daemon restart must not silently reset a turn's
// depth for the agent that is still running it, and a session that outlives
// the daemon must not inherit the daemon's idea of where it was).
//
// Deliberately no clock and no I/O: a turn's depth is a count of events, and
// a Counter that expired entries on time would start guessing at boundaries
// it can observe directly.
type Counter struct {
	mu    sync.Mutex
	turns map[string]*State
}

// NewCounter returns an empty Counter, ready for concurrent use.
func NewCounter() *Counter {
	return &Counter{turns: map[string]*State{}}
}

// Tool records one tool call for agent and returns the turn as it now
// stands, ready to hand to Policy.Evaluate.
func (c *Counter) Tool(agent string) State {
	if c == nil {
		return State{Agent: agent}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.get(agent)
	st.Calls++
	return *st
}

// MarkRequested latches that this turn has been asked to checkpoint, so the
// ask fires once rather than on every call that follows it.
func (c *Counter) MarkRequested(agent string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.get(agent).Requested = true
}

// MarkInterrupted latches that the fallback has fired for this turn.
func (c *Counter) MarkInterrupted(agent string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.get(agent).Interrupted = true
}

// EndTurn clears the agent's turn and returns what it looked like at the
// end. The returned State is the record of a completed turn — its Calls is
// the depth reached, which is what the depth histogram is made of.
func (c *Counter) EndTurn(agent string) State {
	if c == nil {
		return State{Agent: agent}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.turns[agent]
	if !ok {
		return State{Agent: agent}
	}
	delete(c.turns, agent)
	return *st
}

// Depth reports the current turn's depth without disturbing it.
func (c *Counter) Depth(agent string) int {
	return c.State(agent).Calls
}

// State reports the current turn without disturbing it.
func (c *Counter) State(agent string) State {
	if c == nil {
		return State{Agent: agent}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if st, ok := c.turns[agent]; ok {
		return *st
	}
	return State{Agent: agent}
}

// Forget drops an agent entirely — used when an agent leaves the fleet, so a
// long-lived daemon does not accumulate turn records for names that no
// longer exist.
func (c *Counter) Forget(agent string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.turns, agent)
}

// get returns the agent's live turn, creating it on first sight. Caller
// holds the lock.
func (c *Counter) get(agent string) *State {
	if c.turns == nil {
		c.turns = map[string]*State{}
	}
	st, ok := c.turns[agent]
	if !ok {
		st = &State{Agent: agent}
		c.turns[agent] = st
	}
	return st
}
