// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Policy is the tunable half of admission control. The zero value is
// unusable; start from DefaultPolicy and override (durable overrides live in
// state_dir/capacity.json — see config.go).
type Policy struct {
	// Disabled turns admission control off: every request is admitted in
	// full. The 🎯T36 clamp still applies underneath — this switch only
	// removes the holistic layer, never the safety net.
	Disabled bool `json:"disabled,omitempty"`

	// DailyTokenBudget is the fleet's token allowance per local day. 0 means
	// unset, and unset means unknown: token headroom then reports "unknown"
	// rather than a fabricated ceiling, and admission falls back to USD (when
	// billable) and concurrent load. Set it to make the honest lever bind
	// under subscription accounting, where USD is only an estimate (🎯T137).
	DailyTokenBudget int64 `json:"daily_token_budget"`

	// OwnerReserveFraction is the share of the budget (tokens, and USD when
	// billable) held back for owner turns and open Build missions. Once
	// headroom falls below it, background work stops competing for what is
	// left.
	OwnerReserveFraction float64 `json:"owner_reserve_fraction"`
	// DegradeFraction is the headroom below which ambient work drops to the
	// reduced tier. Must be ≥ OwnerReserveFraction to have any effect.
	DegradeFraction float64 `json:"degrade_fraction"`

	// MaxConcurrentBackground bounds background cycles in flight across all
	// classes. 0 = unbounded.
	MaxConcurrentBackground int `json:"max_concurrent_background"`
	// MaxPerClass bounds cycles in flight per class; a missing entry falls
	// back to DefaultMaxPerClass. 0 = unbounded for that class.
	MaxPerClass map[Class]int `json:"max_per_class,omitempty"`

	// LoadPerCoreCritical is the 1-minute load average per core at which the
	// host itself is out of capacity (🎯T463). 0 falls back to
	// DefaultLoadPerCoreCritical. Configurable and per-core on purpose: the
	// same binary runs on a 16-core laptop and on whatever CI provides.
	LoadPerCoreCritical float64 `json:"load_per_core_critical,omitempty"`
	// SwapCriticalFraction is the share of configured swap in use at which the
	// host is treated as out of memory. 0 falls back to
	// DefaultSwapCriticalFraction.
	SwapCriticalFraction float64 `json:"swap_critical_fraction,omitempty"`
	// ProviderCapFallback is the concurrency cap for a provider that publishes
	// no soft cap. 0 in a cap table means unpublished, never unlimited
	// (🎯T463); 0 here falls back to DefaultProviderCapFallback.
	ProviderCapFallback int `json:"provider_cap_fallback,omitempty"`

	// LoadBearing are the background classes that keep running at
	// PressureTight — the ones whose absence leaves the fleet stuck.
	LoadBearing []Class `json:"load_bearing,omitempty"`

	// OwnerBusyDefersBackground defers ambient work while an owner-facing
	// turn is in flight (🎯T291). Load-bearing classes are exempt.
	OwnerBusyDefersBackground bool `json:"owner_busy_defers_background"`

	// RetryAfter is the hint returned with a deferral, for callers that can
	// requeue rather than wait a whole tick.
	RetryAfter Duration `json:"retry_after"`
}

// Duration marshals as a human-editable string ("5m") in capacity.json,
// matching the cost package's budget.json convention.
type Duration time.Duration

// Std returns the standard-library form.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// DefaultMaxPerClass is the per-class in-flight bound used when a class has
// no explicit entry: one ambient cycle of a kind at a time.
const DefaultMaxPerClass = 1

// DefaultPolicy returns the shipped policy. The fractions are deliberately
// generous — the point is to stop the fleet piling ambient work onto a budget
// that is nearly spent, not to throttle a quiet day.
func DefaultPolicy() *Policy {
	return &Policy{
		OwnerReserveFraction:      0.20,
		DegradeFraction:           0.40,
		LoadPerCoreCritical:       DefaultLoadPerCoreCritical,
		SwapCriticalFraction:      DefaultSwapCriticalFraction,
		ProviderCapFallback:       DefaultProviderCapFallback,
		MaxConcurrentBackground:   3,
		MaxPerClass:               map[Class]int{},
		LoadBearing:               []Class{ClassControlRepair},
		OwnerBusyDefersBackground: true,
		RetryAfter:                Duration(5 * time.Minute),
	}
}

