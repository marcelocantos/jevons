// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
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

// PlanAction is one sweep decision: migrate to To, or park when To is empty.
type PlanAction struct {
	Name   string
	From   string
	To     string
	Reason string
	// Author names who resolved the dest (🎯T691). Product placement is
	// "claudia"; a prompt-level choice is a different decision.
	Author string
}

// DestCand is one published backend plus fleet load for PickPlanDest.
type DestCand struct {
	Provider string
	Backend  Backend
	Load     int
}

// WeeklyBandOf classifies one backend's weekly window at now.
// The verdict is claudia.ClassifyPlan — jevons does not re-derive it (🎯T691).
func WeeklyBandOf(be Backend, now time.Time, th Thresholds) WeeklyBand {
	return WeeklyBand(claudia.ClassifyPlan(backendToPlanUsage(be), now, claudiaThresholdsPtr(th)).Weekly)
}

// BandOfWindow classifies a single window at now.
//
// Split out of WeeklyBandOf so the same verdict can be attached to every
// window the API serves (🎯T610), rather than only to the backend's primary
// one. One rule, one implementation, reached from both paths — the whole
// point of serving the band is that nothing downstream re-derives it.
func BandOfWindow(w Window, now time.Time, th Thresholds) WeeklyBand {
	return WeeklyBand(claudia.ClassifyWindow(windowToClaudia(w), now, thresholdsToClaudia(th)))
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
	return SessionStatus(claudia.ClassifyPlan(backendToPlanUsage(be), time.Time{}, claudiaThresholdsPtr(th)).Session)
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

// MigrateOff reports that running seats on this provider must leave.
// Same bar as claudia.ShouldVacate (🎯T691).
func MigrateOff(be Backend, now time.Time, th Thresholds) bool {
	return claudia.ShouldVacate(backendToPlanUsage(be), now, claudiaThresholdsPtr(th))
}

// DestEligible reports a published dest that may receive work (🎯T693):
// locked, under, or ok. hot and ahead remain never-destinations even when
// claudia.HasAvailableTokens still says the account has tokens.
func DestEligible(be Backend, now time.Time, th Thresholds) bool {
	u := backendToPlanUsage(be)
	if u.Status != claudia.PlanUsageAvailable {
		return false
	}
	if !claudia.HasAvailableTokens(u, now, claudiaThresholdsPtr(th)) {
		return false
	}
	return claudia.IsDestBand(claudia.PlanBand(WeeklyBandOf(be, now, th)))
}

// PickPlanDest chooses dest through claudia.Resolve (🎯T691). ok is false
// when no published dest is token-eligible.
func PickPlanDest(cands []DestCand, now time.Time, th Thresholds) (string, bool) {
	pick, err := ResolveDest(context.Background(), cands, "", now, th)
	if err != nil || pick.Provider == "" {
		return "", false
	}
	return strings.ToLower(string(pick.Provider)), true
}

// OverseerNames is the set of agents whose Purpose is overseer.
// Used with PlanMigrateExempt so a PO is identified by parentage, not name.
func OverseerNames(agents []AgentRef) map[string]bool {
	out := map[string]bool{}
	for _, a := range agents {
		if strings.EqualFold(strings.TrimSpace(a.Purpose), "overseer") {
			if n := strings.TrimSpace(a.Name); n != "" {
				out[n] = true
			}
		}
	}
	return out
}

// PlanMigrateExempt is true for control-plane seats T390.1.5 must not
// bounce (🎯T517): the overseer itself, and any agent whose Parent is an
// overseer (stratum-1 product owners). purpose=work on a PO does not
// enroll it. Workers parented to a PO stay eligible.
func PlanMigrateExempt(a AgentRef, overseers map[string]bool) bool {
	if strings.EqualFold(strings.TrimSpace(a.Purpose), "overseer") {
		return true
	}
	parent := strings.TrimSpace(a.Parent)
	return parent != "" && overseers[parent]
}

// PlanActions lists migrate/park steps for seats on hot or exhausted
// providers. Overseer purpose and aside seats are skipped (🎯T517, 🎯T543).
// To is empty when dest is empty (park).
func PlanActions(snap Snapshot, agents []AgentRef, now time.Time, th Thresholds) []PlanAction {
	view := CockpitSnapshot(snap)
	byProv := map[string]Backend{}
	var cands []DestCand
	load := map[string]int{}
	for _, a := range agents {
		p := strings.ToLower(strings.TrimSpace(a.Provider))
		if p != "" {
			load[p]++
		}
	}
	for _, be := range view.Backends {
		p := strings.ToLower(strings.TrimSpace(be.Provider))
		byProv[p] = be
		cands = append(cands, DestCand{Provider: p, Backend: be, Load: load[p]})
	}
	dest, destOK := PickPlanDest(cands, now, th)
	overseers := OverseerNames(agents)
	var out []PlanAction
	for _, a := range agents {
		if strings.EqualFold(strings.TrimSpace(a.Purpose), "aside") {
			continue
		}
		if PlanMigrateExempt(a, overseers) {
			continue
		}
		from := strings.ToLower(strings.TrimSpace(a.Provider))
		be, ok := byProv[from]
		if !ok || !MigrateOff(be, now, th) {
			continue
		}
		to := ""
		reason := migrateOffReason(be, now, th)
		if destOK && dest != from {
			to = dest
		} else {
			reason += "; no eligible dest — park"
		}
		out = append(out, PlanAction{
			Name: a.Name, From: from, To: to, Reason: reason,
			Author: claudia.DecisionAuthor,
		})
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


