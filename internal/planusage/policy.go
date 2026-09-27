// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// WeeklyBand is the daemon policy class for one provider's weekly window.
// Names match the ticker's discrete labels so paint and policy cohere;
// they are computed here, not read from CSS or classifyPace.
type WeeklyBand string

const (
	BandOK          WeeklyBand = "ok"
	BandAhead       WeeklyBand = "ahead"
	BandHot         WeeklyBand = "hot"
	BandUnder       WeeklyBand = "under"
	BandLocked      WeeklyBand = "locked"
	BandExhausted   WeeklyBand = "exhausted"
	BandUnpublished WeeklyBand = "unpublished"
)

// AgentRef is a running seat the sweep can migrate or park.
type AgentRef struct {
	Name     string
	Provider string
	Purpose  string
	Parent   string
}

// PlanAction is Claudia's per-seat verdict. Only migrate and park are actions
// the sweep executes; stay and defer are reported for inspection.
type PlanAction struct {
	Name   string
	From   string
	To     string
	Model  string
	Action claudia.SeatPlacementAction
	Reason string
	// Execution is the host's result of acting on Claudia's verdict. A
	// placement choice is not proof that a migration finished.
	Execution string
	Failure   string
	// Author names who resolved the dest (🎯T691). Product placement is
	// "claudia"; a prompt-level choice is a different decision.
	Author string
}

// DestCand is one published backend plus fleet load for PickPlanDest.
type DestCand struct {
	Provider string
	Backend  Backend
	Load     int
	// Cap is the published session soft cap for this dest (🎯T715). 0 means
	// unpublished — pickDest does not skip on load. A dest at Load >= Cap
	// is not a destination even when the plan band is eligible.
	Cap int
}

// WeeklyBandOf classifies one backend's weekly window at now.
// The verdict is claudia.ClassifyPlan — jevons does not re-derive it (🎯T691).
// Ticker waste colour is BandOfWindow (🎯T390.1.1), not this dest/policy band.
func WeeklyBandOf(be Backend, now time.Time, th Thresholds) WeeklyBand {
	return WeeklyBand(classifyPlan(be, now, th).Weekly)
}

// BandOfWindow classifies a single window at now for the served ticker band.
//
// Overspend (ahead/hot/exhausted) is claudia.ClassifyWindow (🎯T691 / T596).
// Waste (under/locked) is T390.1.1 arithmetic so late-window leftover paints
// as locked surplus, not continuation-blue, and session bars never paint
// underutilization. Dest/mint still read WeeklyBandOf.
func BandOfWindow(w Window, now time.Time, th Thresholds) WeeklyBand {
	band := WeeklyBand(claudia.ClassifyWindow(windowToClaudia(w), now, thresholdsToClaudia(th)))
	return applyWeeklyWaste(band, w, now, th)
}

func wasteWindow(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case WindowWeekly, WindowMonthly, WindowModelWeekly:
		return true
	default:
		return false
	}
}

// applyWeeklyWaste is 🎯T390.1.1: session bars never paint underutilization;
// weekly/monthly continuation leftover is under (blue) past warmup; locked
// surplus remaining−1.5×time_left is locked (purple) and outranks blue.
// Overspend bands from the pressure model are left alone.
func applyWeeklyWaste(band WeeklyBand, w Window, now time.Time, th Thresholds) WeeklyBand {
	switch band {
	case BandHot, BandAhead, BandExhausted, BandUnpublished:
		return band
	}
	name := strings.ToLower(strings.TrimSpace(w.Name))
	if name == WindowSession {
		if band == BandUnder || band == BandLocked {
			return BandOK
		}
		return band
	}
	if !wasteWindow(w.Name) {
		return band
	}
	used := usedPercent(w)
	rtp, hasTime := remainingTimePercent(w, now)
	if used == nil || !hasTime {
		return band
	}
	rem := 100 - *used
	if w.RemainingPercent != nil {
		rem = *w.RemainingPercent
	}
	elapsed := 100 - rtp
	hot := th.HotRatio
	if hot <= 0 {
		hot = 1.5
	}
	lockedThresh := th.LockedWastePercent
	if lockedThresh <= 0 {
		lockedThresh = 15
	}
	underThresh := th.UnderWastePercent
	if underThresh <= 0 {
		underThresh = 15
	}
	warmup := th.WarmupElapsedPercent
	if warmup <= 0 {
		warmup = 5
	}
	locked := math.Max(0, rem-hot*rtp)
	if locked >= lockedThresh {
		return BandLocked
	}
	continuation := 0.0
	if elapsed > 0 {
		continuation = math.Max(0, 100-(*used/elapsed)*100)
	}
	if elapsed >= warmup && continuation >= underThresh {
		return BandUnder
	}
	if band == BandUnder || band == BandLocked {
		return BandOK
	}
	return band
}

// SessionStatus is the session-window eligibility class (🎯T390.1.5.1).
// Unlike WeeklyBand, session does not use leftover-vs-time pace ranking —
// only remaining % and 429/rate_limit. An unpublished session is not a veto.
type SessionStatus string