// maxForClass returns the in-flight bound for c.
func (p *Policy) maxForClass(c Class) int {
	if p.MaxPerClass != nil {
		if n, ok := p.MaxPerClass[c]; ok {
			return n
		}
	}
	return DefaultMaxPerClass
}

// isLoadBearing reports whether c keeps running at PressureTight.
func (p *Policy) isLoadBearing(c Class) bool {
	return slices.Contains(p.LoadBearing, c)
}

// Assessment is the holistic read of a Snapshot: how much room is left, why,
// and whether only owner work still fits.
type Assessment struct {
	Pressure Pressure `json:"pressure"`
	// Headroom is the tightest known fraction of remaining capacity in
	// [0,1]; 1 when nothing is known (no budget configured, no load).
	Headroom float64 `json:"headroom"`
	// CostHeadroom / TokenHeadroom / LoadHeadroom are the three inputs, or
	// -1 when that dimension is unknown (or not honest to use: USD under
	// subscription accounting).
	CostHeadroom  float64 `json:"cost_headroom"`
	TokenHeadroom float64 `json:"token_headroom"`
	LoadHeadroom  float64 `json:"load_headroom"`
	// HostHeadroom is what the host itself has left — run-queue length per
	// core and swap occupancy (🎯T463) — or -1 when nothing read it. It is
	// folded into LoadHeadroom rather than kept beside it, because "load" is
	// the dimension a saturated host saturates; reported separately so a
	// pinned host can be told apart from a pinned provider cap.
	HostHeadroom float64 `json:"host_headroom"`
	// OwnerOnly is true when nothing but owner and Build work fits.
	OwnerOnly bool `json:"owner_only"`
	// Reasons are the human sentences behind the pressure, most significant
	// first.
	Reasons []string `json:"reasons,omitempty"`
	// Residual names what this assessment cannot decide, so a caller can
	// report it honestly rather than imply a precision it does not have.
	Residual string `json:"residual,omitempty"`
}

// unknownHeadroom marks a dimension that carries no signal.
const unknownHeadroom = -1.0

// fraction returns the remaining share of limit after used, clamped to [0,1].
// A non-positive limit is unknown.
func fraction(used, limit float64) float64 {
	if limit <= 0 {
		return unknownHeadroom
	}
	left := 1 - used/limit
	if left < 0 {
		return 0
	}
	if left > 1 {
		return 1
	}
	return left
}

