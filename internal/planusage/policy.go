// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
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
	w, ok := be.Window(WindowWeekly)
	if !ok {
		return BandUnpublished
	}
	if w.RemainingPercent != nil && *w.RemainingPercent <= 0 {
		return BandExhausted
	}
	used := usedPercent(w)
	rtp, hasTime := remainingTimePercent(w, now)
	if !hasTime || used == nil {
		return BandOK
	}
	elapsed := 100 - rtp
	if elapsed < th.WarmupElapsedPercent {
		return BandOK
	}
	if elapsed <= 0 {
		return BandOK
	}
	burn := dampedBurn(*used, elapsed, th.DampLambdaPercent)
	if burn > th.HotRatio {
		return BandHot
	}
	if burn > th.AheadRatio {
		return BandAhead
	}
	locked := 0.0
	if w.RemainingPercent != nil {
		locked = *w.RemainingPercent - th.HotRatio*rtp
		if locked < 0 {
			locked = 0
		}
	}
	if locked >= th.LockedWastePercent {
		return BandLocked
	}
	cont := 0.0
	if elapsed > 0 {
		cont = 100 - (*used/elapsed)*100
		if cont < 0 {
			cont = 0
		}
	}
	if cont >= th.UnderWastePercent {
		return BandUnder
	}
	return BandOK
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
		prov string
		rank int
		load int
	}
	var best *scored
	for _, c := range cands {
		if !DestEligible(c.Backend, now, th) {
			continue
		}
		b := WeeklyBandOf(c.Backend, now, th)
		rank := 3
		switch b {
		case BandLocked:
			rank = 1
		case BandUnder:
			rank = 2
		case BandOK:
			rank = 3
		default:
			continue
		}
		s := scored{prov: strings.ToLower(strings.TrimSpace(c.Provider)), rank: rank, load: c.Load}
		if s.prov == "" {
			s.prov = strings.ToLower(strings.TrimSpace(c.Backend.Provider))
		}
		if best == nil || s.rank < best.rank || (s.rank == best.rank && s.load < best.load) {
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
// providers. Overseer purpose is skipped. To is empty when dest is empty
// (park).
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