const (
	SessionOK          SessionStatus = "ok"
	SessionLow         SessionStatus = "low"         // remaining ≤ LowRemainingPercent, > 0
	SessionExhausted   SessionStatus = "exhausted"   // 0% remaining or 429
	SessionUnpublished SessionStatus = "unpublished" // no session figure — not a veto
)

// SessionStatusOf classifies one backend's session window for mint/migrate
// eligibility. Same snapshot numbers the ticker paints; no JS classifyPace.
func SessionStatusOf(be Backend, th Thresholds) SessionStatus {
	return SessionStatus(classifyPlan(be, time.Time{}, th).Session)
}

// MintIneligible reports a published dest that must not receive new work
// (session remaining-low / exhausted, or weekly ahead/hot/exhausted).
// Unpublished is not spent (🎯T677).
func MintIneligible(be Backend, now time.Time, th Thresholds) bool {
	switch SessionStatusOf(be, th) {
	case SessionLow, SessionExhausted:
		return true
	}
	switch WeeklyBandOf(be, now, th) {
	case BandAhead, BandHot, BandExhausted:
		return true
	default:
		return false
	}
}

// MigrateOff reports Claudia's decision that running seats on this provider
// should leave (🎯T691).
func MigrateOff(be Backend, now time.Time, th Thresholds) bool {
	return claudia.ShouldVacate(backendToPlanUsage(be), now, claudiaThresholdsPtr(th))
}

// DestEligible reports a published dest that may receive work (🎯T693):
// locked, under, or ok. hot and ahead remain never-destinations even
// when claudia.HasAvailableTokens still says the account has tokens.
// Eligibility only — ranking is claudia.Resolve.
func DestEligible(be Backend, now time.Time, th Thresholds) bool {
	u := backendToPlanUsage(be)
	if u.Status != claudia.PlanUsageAvailable {
		return false
	}
	if !claudia.HasAvailableTokens(u, now, claudiaThresholdsPtr(th)) {
		return false
	}
	return claudia.IsDestBand(claudia.ClassifyPlan(u, now, claudiaThresholdsPtr(th)).Weekly)
}

// PickPlanDest chooses dest through claudia.Resolve (🎯T691 / 🎯T693).
// ok is false when no published dest is dest-band eligible. Jevons does
// not rank the candidates.
func PickPlanDest(cands []DestCand, now time.Time, th Thresholds) (string, bool) {
	pick, err := ResolveDest(context.Background(), cands, "", now, th)
	if err != nil || pick.Provider == "" {
		return "", false
	}
	return strings.ToLower(string(pick.Provider)), true
}

// PlanDecisions reports Claudia's placement verdict for every non-aside seat,
// including reasoned stays and deferrals. It does not move a seat.
func PlanDecisions(snap Snapshot, agents []AgentRef, now time.Time, th Thresholds, destinations ...DestCand) []PlanAction {
	view := CockpitSnapshot(snap)
	var usage []claudia.PlanUsage
	for _, be := range view.Backends {
		usage = append(usage, backendToPlanUsage(be))
	}
	var exclusions []claudia.Provider
	var capped []string
	for _, c := range destinations {
		if !destAtSessionCap(c) {
			continue
		}
		p := c.Provider
		if p == "" {
			p = c.Backend.Provider
		}
		exclusions = append(exclusions, claudia.PlanProvider(claudia.Provider(p)))
		capped = append(capped, p)
	}
	out := make([]PlanAction, 0, len(agents))
	for _, a := range agents {
		if strings.EqualFold(strings.TrimSpace(a.Purpose), "aside") {
			continue
		}
		decision, err := claudia.ResolveSeatPlacement(context.Background(), &claudia.SeatPlacementArgs{
			CurrentProvider: claudia.Provider(a.Provider), Usage: usage, Now: now,
			Thresholds: claudiaThresholdsPtr(th), ExcludeProviders: exclusions,
		})
		if err != nil {
			out = append(out, PlanAction{Name: a.Name, From: a.Provider, Action: claudia.SeatDefer,
				Reason: err.Error(), Author: claudia.DecisionAuthor})
			continue
		}
		if decision.Action == claudia.SeatPark && len(capped) > 0 {
			decision.Reason += "; fleet session cap reached on " + strings.Join(capped, ", ")
		}
		out = append(out, PlanAction{
			Name: a.Name, From: string(decision.From), To: string(decision.Pick.Provider),
			Model: decision.Pick.Model, Action: decision.Action,
			Reason: decision.Reason, Author: decision.Author,
		})
	}
	return out
}

// PlanActions lists migrate/park steps for seats whose own provider is
// weekly-hot or exhausted (🎯T850). The overseer and a stratum-1 PO are
// the same as any other seat: they move when their provider is hot, and
// they stay when MigrateOff is false. There is no control-plane
// exemption. Aside seats stay out (🎯T543). To is empty when dest is
// empty (park).
func PlanActions(snap Snapshot, agents []AgentRef, now time.Time, th Thresholds, destinations ...DestCand) []PlanAction {
	var out []PlanAction
	for _, decision := range PlanDecisions(snap, agents, now, th, destinations...) {
		if decision.Action == claudia.SeatMigrate || decision.Action == claudia.SeatPark {
			out = append(out, decision)
		}
	}
	return out
}