// Assess computes the capacity picture from a snapshot.
//
// USD only counts when Billable (🎯T137): under a flat subscription the
// figures are API-equivalent estimates, so they are reported as unknown here
// and can never deny work on their own. Tokens and concurrent load stay
// binding in every accounting mode.
func Assess(snap Snapshot, pol *Policy) Assessment {
	if pol == nil {
		pol = DefaultPolicy()
	}
	a := Assessment{
		CostHeadroom:  unknownHeadroom,
		TokenHeadroom: unknownHeadroom,
		LoadHeadroom:  unknownHeadroom,
		HostHeadroom:  unknownHeadroom,
		Headroom:      1,
	}

	if snap.Billable {
		// The tighter of "projected against today's budget" and "spent
		// against the hard ceiling": a projection can be optimistic, but
		// money already spent cannot be argued with.
		for _, h := range []float64{
			fraction(snap.ProjectedTodayUSD, snap.DailyBudgetUSD),
			fraction(snap.SpentTodayUSD, snap.HardCeilingUSDPerDay),
		} {
			if h != unknownHeadroom && (a.CostHeadroom == unknownHeadroom || h < a.CostHeadroom) {
				a.CostHeadroom = h
			}
		}
	} else if snap.Accounting == "subscription" {
		a.Residual = "USD figures are API-equivalent estimates under subscription accounting (🎯T137) — token and load headroom decide"
	}

	a.TokenHeadroom = fraction(float64(snap.TokensUsed), float64(snap.TokensBudget))

	// Load headroom is the most saturated of the session bound and every
	// provider soft cap (🎯T325.2): one pinned provider is a real constraint
	// even when the fleet-wide count looks calm.
	loadUsed, loadLimit := float64(snap.ActiveSessions), float64(snap.MaxSessions)
	a.LoadHeadroom = fraction(loadUsed, loadLimit)
	for prov, capN := range snap.ProviderSoftCaps {
		// A published 0 is an unpublished cap, not an unlimited one (🎯T463):
		// judge it against the fallback rather than skipping the provider.
		h := fraction(float64(snap.ProviderLoad[strings.ToLower(strings.TrimSpace(prov))]), float64(pol.providerCap(capN)))
		if h != unknownHeadroom && (a.LoadHeadroom == unknownHeadroom || h < a.LoadHeadroom) {
			a.LoadHeadroom = h
		}
	}

	// The host is a load dimension too, and the one that actually runs out
	// first under fan-out (🎯T463): fold it into load headroom so the 🎯T359
	// ladder decides, and keep the separate figure for tracing.
	hostH, hostReason := hostHeadroom(snap, pol)
	if hostH != unknownHeadroom {
		a.HostHeadroom = hostH
		if a.LoadHeadroom == unknownHeadroom || hostH < a.LoadHeadroom {
			a.LoadHeadroom = hostH
		}
	}

	known := false
	for _, h := range []float64{a.CostHeadroom, a.TokenHeadroom, a.LoadHeadroom} {
		if h == unknownHeadroom {
			continue
		}
		if !known || h < a.Headroom {
			a.Headroom = h
			known = true
		}
	}
	if !known {
		a.Residual = strings.TrimSpace(a.Residual + " no budget or load bound configured — admission is slot-based only")
	}

	// Pressure is the worst of what the headroom says and what the 🎯T36
	// clamp already decided.
	switch {
	case snap.SpawnHalted:
		a.Pressure = PressureCritical
		a.Reasons = append(a.Reasons, "budget clamp has halted spawning (🎯T36)")
	case known && a.Headroom <= 0:
		a.Pressure = PressureCritical
		a.Reasons = append(a.Reasons, "no remaining headroom on the tightest budget dimension")
	case known && a.Headroom < pol.OwnerReserveFraction:
		a.Pressure = PressureTight
		a.Reasons = append(a.Reasons, fmt.Sprintf("headroom %.0f%% is inside the %.0f%% owner reserve",
			a.Headroom*100, pol.OwnerReserveFraction*100))
	case known && a.Headroom < pol.DegradeFraction:
		a.Pressure = PressureElevated
		a.Reasons = append(a.Reasons, fmt.Sprintf("headroom %.0f%% is below the %.0f%% degrade line",
			a.Headroom*100, pol.DegradeFraction*100))
	}
	// Name the host reading when it is what bound, so a deferral says "load
	// average 247 on 16 cores" rather than a bare percentage that reads like a
	// budget figure (🎯T463).
	if a.Pressure > PressureNormal && hostBound(a) && hostReason != "" {
		a.Reasons = append(a.Reasons, hostReason)
	}
	if ap := alertPressure(snap.HighestAlert); ap > a.Pressure {
		a.Pressure = ap
		a.Reasons = append(a.Reasons, fmt.Sprintf("cost alert level %q", strings.ToLower(strings.TrimSpace(snap.HighestAlert))))
	}
	a.OwnerOnly = a.Pressure == PressureCritical
	if a.OwnerOnly {
		a.Residual = strings.TrimSpace(a.Residual + " only owner-facing turns and open Build missions fit; all ambient background is skipped")
	}
	return a
}

