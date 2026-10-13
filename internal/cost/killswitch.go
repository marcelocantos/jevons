// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// BrokerSeat is one seat the claudia broker holds, as the kill-switch sees it.
type BrokerSeat struct {
	Name  string
	Owned bool
}

// BrokerKillSwitch is the global stop for the claudia worker fleet. Every
// seat runs on the omp agent core under the claudia broker, which outlives
// jevonsd and resumes seats on its own restart — so a seat the registry has
// lost (no connection owns it) keeps burning unless the broker is told to
// stop it. Seats a connection owns are stopped through the registry by the
// caller ([Actions.StopFleet]); this reaches the rest.
//
// Scope is deliberate: it stops only broker seats, never arbitrary
// processes, so the owner's own interactive sessions are untouched.
type BrokerKillSwitch struct {
	// List returns every seat the broker holds (claudia.BrokerSeats).
	List func(context.Context) ([]BrokerSeat, error)
	// Stop stops one unowned seat and drops its grant (claudia.StopBrokerSeat).
	Stop func(context.Context, string) error
	// Protect names seats never stopped (the overseer, 🎯T139).
	Protect []string
	// Timeout bounds the whole pass. Zero means 30s.
	Timeout time.Duration
}

// Kill stops every unowned, unprotected broker seat. No broker is success:
// there is nothing for the switch to reach.
func (k *BrokerKillSwitch) Kill() error {
	if k == nil || k.List == nil || k.Stop == nil {
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
	for _, s := range seats {
		if s.Owned || protect[s.Name] {
			continue
		}
		if err := k.Stop(ctx, s.Name); err != nil {
			errs = append(errs, fmt.Errorf("stop %s: %w", s.Name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("killswitch: %w", errors.Join(errs...))
	}
	return nil
}
