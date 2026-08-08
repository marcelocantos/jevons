// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package sticky is the owner-visible surface of the 🎯T317 impatience
// ladder's top rung: a sticky alert the owner sees when re-pressure and
// overseer noise have both failed to get an agent working again.
//
// It implements converge.HumanSink. Two properties are load-bearing and both
// are oracled here:
//
//   - The alert is withdrawable. 🎯T319 corrected the human rung from an
//     ack-only latch to an auto-clearing one: satisfaction withdraws the
//     alert with no owner action. A surface that could only be raised would
//     make that correction unimplementable, so Backend.Withdraw is required,
//     not optional.
//   - One alert per agent, replaced rather than stacked. The ladder re-fires
//     the human rung every HumanAlertEvery while the gap stays open; each
//     re-fire must land on the same alert, not pile up a queue the owner has
//     to shovel out.
//
// The OS call itself lives behind Backend so the state machine above is
// hermetic and platform-neutral; the macOS implementation is in
// sticky_darwin.go.
package sticky

import (
	"fmt"
	"strings"
	"sync"
)

// groupPrefix namespaces this product's alerts in the OS notification store,
// so a withdraw can never reach something jevons did not raise.
const groupPrefix = "jevons-converge-impatience"

// alertTitle is what the owner sees in bold. Deliberately names the product
// and the condition, because the alert arrives without context.
const alertTitle = "Jevons — convergence stuck"

// Backend is the OS notification surface. Withdraw must actually remove a
// previously shown alert; a backend that cannot withdraw is not usable for
// this rung and must fail at construction rather than no-op at runtime.
type Backend interface {
	Show(group, title, message string) error
	Withdraw(group string) error
}

// Sticky tracks which agents currently have an alert on screen so that
// withdrawal is exact and repeat raises coalesce.
//
// Safe for concurrent use: the converge loop drives it, but the daemon may
// clear from a different goroutine when an agent reports in.
type Sticky struct {
	mu  sync.Mutex
	be  Backend
	lit map[string]string // agent → message currently on screen
}

// New wraps a backend. Returns nil if be is nil, so a daemon that could not
// build an OS surface leaves the sink unwired — converge.Actuate reports an
// unwired rung as an error rather than swallowing the alert.
func New(be Backend) *Sticky {
	if be == nil {
		return nil
	}
	return &Sticky{be: be, lit: map[string]string{}}
}

// RaiseImpatienceAlert puts (or replaces) the sticky alert for agent.
//
// An identical repeat is coalesced: the ladder's anti-thrash interval already
// bounds re-fires, and this bounds the degenerate case where the same text
// arrives twice. A changed message always re-shows, since the dwell it quotes
// has grown and a stale duration would understate how long the owner has been
// waiting.
func (s *Sticky) RaiseImpatienceAlert(agent, text string) error {
	if s == nil {
		return fmt.Errorf("sticky: no surface wired")
	}
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return fmt.Errorf("sticky: raise needs an agent name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.lit[agent]; ok && prev == text {
		return nil
	}
	if err := s.be.Show(Group(agent), alertTitle, text); err != nil {
		// Leave the agent unlit so the next tick retries rather than
		// believing an alert the owner never saw.
		delete(s.lit, agent)
		return fmt.Errorf("sticky: raise %s: %w", agent, err)
	}
	s.lit[agent] = text
	return nil
}

// ClearImpatienceAlert withdraws the alert for agent. Clearing an agent that
// has no alert is a no-op that does not touch the OS: the ladder emits
// ActClearHuman only when it lit the sticky, but a daemon restart can leave
// the ladder and the screen briefly disagreeing, and a spurious withdraw
// would be a silent lie about state we do not own.
func (s *Sticky) ClearImpatienceAlert(agent string) error {
	if s == nil {
		return fmt.Errorf("sticky: no surface wired")
	}
	agent = strings.TrimSpace(agent)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.lit[agent]; !ok {
		return nil
	}
	if err := s.be.Withdraw(Group(agent)); err != nil {
		return fmt.Errorf("sticky: clear %s: %w", agent, err)
	}
	delete(s.lit, agent)
	return nil
}

// Lit reports the agents whose alerts are currently on screen. The daemon
// uses it to withdraw everything on shutdown; tests use it as the oracle for
// raise/clear bookkeeping.
func (s *Sticky) Lit() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.lit))
	for a := range s.lit {
		out = append(out, a)
	}
	return out
}

// ClearAll withdraws every alert this Sticky raised. Called on daemon
// shutdown so a stale "stuck" alert does not outlive the daemon that could
// have withdrawn it.
func (s *Sticky) ClearAll() []error {
	if s == nil {
		return nil
	}
	var errs []error
	for _, a := range s.Lit() {
		if err := s.ClearImpatienceAlert(a); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// Group is the OS notification group for one agent's alert — stable across
// re-fires so the OS replaces rather than stacks, and namespaced so a
// withdraw only ever reaches jevons' own alerts.
func Group(agent string) string {
	return groupPrefix + "-" + agent
}
