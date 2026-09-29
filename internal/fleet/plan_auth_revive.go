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
	return relaunchPlanSeats(reg, planAuthCandidates(reg.List(), registryAlive(reg), intent, provider), provider, skip, now)
}

// ownerReauthCandidates are the seats an owner's repair of provider's plan
// brings back (🎯T905): every stopped seat on that plan whose intent allows
// revival and that was either meant to run (auto-start) or last failed on
// the plan login. A broker that could not open the plan when it resumed its
// seats records nothing on this host, so the recorded failure alone misses
// them; a seat the owner stopped is parked and stays stopped.
func ownerReauthCandidates(defs []claudia.AgentDef, alive func(string) bool,
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
		failed := false
		if v, ok := rehydrateFailures.Load(d.Name); ok {
			failed = PlanAuthFailure(v.(string))
		}
		if !d.AutoStart && !failed {
			continue
		}
		if dec := intent.Allow(d.Name, fleetintent.ControlRevive); !dec.Allow {
			continue
		}
		out = append(out, d.Name)
	}
	return out
}

// RevivePlanAfterOwnerReauth relaunches the seats an owner's successful
// reauth of provider's plan brings back; skip names the seat the caller
// already handled.
func RevivePlanAfterOwnerReauth(reg *claudia.Registry, intent fleetintent.Snapshot,
	provider claudia.Provider, skip string, now time.Time) []PlanAuthRevival {
	if reg == nil {
		return nil
	}
	return relaunchPlanSeats(reg, ownerReauthCandidates(reg.List(), registryAlive(reg), intent, provider), provider, skip, now)
}

func registryAlive(reg *claudia.Registry) func(string) bool {
	return func(name string) bool {
		p := reg.Get(name)
		return p != nil && p.Alive()
	}
}

// relaunchPlanSeats relaunches names, each at most once per cooldown.
func relaunchPlanSeats(reg *claudia.Registry, names []string, provider claudia.Provider, skip string, now time.Time) []PlanAuthRevival {
	var out []PlanAuthRevival
	for _, name := range names {
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

// reattachCandidates lists seats with no live handle whose intent allows
// revival: auto-start seats, and any seat this host lost to a broker
// restart (🎯T925).
func reattachCandidates(defs []claudia.AgentDef, alive func(string) bool, intent fleetintent.Snapshot, lostToBroker func(string) bool) []string {
	var out []string
	for _, d := range defs {
		if d.Name == "" || alive(d.Name) {
			continue
		}
		if !d.AutoStart && (lostToBroker == nil || !lostToBroker(d.Name)) {
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
// host holds no live handle for. A seat that is not running stays stopped,
// unless lostToBroker says this host lost it to a broker restart: then the
// broker did not bring it back, and it is relaunched on its own
// conversation, brief intact (🎯T925). lostToBroker may be nil.
func ReattachRunningSeats(reg *claudia.Registry, intent fleetintent.Snapshot, now time.Time, lostToBroker func(string) bool) []string {
	if reg == nil {
		return nil
	}
	return reattachWith(registryReattach{reg}, intent, now, lostToBroker)
}

// reattachReg is what the reattach loop needs of the registry; tests fake it.
type reattachReg interface {
	List() []claudia.AgentDef
	Alive(name string) bool
	Adopt(name string) error
	Relaunch(name string) error
}

type registryReattach struct{ reg *claudia.Registry }

func (r registryReattach) List() []claudia.AgentDef { return r.reg.List() }
func (r registryReattach) Alive(name string) bool {
	p := r.reg.Get(name)
	return p != nil && p.Alive()
}
func (r registryReattach) Adopt(name string) error {
	_, err := r.reg.Adopt(name)
	return err
}
func (r registryReattach) Relaunch(name string) error {
	_, err := LaunchRecovering(r.reg, name)
	return err
}

// brokerLostRetry spaces relaunch attempts for a seat lost to a broker
// restart. The broker is usually back within seconds, and the full cooldown
// would leave the seat down for minutes after it is (🎯T925).
const brokerLostRetry = 20 * time.Second

func reattachWith(reg reattachReg, intent fleetintent.Snapshot, now time.Time, lostToBroker func(string) bool) []string {
	lost := func(name string) bool { return lostToBroker != nil && lostToBroker(name) }
	var attached []string
	for _, name := range reattachCandidates(reg.List(), reg.Alive, intent, lostToBroker) {
		cooldown := brokerReattachCooldown
		if lost(name) {
			cooldown = brokerLostRetry
		}
		if last, ok := brokerReattachTried.Load(name); ok && now.Sub(last.(time.Time)) < cooldown {
			continue
		}
		brokerReattachTried.Store(name, now)
		if err := reg.Adopt(name); err != nil {
			if !lost(name) {
				noteAdoptFailure(name, err)
				continue
			}
			// The broker did not bring this seat back. It went down with the
			// broker, not on purpose, so it comes back on its own conversation.
			if lerr := reg.Relaunch(name); lerr != nil {
				noteAdoptFailure(name, lerr)
				slog.Warn("seat lost to a broker restart: relaunch failed", "name", name, "adopt_err", err, "err", lerr)
				continue
			}
			slog.Info("relaunched seat the broker did not bring back after it restarted", "name", name)
		}
		brokerReattachTried.Delete(name)
		// It is running: an older launch refusal no longer describes it.
		noteRehydrate(name, nil)
		slog.Info("re-attached running seat after its host handle was lost", "name", name)
		attached = append(attached, name)
	}
	return attached
}

// noteAdoptFailure records an adopt that failed on the plan login (🎯T905):
// the broker could not open the plan to resume the seat, and nothing else on
// this host would say so. The standing sweep then relaunches it once a seat
// on the plan runs again. Any other adopt failure is not recorded.
func noteAdoptFailure(name string, err error) {
	if err != nil && PlanAuthFailure(err.Error()) {
		noteRehydrate(name, err)
	}
}
