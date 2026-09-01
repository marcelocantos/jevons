// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"math"
	"strings"
	"time"
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
}

// DestCand is one published backend plus fleet load for PickPlanDest.
type DestCand struct {
	Provider string
	Backend  Backend
	Load     int
}

// WeeklyBandOf classifies one backend's weekly window at now.
func WeeklyBandOf(be Backend, now time.Time, th Thresholds) WeeklyBand {
	if IsExhaustedReason(be.Reason) {
		return BandExhausted
	}
	if !be.Available() {
		return BandUnpublished
	}
	w, ok := be.PrimaryAllowanceWindow()
	if !ok {
		return BandUnpublished
	}
	return BandOfWindow(w, now, th)
}

// BandOfWindow classifies a single window at now.
//
// Split out of WeeklyBandOf so the same verdict can be attached to every
// window the API serves (🎯T610), rather than only to the backend's primary
// one. One rule, one implementation, reached from both paths — the whole
// point of serving the band is that nothing downstream re-derives it.
func BandOfWindow(w Window, now time.Time, th Thresholds) WeeklyBand {
	if w.RemainingPercent != nil && *w.RemainingPercent <= 0 {
		return BandExhausted
	}
	used := usedPercent(w)
	rtp, hasTime := remainingTimePercent(w, now)
	if !hasTime || used == nil {
		return BandOK
	}
	elapsed := 100 - rtp
	// 🎯T596: colour answers "how much must we change what we are doing",
	// not "where do we end up if nothing changes".
	return BandOfPressure(Pressure(*used, elapsed, th), th)
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
	if IsExhaustedReason(be.Reason) {
		return SessionExhausted
	}
	w, ok := be.Window(WindowSession)
	if !ok || w.RemainingPercent == nil {
		return SessionUnpublished
	}
	if *w.RemainingPercent <= 0 {
		return SessionExhausted
	}
	if *w.RemainingPercent <= th.LowRemainingPercent {
		return SessionLow
	}
	return SessionOK
}

// MintIneligible reports that omit-provider mint must not land here
// (weekly ahead/hot/exhausted, or session remaining-low / exhausted).
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

// MigrateOff reports that running seats on this provider must leave
// (weekly hot/exhausted, or session 0%/429). Session remaining-low does
// not bounce the fleet — that window is only a mint veto (🎯T390.1.5.1).
func MigrateOff(be Backend, now time.Time, th Thresholds) bool {
	if SessionStatusOf(be, th) == SessionExhausted {
		return true
	}
	switch WeeklyBandOf(be, now, th) {
	case BandHot, BandExhausted:
		return true
	default:
		return false
	}
}

// DestEligible reports a published weekly that may receive work, and whose
// session is not remaining-low or exhausted (🎯T390.1.5.1). Healthy weekly
// with a dead session is ineligible until the session resets.
func DestEligible(be Backend, now time.Time, th Thresholds) bool {
	switch SessionStatusOf(be, th) {
	case SessionLow, SessionExhausted:
		return false
	}
	switch WeeklyBandOf(be, now, th) {
	case BandOK, BandUnder, BandLocked:
		return true
	default:
		return false
	}
}

// PickPlanDest chooses dest: locked, then under, then ok; within a band,
// least Load. ok is false when no dest is eligible.
func PickPlanDest(cands []DestCand, now time.Time, th Thresholds) (string, bool) {
	type scored struct {
		prov     string
		pressure float64
		load     int
	}
	var best *scored
	for _, c := range cands {
		if !DestEligible(c.Backend, now, th) {
			continue
		}
		b := WeeklyBandOf(c.Backend, now, th)
		if b == BandHot || b == BandAhead {
			continue // burning too fast is never a destination
		}
		// Rank by headroom, not by which colour the band happens to be
		// (🎯T596). Routing and colour answer different questions: colour
		// asks how hard the owner must correct, and under the pressure
		// model it deliberately stays quiet about small deviations, since
		// leaving allowance unspent is the cheaper failure. Routing asks
		// which backend has the most slack — and 16% behind pace is still
		// more slack than dead level, whether or not it is worth a colour.
		// Ranking on the band identity made those two move together, so
		// widening the waste vertex silently changed where work went.
		s := scored{
			prov:     strings.ToLower(strings.TrimSpace(c.Provider)),
			pressure: destPressure(c.Backend, now, th),
			load:     c.Load,
		}
		if s.prov == "" {
			s.prov = strings.ToLower(strings.TrimSpace(c.Backend.Provider))
		}
		// A meaningful headroom gap decides it; otherwise load breaks the
		// tie, so two comparable backends still balance by load rather
		// than by a rounding difference in pressure.
		if best == nil || s.pressure < best.pressure-destPressureIndifference ||
			(s.pressure <= best.pressure+destPressureIndifference && s.load < best.load) {
			cp := s
			best = &cp
		}
	}
	if best == nil || best.prov == "" {
		return "", false
	}
	return best.prov, true
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
		out = append(out, PlanAction{Name: a.Name, From: from, To: to, Reason: reason})
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
	remaining := 100 - used
	timeLeft := 100 - elapsed
	if timeLeft <= 0 {
		timeLeft = 0.0001 // the last instant, not a division by zero
	}
	if remaining <= 0 {
		return math.Inf(1) // spent: no correction can fix it
	}
	// Zero means "unset, use the default", consistent with every other
	// vertex here and robust to a config that omits the key. The cost is
	// that zero cannot express "no prior at all"; pass a negligible k for
	// that (a test isolating the prior's effect is the only caller that
	// wants it).
	k := th.ShrinkPriorK
	if k <= 0 {
		k = DefaultShrinkPriorK
	}
	lambda := k * (timeLeft / 100)
	current := (used + lambda) / (elapsed + lambda)
	required := remaining / timeLeft
	return math.Log(current / required)
}

// BandOfPressure maps pressure onto the owner-visible bands (🎯T596).
// The scale is deliberately asymmetric: running dry costs more than
// leaving allowance unspent, so the waste side is given more room before
// it says anything.
func BandOfPressure(p float64, th Thresholds) WeeklyBand {
	red, amber := th.PanicRedLn, th.PanicAmberLn
	locked, under := th.WasteLockedLn, th.WasteUnderLn
	if red <= 0 {
		red = DefaultPanicRedLn
	}
	if amber <= 0 {
		amber = DefaultPanicAmberLn
	}
	if locked >= 0 {
		locked = DefaultWasteLockedLn
	}
	if under >= 0 {
		under = DefaultWasteUnderLn
	}
	switch {
	case p >= red:
		return BandHot
	case p >= amber:
		return BandAhead
	case p <= locked:
		return BandLocked
	case p <= under:
		return BandUnder
	default:
		return BandOK
	}
}

// destPressureIndifference is the headroom gap below which two backends
// are treated as equivalent and load decides. Without it, routing would
// chase noise in the pressure estimate.
const destPressureIndifference = 0.05

// destPressure is the window pressure used for routing: lower means more
// headroom. A backend with no usable weekly window sorts as level rather
// than as maximally attractive, so missing data never wins a race.
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
