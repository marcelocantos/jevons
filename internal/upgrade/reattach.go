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

	"github.com/marcelocantos/jevons/internal/fleet"
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
// time. Each seat is reaped, hushed and adopted on its own, so one slow seat
// delays only itself. The boot calls it for the overseer alone, attaches the
// chat and starts the cockpit converge loop, then calls it for the rest
// (🎯T778). Names whose session_id changed are returned.
func ReattachSeatsContext(ctx context.Context, reg *claudia.Registry, include func(string) bool, limit int) []string {
	if reg == nil {
		return nil
	}
	if limit < 1 {
		limit = 1
	}
	accepts := func(name string) bool { return include == nil || include(name) }
	before := SessionSnapshot(reg)
	releasePhantomCursorSessions(reg)
	// Cursor ACP stdio cannot be adopted in-process. Without a claudia
	// daemon, reap leftover writers (and ppid=1 orphans) and wait for
	// them to exit before PreferAdopt Launch — a second session/load
	// on a still-held store.db is 🎯T541.1. Seats that will not die
	// lose AutoStart (fail loud). With a daemon, the leftover is the
	// live seat and Launch reclaims it by name.
	reapCursor := !brokerMayOwnSeats()
	if reapCursor {
		for _, d := range reg.List() {
			if d.Provider == claudia.ProviderCursor && accepts(d.Name) {
				reapOrphanCursorACP()
				break
			}
		}
	} else {
		slog.Info("claudia daemon present; fleet seats are reclaimed, not reaped")
	}
	var names []string
	for _, d := range reg.List() {
		if accepts(d.Name) {
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
			only := func(n string) bool { return n == name }
			if reapCursor {
				reapCursorLeftoversIn(reg, only)
				hushUnreapedCursorSeatsIn(reg, only)
			}
			if ctx.Err() != nil {
				return
			}
			def := reg.Def(name)
			if def == nil || !def.AutoStart {
				return
			}
			if _, err := adoptOrLaunchRetryingHeld(ctx, reg, name); err != nil {
				slog.Error("auto-start failed", "agent", name, "err", err)
				if errors.Is(err, ErrClaudeHeldByBroker) {
					if notify := LaunchRefusedNotifier; notify != nil {
						notify(name, err)
					}
				}
				return
			}
			ReapClaudeStraysAfterGrant(reg, name)
		}(name)
	}
	wg.Wait()
	return SessionDriftNames(before, SessionSnapshot(reg))
}

// reapOrphanCursorACP is a seam so hermetics never signal live leftovers.
var reapOrphanCursorACP = claudia.ReapOrphanCursorACP

// brokerAvailable is a seam over claudia.BrokerAvailable: when the claudia
// daemon runs, it owns every seat's process and its survival across a
// jevonsd restart; jevonsd only reconnects by name.
var brokerAvailable = claudia.BrokerAvailable

// ReapCursorFleetLeftovers kills leftover writers on every registered
// Cursor session store (persisted ConnectPID + anyone holding store.db).
func ReapCursorFleetLeftovers(reg *claudia.Registry) {
	reapCursorLeftoversIn(reg, nil)
}

func reapCursorLeftoversIn(reg *claudia.Registry, include func(string) bool) {
	if reg == nil {
		return
	}
	for _, d := range reg.List() {
		if d.Provider != claudia.ProviderCursor || (include != nil && !include(d.Name)) {
			continue
		}
		claudia.ReapCursorACPLeftovers(d.SessionID, d.ConnectPID)
	}
}

// StopNonAdoptable stops Cursor (and other stdio-owned) seats on upgrade
// exit. Grok connect-mode and Claude tmux stay running for reattach.
func StopNonAdoptable(reg *claudia.Registry) int {
	if reg == nil {
		return 0
	}
	if brokerMayOwnSeats() {
		// Every seat is adoptable when the daemon parents it: the next
		// jevonsd grants by name and gets the running process back.
		return 0
	}
	n := 0
	for _, d := range reg.List() {
		h := Handle{
			Name:         d.Name,
			SessionID:    d.SessionID,
			Provider:     string(d.Provider),
			ConnectURL:   d.ConnectURL,
			PID:          d.ConnectPID,
			TmuxWindowID: "",
		}
		proc := reg.Get(d.Name)
		if proc != nil {
			if p := proc.PID(); p > 0 {
				h.PID = p
			}
			if u := proc.ConnectURL(); u != "" {
				h.ConnectURL = u
			}
			if w := adoptiveTmuxWindowID(proc.WindowID()); w != "" {
				h.TmuxWindowID = w
			}
		}
		if !ShouldStopOnUpgrade(h, proc != nil) {
			continue
		}
		reg.Stop(d.Name)
		n++
	}
	return n
}

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

// LaunchRefusedNotifier, when set, is told once per seat when the bounded
// refuse-and-retry is exhausted (🎯T796.1): the seat did not start because a
// client already holds its session and the broker could not be ruled out. The
// refusal signals nothing, so the only way the owner learns of it is this
// notice. The daemon wires it to the owner notice channel.
var LaunchRefusedNotifier func(agent string, err error)

// heldRetryDelay and heldRetries bound how long a seat waits for the broker's
// grant to come free: the previous daemon's connection is still closing when
// a restart's adopt is refused with grant_held (🎯T796).
var (
	heldRetryDelay = 2 * time.Second
	heldRetries    = 10
)

// adoptOrLaunchRetryingHeld adopts a seat, and when the launch fallback is
// refused because the broker's own client already holds the session, waits and
// adopts again instead of stacking a second client.
func adoptOrLaunchRetryingHeld(ctx context.Context, reg *claudia.Registry, name string) (*claudia.Agent, error) {
	// A Cursor restart does not session/load the stored id and then brief
	// only if that fails. The fresh session is the start, and the
	// post-restart wake sends the brief either way.
	if err := fleet.RestartCursorFresh(reg, name); err != nil {
		slog.Warn("cursor restart fresh session failed", "agent", name, "err", err)
	}
	for attempt := 0; ; attempt++ {
		a, err := reg.AdoptOrLaunchContext(ctx, name)
		if err == nil || !errors.Is(err, ErrClaudeHeldByBroker) || attempt >= heldRetries {
			// AdoptOrLaunch latches a Cursor session/load refusal and
			// every later boot repeats it. A leftover still holding the
			// store is 🎯T541.1 and must not remint. "refusing to mint"
			// with nobody on the store is a dead session id.
			if err != nil && resumeDeniedRemint(reg.Def(name), err) {
				slog.Warn("auto-start reminting after provider resume refusal", "agent", name, "err", err)
				return fleet.LaunchRecovering(reg, name)
			}
			return a, err
		}
		slog.Warn("grant held by another connection; waiting to adopt", "agent", name, "attempt", attempt+1)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(heldRetryDelay):
		}
	}
}

// resumeDeniedRemint is the boot-path half of fleet.LaunchRecovering.
// AdoptOrLaunch latches the refusal, so the next bounce would otherwise
// repeat the same session/load forever. A store a leftover still holds
// is 🎯T541.1: that error wraps the same sentinel and must not rotate.
func resumeDeniedRemint(def *claudia.AgentDef, err error) bool {
	if err == nil || def == nil || strings.Contains(err.Error(), "still holds store") {
		return false
	}
	switch def.Provider {
	case claudia.ProviderCursor:
		return claudia.IsCursorResumeDenied(err)
	case claudia.ProviderGrok:
		return strings.Contains(err.Error(), "exclusive GROK_HOME unavailable")
	default:
		return false
	}
}