func migrateOffReason(be Backend, now time.Time, th Thresholds) string {
	sess := SessionStatusOf(be, th) == SessionExhausted
	week := false
	switch WeeklyBandOf(be, now, th) {
	case BandHot, BandExhausted:
		week = true
	}
	switch {
	case sess && week:
		return "session or weekly exhausted"
	case sess:
		return "session exhausted"
	default:
		return "weekly hot or exhausted"
	}
}

// dampedBurn is used/elapsed with the additive damping λ on both terms
// (🎯T390.1.6.1) — see Thresholds.DampLambdaPercent for why. The ticker's
// classifyPace applies the same formula from the same served λ.
func dampedBurn(used, elapsed, lambda float64) float64 {
	if lambda < 0 {
		lambda = 0
	}
	return (used + lambda) / (elapsed + lambda)
}

func usedPercent(w Window) *float64 {
	if w.UsedPercent != nil {
		return w.UsedPercent
	}
	if w.RemainingPercent != nil {
		u := 100 - *w.RemainingPercent
		return &u
	}
	return nil
}

func remainingTimePercent(w Window, now time.Time) (float64, bool) {
	if w.ResetsAt == nil {
		return 0, false
	}
	lim := int64(0)
	if w.LimitWindowSeconds != nil && *w.LimitWindowSeconds > 0 {
		lim = *w.LimitWindowSeconds
	} else if w.Name == WindowWeekly {
		lim = DefaultWeeklyWindowSeconds
	} else if w.Name == WindowMonthly {
		lim = DefaultMonthlyWindowSeconds
	} else if w.Name == WindowSession {
		lim = DefaultSessionWindowSeconds
	} else {
		return 0, false
	}
	rem := w.ResetsAt.Sub(now).Seconds()
	pct := 100 * rem / float64(lim)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct, true
}

// burningFastReachable gates the ahead / hot verdicts (🎯T591, 🎯T595).
//
// Two independent ways to be over-confident about a window, so two guards:
// the overspend must be real rather than a rounding step (T591), and
// enough of the window must have passed for a rate to mean anything
// (T595) — unless the absolute spend is already alarming on its own, which
// keeps a genuine early blowout red.
func burningFastReachable(used, elapsed float64, th Thresholds) bool {
	if used-elapsed <= th.AheadMarginPercent {
		return false
	}
	warmup := th.WarmupElapsedPercent
	early := th.EarlyAlarmUsedPercent
	return elapsed >= warmup || (early > 0 && used >= early)
}

// Pressure is the log of the correction this window demands: how far the
// rate we appear to be running sits from the rate that lands exactly on
// 100% used at rollover (🎯T596).
//
//	current  ≈ (used + λ) / (elapsed + λ)     rate so far, in nominal units
//	required =  remaining / timeLeft          rate that finishes exactly level
//	pressure =  ln(current / required)
//
// Positive means burning too fast — the allowance runs out early. Negative
// means the opposite failure the owner cares about equally: time runs out
// with allowance unspent. Zero is on track, whatever has happened so far.
//
// The prior λ shrinks the current-rate estimate toward nominal, and its
// strength scales with the time left rather than being a constant. Early
// in a window a deviation is both weak evidence (a tiny sample: a
// five-minute fan-out sprint is not a policy) and cheap to correct (the
// runway is long); late it is strong evidence and cannot be corrected.
// Those two move together, which is why one decaying prior does both jobs
// instead of the constant λ plus the T591 margin plus the T595 warmup —
// three patches on a statistic that was measuring the wrong thing.
//
// Being a ratio of two well-conditioned quantities, this is stable at the
// start of a window, where used/elapsed divides one near-zero number by
// another and produced the red bar on a 94%-remaining week.
func Pressure(used, elapsed float64, th Thresholds) float64 {
	return claudia.Pressure(used, elapsed, thresholdsToClaudia(th))
}

// BandOfPressure maps pressure onto the owner-visible bands (🎯T596).
// The scale is deliberately asymmetric: running dry costs more than
// leaving allowance unspent, so the waste side is given more room before
// it says anything.
func BandOfPressure(p float64, th Thresholds) WeeklyBand {
	return WeeklyBand(claudia.BandOfPressure(p, thresholdsToClaudia(th)))
}

// destPressure is the window pressure used to explain slack ranking.
// Lower means more headroom. A backend with no usable weekly window
// sorts as level rather than as maximally attractive.
func destPressure(be Backend, now time.Time, th Thresholds) float64 {
	w, ok := be.PrimaryAllowanceWindow()
	if !ok {
		return 0
	}
	used := usedPercent(w)
	rtp, hasTime := remainingTimePercent(w, now)
	if used == nil || !hasTime {
		return 0
	}
	return Pressure(*used, 100-rtp, th)
}