// Admit decides one request against a snapshot, ignoring any other work in
// the same pass. Plan is the holistic form — prefer it when several requests
// compete for the same headroom.
func Admit(req Request, snap Snapshot, pol *Policy) Decision {
	return Plan([]Request{req}, snap, pol)[0]
}

// Plan ranks a queue of requests and decides them together: foreground work
// first and unconditionally, then background in priority order while slots
// and the background token allowance last. It is the holistic pass acceptance
// (2) asks for — the same queue always yields the same decisions.
func Plan(queue []Request, snap Snapshot, pol *Policy) []Decision {
	if pol == nil {
		pol = DefaultPolicy()
	}
	assessment := Assess(snap, pol)

	// Preserve the caller's order in the output while deciding in rank order.
	type item struct {
		req   Request
		index int
	}
	items := make([]item, 0, len(queue))
	for i, req := range queue {
		req.Class = NormalizeClass(string(req.Class))
		items = append(items, item{req: req, index: i})
	}
	sortByRank(items, func(it item) Class { return it.req.Class })

	inFlight := map[Class]int{}
	background := 0
	for c, n := range snap.RunningBackground {
		c = NormalizeClass(string(c))
		if c.Background() && n > 0 {
			inFlight[c] += n
			background += n
		}
	}
	// Background may spend down to the owner reserve and no further.
	tokensLeft := backgroundTokenAllowance(snap, pol)

	decisions := make([]Decision, len(queue))
	for _, it := range items {
		d := decide(it.req, snap, pol, assessment, inFlight, &background, &tokensLeft)
		decisions[it.index] = d
	}
	if pol.Disabled {
		for i := range decisions {
			decisions[i].Verdict = VerdictAdmit
			decisions[i].Tier = TierFull
			decisions[i].Reason = ReasonHeadroomOK
			decisions[i].Detail = "admission control disabled (🎯T36 clamp still applies)"
		}
	}
	return decisions
}

// backgroundTokenAllowance is what background work may still spend before it
// eats into the owner reserve. -1 means no token budget is configured.
func backgroundTokenAllowance(snap Snapshot, pol *Policy) int64 {
	if snap.TokensBudget <= 0 {
		return -1
	}
	reserve := int64(float64(snap.TokensBudget) * pol.OwnerReserveFraction)
	left := snap.TokensBudget - snap.TokensUsed - reserve
	if left < 0 {
		return 0
	}
	return left
}

