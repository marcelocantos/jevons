// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// BrokerKillSwitch is the global stop for the claudia worker fleet. Every
// seat runs on the omp agent core under the claudia broker, which outlives
// jevonsd and resumes seats on its own restart — so a seat the registry has
// lost keeps burning unless the broker is told to stop it. Registered seats
// are stopped through the registry by the caller ([Actions.StopFleet]); this
// reaches the rest.
//
// Scope is deliberate: it stops only broker seats, never arbitrary
// processes, so the owner's own interactive sessions are untouched.
type BrokerKillSwitch struct {
	// List returns the name of every seat the broker holds.
	List func(context.Context) ([]string, error)
	// Registered reports a name the registry tracks; StopFleet owns those.
	Registered func(string) bool
	// Stop stops one seat and drops its grant (claudia.StopBrokerSeat).
	Stop func(context.Context, string) error
	// Protect names seats never stopped (the overseer, 🎯T139).
	Protect []string
	// Timeout bounds the whole pass. Zero means 30s.
	Timeout time.Duration
}

// Kill stops every unregistered, unprotected broker seat.
func (k *BrokerKillSwitch) Kill() error {
	if k == nil || k.List == nil || k.Stop == nil || k.Registered == nil {
		return fmt.Errorf("killswitch: broker seams not wired")
	}
	timeout := k.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	seats, err := k.List(ctx)
	if err != nil {
		return fmt.Errorf("killswitch: list broker seats: %w", err)
	}
	protect := map[string]bool{}
	for _, p := range k.Protect {
		protect[p] = true
	}
	var errs []error
	for _, name := range seats {
		if protect[name] || k.Registered(name) {
			continue
		}
		if err := k.Stop(ctx, name); err != nil {
			errs = append(errs, fmt.Errorf("stop %s: %w", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("killswitch: %w", errors.Join(errs...))
	}
	return nil
}
