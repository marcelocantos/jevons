// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T796: the claudia broker detaches a seat's owner when the per-owner event
// pump overflows (claudia T125: a large replay, or a consumer read loop stalled
// under load), emits no event, and never re-claims (claudia T124). The daemon's
// connection stays open, so every later send or interrupt is refused
// `not_owner`. Both fixes reach the running broker only through claudia's
// release gate (T119); this is the jevons-side mitigation.
//
// A not_owner refusal re-adopts the seat by name through the registry's Adopt:
// the handle's dead connection is closed first (Agent.Detach, which the broker
// reads as the owner going away, not a release), so the adopt builds the one
// delivering connection that then holds the grant. Never Launch and never an
// in-process fallback: stacking a second client on the session is what T796's
// first half fixed.

// ErrReadoptExhausted marks a seat whose re-adopt budget is spent. It is loud
// on purpose: the owner is notified once and every later delivery fails fast.
var ErrReadoptExhausted = errors.New("broker grant could not be re-adopted")

// IsNotOwner reports whether err is the broker refusing a delivery because this
// connection no longer owns the seat's grant.
func IsNotOwner(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not_owner")
}

// Re-adopt pacing. A re-adopt replays the seat's ring to a fresh consumer, the
// very burst that overflowed the pump, so the loop must not feed itself.
const (
	DefaultReadoptGap         = 3 * time.Second  // first gap between two re-adopts of one seat; doubles
	DefaultReadoptMaxGap      = 60 * time.Second // ceiling of the doubling gap
	DefaultReadoptMaxAttempts = 5                // re-adopts per window before failing loud
	DefaultReadoptWindow      = 10 * time.Minute // a seat quiet this long gets a fresh budget
	readoptDeadWait           = 3 * time.Second  // how long a detached handle may take to report dead
)

// Readopter re-adopts seats whose grant the broker detached.
type Readopter struct {
	// Notify tells the owner when a seat's budget is spent. Called once per
	// exhaustion, outside any lock.
	Notify func(name string, err error)
	// OnReadopt receives the fresh handle so its owner can resubscribe (the
	// overseer's chat stream rides the handle).
	OnReadopt func(name string, a *claudia.Agent)

	Gap, MaxGap, Window time.Duration
	MaxAttempts         int

	// Seams. nil means the real registry and clock.
	get           func(name string) *claudia.Agent
	adopt         func(name string) (*claudia.Agent, error)
	detach        func(a *claudia.Agent) error
	alive         func(a *claudia.Agent) bool
	brokerPresent func() bool
	now           func() time.Time
	sleep         func(ctx context.Context, d time.Duration) error

	mu    sync.Mutex
	seats map[string]*readoptSeat
}

type readoptSeat struct {
	mu        sync.Mutex // serializes re-adopts of one seat: the dedupe
	attempts  int
	last      time.Time
	exhausted bool
}

// NewReadopter binds a Readopter to reg.
func NewReadopter(reg *claudia.Registry) *Readopter {
	return &Readopter{
		Gap: DefaultReadoptGap, MaxGap: DefaultReadoptMaxGap,
		Window: DefaultReadoptWindow, MaxAttempts: DefaultReadoptMaxAttempts,
		get:   reg.Get,
		adopt: reg.Adopt,

		brokerPresent: brokerMayOwnSeats,
	}
}

func (r *Readopter) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func (r *Readopter) wait(ctx context.Context, d time.Duration) error {
	if r.sleep != nil {
		return r.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (r *Readopter) isAlive(a *claudia.Agent) bool {
	if r.alive != nil {
		return r.alive(a)
	}
	return a != nil && a.Alive()
}

func (r *Readopter) seat(name string) *readoptSeat {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seats == nil {
		r.seats = map[string]*readoptSeat{}
	}
	s := r.seats[name]
	if s == nil {
		s = &readoptSeat{}
		r.seats[name] = s
	}
	return s
}

// Readopt replaces stale (the handle a delivery just got not_owner on) with a
// freshly adopted one and returns it. Concurrent callers for one seat share one
// adopt: the later ones find the registry already holds a live, different
// handle and return it.
func (r *Readopter) Readopt(ctx context.Context, name string, stale *claudia.Agent) (*claudia.Agent, error) {
	st := r.seat(name)
	st.mu.Lock()
	defer st.mu.Unlock()

	if cur := r.get(name); cur != nil && cur != stale && r.isAlive(cur) {
		return cur, nil
	}
	if r.brokerPresent != nil && !r.brokerPresent() {
		return nil, fmt.Errorf("re-adopt %s: no claudia broker to adopt from", name)
	}
	now := r.clock()
	if st.attempts > 0 && now.Sub(st.last) > r.Window {
		st.attempts, st.exhausted = 0, false
	}
	if st.exhausted {
		return nil, fmt.Errorf("%w: %s (budget spent, owner already told)", ErrReadoptExhausted, name)
	}
	if st.attempts >= r.MaxAttempts {
		st.exhausted = true
		err := fmt.Errorf("%w: %s lost its broker grant %d times in %s", ErrReadoptExhausted, name, st.attempts, r.Window)
		slog.Error("🎯T796 re-adopt budget spent", "agent", name, "attempts", st.attempts)
		if r.Notify != nil {
			r.Notify(name, err)
		}
		return nil, err
	}
	if st.attempts > 0 {
		gap := r.Gap << (st.attempts - 1)
		if gap <= 0 || gap > r.MaxGap {
			gap = r.MaxGap
		}
		if remaining := gap - now.Sub(st.last); remaining > 0 {
			if err := r.wait(ctx, remaining); err != nil {
				return nil, err
			}
		}
	}
	st.attempts++
	st.last = r.clock()

	slog.Warn("🎯T796 broker refused delivery not_owner; re-adopting seat", "agent", name, "attempt", st.attempts)
	if stale != nil {
		if r.detach != nil {
			_ = r.detach(stale)
		} else {
			_ = stale.Detach()
		}
		// Adopt returns the registry's prior handle while it still reports
		// alive; the detach kills it asynchronously.
		deadline := r.clock().Add(readoptDeadWait)
		for r.isAlive(stale) && r.clock().Before(deadline) {
			if err := r.wait(ctx, 20*time.Millisecond); err != nil {
				return nil, err
			}
		}
	}
	a, err := r.adopt(name)
	if err != nil {
		return nil, fmt.Errorf("re-adopt %s: %w", name, err)
	}
	if r.OnReadopt != nil {
		r.OnReadopt(name, a)
	}
	return a, nil
}

// DefaultReadopter is the daemon's, installed at boot; nil means deliveries are
// not retried.
var DefaultReadopter *Readopter

// WithReadopt runs op on proc and, when the broker answers not_owner, re-adopts
// the seat once and runs op on the fresh handle. Every other outcome, and every
// call while no Readopter is installed, is op's own.
func WithReadopt(ctx context.Context, name string, proc *claudia.Agent, op func(*claudia.Agent) error) error {
	err := op(proc)
	if !IsNotOwner(err) || DefaultReadopter == nil {
		return err
	}
	fresh, rerr := DefaultReadopter.Readopt(ctx, name, proc)
	if rerr != nil {
		return fmt.Errorf("%w (re-adopt: %v)", err, rerr)
	}
	return op(fresh)
}