// decide resolves one request, mutating the in-flight counters so later
// (lower-ranked) requests in the same pass see the slots this one took.
func decide(req Request, snap Snapshot, pol *Policy, a Assessment,
	inFlight map[Class]int, background *int, tokensLeft *int64) Decision {

	d := Decision{Class: req.Class, Name: req.Name, Pressure: a.Pressure}

	// Foreground work is never gated here. Owner turns in particular are the
	// thing every other rule exists to protect.
	if req.Class == ClassOwnerTurn {
		d.Verdict, d.Tier, d.Reason = VerdictAdmit, TierFull, ReasonOwnerPriority
		d.Detail = "owner-facing turns are never deferred for background work"
		return d
	}
	if req.Class == ClassBuildMission {
		if snap.SpawnHalted {
			d.Verdict, d.Reason = VerdictDefer, ReasonSpawnHalted
			d.Detail = "budget clamp has halted spawning (🎯T36); Build work waits for the clamp to clear"
			return d
		}
		d.Verdict, d.Tier, d.Reason = VerdictAdmit, TierFull, ReasonOwnerPriority
		d.Detail = "open Build missions outrank ambient background"
		return d
	}

	loadBearing := pol.isLoadBearing(req.Class)

	switch a.Pressure {
	case PressureCritical:
		d.Verdict, d.Reason = VerdictDefer, deferReason(a, ReasonCriticalOwnerOnly)
		d.Detail = "only owner and Build work fits: " + joinReasons(a.Reasons)
		return d
	case PressureTight:
		if !loadBearing {
			d.Verdict, d.Reason = VerdictDefer, deferReason(a, ReasonTightLoadBearing)
			d.Detail = "capacity tight: " + joinReasons(a.Reasons) + "; load-bearing background only"
			return d
		}
	}

	if pol.OwnerBusyDefersBackground && snap.OwnerActive && !loadBearing {
		d.Verdict, d.Reason = VerdictDefer, ReasonOwnerTurnActive
		d.Detail = "an owner turn is in flight; ambient work yields the seat (🎯T291)"
		return d
	}

	if pol.MaxConcurrentBackground > 0 && *background >= pol.MaxConcurrentBackground {
		d.Verdict, d.Reason = VerdictDefer, ReasonBackgroundSlots
		d.Detail = fmt.Sprintf("%d background cycles already in flight (bound %d)", *background, pol.MaxConcurrentBackground)
		return d
	}
	if capN := pol.maxForClass(req.Class); capN > 0 && inFlight[req.Class] >= capN {
		d.Verdict, d.Reason = VerdictDefer, ReasonClassSlots
		d.Detail = fmt.Sprintf("%s already has %d cycle(s) in flight (bound %d)", req.Class, inFlight[req.Class], capN)
		return d
	}
	if *tokensLeft >= 0 && req.EstTokens > 0 && req.EstTokens > *tokensLeft {
		d.Verdict, d.Reason = VerdictDefer, ReasonTokenReserve
		d.Detail = fmt.Sprintf("estimated %d tokens exceeds the %d left before the %.0f%% owner reserve",
			req.EstTokens, *tokensLeft, pol.OwnerReserveFraction*100)
		return d
	}

	// Admitted: take the slots and the tokens.
	inFlight[req.Class]++
	*background++
	if *tokensLeft >= 0 {
		*tokensLeft -= req.EstTokens
		if *tokensLeft < 0 {
			*tokensLeft = 0
		}
	}

	if a.Pressure == PressureElevated && req.Degradable && !loadBearing {
		d.Verdict, d.Tier, d.Reason = VerdictDegrade, TierReduced, ReasonElevatedDegrade
		d.Detail = "capacity elevated: " + joinReasons(a.Reasons) + "; run a reduced pass"
		return d
	}
	d.Verdict, d.Tier, d.Reason = VerdictAdmit, TierFull, ReasonHeadroomOK
	d.Detail = fmt.Sprintf("headroom %.0f%% at pressure %s", a.Headroom*100, a.Pressure)
	return d
}

// Preempt decides which in-flight background work to stop so foreground work
// keeps its room. It never touches foreground classes, and never cancels work
// that declared itself non-preemptible — that work is reported by the caller
// as residual rather than killed mid-write.
//
// At PressureCritical every preemptible background cycle is cancelled; at
// PressureTight only the non-load-bearing ones are.
func Preempt(running []Running, snap Snapshot, pol *Policy) []Decision {
	if pol == nil {
		pol = DefaultPolicy()
	}
	if pol.Disabled {
		return nil
	}
	a := Assess(snap, pol)
	if a.Pressure < PressureTight {
		return nil
	}

	// Cancel from the bottom of the ranking up: the cheapest thing to lose
	// goes first.
	ordered := append([]Running(nil), running...)
	sortByRank(ordered, func(r Running) Class { return NormalizeClass(string(r.Class)) })
	var out []Decision
	for i := len(ordered) - 1; i >= 0; i-- {
		r := ordered[i]
		class := NormalizeClass(string(r.Class))
		if class.Foreground() || !r.Preemptible {
			continue
		}
		if a.Pressure == PressureTight && pol.isLoadBearing(class) {
			continue
		}
		out = append(out, Decision{
			Class: class, Name: r.Name, Verdict: VerdictCancel,
			Reason:   ReasonPreempted,
			Detail:   "stopped to keep capacity for owner and Build work: " + joinReasons(a.Reasons),
			Pressure: a.Pressure,
		})
	}
	return out
}

// joinReasons renders assessment reasons as one clause.
func joinReasons(reasons []string) string {
	if len(reasons) == 0 {
		return "capacity policy"
	}
	return strings.Join(reasons, "; ")
}
