// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleet"

	"github.com/marcelocantos/jevons/internal/seatstate"
)

// ReattachFleet is the T40.2 return: every jevonsd boot adopts leftover
// processes, then Launch-es (resumes) what exited. Launch itself still
// only creates. Leftovers are not reaped. The returned names reminted
// a new session_id (🎯T545.1). The post-restart wake still full_briefs them.
//
// Upgrade handoff is no longer what chooses the start method — it is
// only consumed so a later drain start is not mistaken for an upgrade.
func ReattachFleet(reg *claudia.Registry) []string {
	return ReattachFleetContext(context.Background(), reg)
}

// ReattachFleetContext cooperates with startup cancellation when provided by
// Claudia. The compatibility path preserves the published dependency until its
// next release; activation gates must exercise the contextual implementation.
func ReattachFleetContext(ctx context.Context, reg *claudia.Registry) []string {
	return ReattachSeatsContext(ctx, reg, nil, DefaultReattachConcurrency)
}

// DefaultReattachConcurrency bounds how many seats adopt or launch at once.
// A seat's adopt can burn minutes on a broker timeout or lsof retries; serial
// start made those add up (🎯T778: 5m46s to the converge loop with four
// Cursor seats).
const DefaultReattachConcurrency = 4

// ReattachSeatsContext is [ReattachFleetContext] for the AutoStart seats
// include accepts (nil accepts all), started concurrently, at most limit at a
// time, so one slow seat delays only itself. The boot calls it for the
// overseer alone, attaches the chat and starts the cockpit converge loop,
// then calls it for the rest (🎯T778). Names whose session_id changed are
// returned.
func ReattachSeatsContext(ctx context.Context, reg *claudia.Registry, include func(string) bool, limit int) []string {
	if reg == nil {
		return nil
	}
	if limit < 1 {
		limit = 1
	}
	before := SessionSnapshot(reg)
	var names []string
	for _, d := range reg.List() {
		if include == nil || include(d.Name) {
			names = append(names, d.Name)
		}
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for _, name := range names {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(name string) {
			defer func() { <-sem; wg.Done() }()
			def := reg.Def(name)
			if def == nil || !def.AutoStart || ctx.Err() != nil {
				return
			}
			if _, err := fleet.AutoStartSeat(ctx, reg, name); err != nil {
				slog.Error("auto-start failed", "agent", name, "err", err)
				return
			}
			// A reattach is a lifecycle boundary, as readopt's is: record
			// the seat now, or it stays unwired until the 30s sweep and its
			// turns go unrecorded (journey J34, 2026-10-05).
			seatstate.ObserveRegistrySeat(reg, name)
		}(name)
	}
	wg.Wait()
	return SessionDriftNames(before, SessionSnapshot(reg))
}

// brokerAvailable is a seam over claudia.BrokerAvailable: when the claudia
// daemon runs, it owns every seat's process and its survival across a
// jevonsd restart; jevonsd only reconnects by name.
var brokerAvailable = claudia.BrokerAvailable

// SessionSnapshot is name → session_id for every registered row.
func SessionSnapshot(reg *claudia.Registry) map[string]string {
	out := map[string]string{}
	if reg == nil {
		return out
	}
	for _, d := range reg.List() {
		out[d.Name] = d.SessionID
	}
	return out
}

// SessionSnapshotFromFile loads a claudia agents.json the same way a
// restarted daemon does and returns the session snapshot. Journeys and
// hermetics share this so neither reimplements the persist format.
func SessionSnapshotFromFile(path string) (map[string]string, error) {
	reg, err := claudia.NewRegistry(path)
	if err != nil {
		return nil, fmt.Errorf("session snapshot: %w", err)
	}
	return SessionSnapshot(reg), nil
}

// SessionDrift names rows whose session_id changed (or vanished). An
// empty result is the T40.2 pass: bounce kept every conversation.
func SessionDrift(before, after map[string]string) []string {
	var out []string
	for name, sid := range before {
		got, ok := after[name]
		if !ok {
			out = append(out, name+" vanished")
			continue
		}
		if got != sid {
			out = append(out, name+": "+sid+" → "+got)
		}
	}
	return out
}

// SessionDriftNames is the remint set: rows whose session_id changed.
// Vanished names are not remints (T545.1 full_brief skip uses this).
func SessionDriftNames(before, after map[string]string) []string {
	var out []string
	for name, sid := range before {
		got, ok := after[name]
		if ok && got != sid {
			out = append(out, name)
		}
	}
	return out
}
