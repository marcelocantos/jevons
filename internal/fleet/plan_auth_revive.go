// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// A plan login failure is shared by every seat on that plan. When the owner
// repairs it through one seat, or a seat on the plan is plainly running
// again, the other seats that broke on the same failure are relaunched
// without another click. Only seats with a recorded rehydrate failure are
// candidates, so a seat the owner stopped deliberately stays stopped; the
// fleet intent's revive gate is honoured on top of that.

// planAuthReviveCooldown spaces automatic retries of one seat, so a plan
// that is still broken is not hammered once per sweep.
const planAuthReviveCooldown = 2 * time.Minute

var planAuthReviveTried sync.Map // name -> time.Time

// planAuthFailureMarkers are substrings of a recorded launch refusal that
// mean the plan's login or credential store failed, rather than the seat.
var planAuthFailureMarkers = []string{
	"invalid_grant", "refresh token", "refresh failed", "keychain",
	"plan blob", "authentication failed", "auth recovery", "login required",
	"not logged in", "no login", "oauth",
}

// PlanAuthFailure reports whether a recorded launch failure is a plan
// login failure that another seat's recovery also repairs.
func PlanAuthFailure(failure string) bool {
	failure = strings.ToLower(failure)
	for _, m := range planAuthFailureMarkers {
		if strings.Contains(failure, m) {
			return true
		}
	}
	return false
}

// PlanAuthRevival is one relaunch attempt made by RevivePlanAuthPeers.
type PlanAuthRevival struct {
	Name string
	Err  error
}

// planAuthCandidates lists stopped seats on provider's plan whose last
// rehydrate failed on plan authentication and whose intent allows revival.
func planAuthCandidates(defs []claudia.AgentDef, alive func(string) bool,
	intent fleetintent.Snapshot, provider claudia.Provider) []string {
	plan := claudia.PlanProvider(provider)
	if plan == "" {
		return nil
	}
	var out []string
	for _, d := range defs {
		if d.Name == "" || claudia.PlanProvider(d.Provider) != plan || alive(d.Name) {
			continue
		}
		v, ok := rehydrateFailures.Load(d.Name)
		if !ok || !PlanAuthFailure(v.(string)) {
			continue
		}
		if dec := intent.Allow(d.Name, fleetintent.ControlRevive); !dec.Allow {
			continue
		}
		out = append(out, d.Name)
	}
	return out
}

// RevivePlanAuthPeers relaunches every stopped seat on provider's plan that
// broke on a plan login failure. Call it once the plan is known good: after
// a successful owner reauth, or when a seat on the plan is running. Each
// seat is tried at most once per cooldown; skip names the seat the caller
// already handled.
func RevivePlanAuthPeers(reg *claudia.Registry, intent fleetintent.Snapshot,
	provider claudia.Provider, skip string, now time.Time) []PlanAuthRevival {
	if reg == nil {
		return nil
	}
	alive := func(name string) bool {
		p := reg.Get(name)
		return p != nil && p.Alive()
	}
	var out []PlanAuthRevival
	for _, name := range planAuthCandidates(reg.List(), alive, intent, provider) {
		if name == skip {
			continue
		}
		if last, ok := planAuthReviveTried.Load(name); ok && now.Sub(last.(time.Time)) < planAuthReviveCooldown {
			continue
		}
		planAuthReviveTried.Store(name, now)
		_, err := LaunchReconciled(reg, name)
		if err != nil {
			slog.Warn("plan auth revive: relaunch failed", "name", name, "provider", provider, "err", err)
		} else {
			planAuthReviveTried.Delete(name)
			slog.Info("plan auth revive: relaunched seat after plan login recovered", "name", name, "provider", provider)
		}
		out = append(out, PlanAuthRevival{Name: name, Err: err})
	}
	return out
}

// RevivePlanAuthWhereHealthy is the standing sweep: any plan with a running
// seat has a working login, so its auth-broken peers are relaunched.
func RevivePlanAuthWhereHealthy(reg *claudia.Registry, intent fleetintent.Snapshot, now time.Time) []PlanAuthRevival {
	if reg == nil {
		return nil
	}
	healthy := map[claudia.Provider]bool{}
	for _, d := range reg.List() {
		if p := reg.Get(d.Name); p != nil && p.Alive() {
			if plan := claudia.PlanProvider(d.Provider); plan != "" {
				healthy[plan] = true
			}
		}
	}
	var out []PlanAuthRevival
	for plan := range healthy {
		out = append(out, RevivePlanAuthPeers(reg, intent, plan, "", now)...)
	}
	return out
}

// A broker restart relaunches its seats, but this host's handles died with
// the old connection and nothing re-attached them: the owner saw four
// working POs as stopped while the broker ran them unowned (2026-09-28,
// 🎯T884). Adopt only re-attaches a seat that is already running; it never
// launches one, so an adopt that fails leaves the seat exactly as it was.

// brokerReattachCooldown spaces failed re-attach attempts for one seat.
const brokerReattachCooldown = 2 * time.Minute

var brokerReattachTried sync.Map // name -> time.Time

// reattachCandidates lists auto-start seats with no live handle whose
// intent allows revival.
func reattachCandidates(defs []claudia.AgentDef, alive func(string) bool, intent fleetintent.Snapshot) []string {
	var out []string
	for _, d := range defs {
		if d.Name == "" || !d.AutoStart || alive(d.Name) {
			continue
		}
		if dec := intent.Allow(d.Name, fleetintent.ControlRevive); !dec.Allow {
			continue
		}
		out = append(out, d.Name)
	}
	return out
}

// ReattachRunningSeats re-adopts seats the broker is still running but this
// host holds no live handle for. A seat that is not running stays stopped.
func ReattachRunningSeats(reg *claudia.Registry, intent fleetintent.Snapshot, now time.Time) []string {
	if reg == nil {
		return nil
	}
	alive := func(name string) bool {
		p := reg.Get(name)
		return p != nil && p.Alive()
	}
	var attached []string
	for _, name := range reattachCandidates(reg.List(), alive, intent) {
		if last, ok := brokerReattachTried.Load(name); ok && now.Sub(last.(time.Time)) < brokerReattachCooldown {
			continue
		}
		brokerReattachTried.Store(name, now)
		if _, err := reg.Adopt(name); err != nil {
			continue
		}
		brokerReattachTried.Delete(name)
		// It is running: an older launch refusal no longer describes it.
		noteRehydrate(name, nil)
		slog.Info("re-attached running seat after its host handle was lost", "name", name)
		attached = append(attached, name)
	}
	return attached
}
