// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
)

// SeatAction is Claudia's placement verb for one seat.
type SeatAction string

const (
	SeatStay    SeatAction = "stay"
	SeatDefer   SeatAction = "defer"
	SeatPark    SeatAction = "park"
	SeatMigrate SeatAction = "migrate"
)

// WeeklyBand is the daemon policy class for one provider's weekly window.
// Names match the ticker's discrete labels so paint and policy cohere;
// they are Claudia's verdict (🎯T1072), never re-derived in the cockpit.
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
	Name             string
	Provider         string
	Purpose          string
	Parent           string
	PreferProvider   claudia.Provider
	AllowedProviders []claudia.Provider
	// AllowNone is an explicit empty destination allow-list. A nil
	// AllowedProviders means every published dest is eligible.
	AllowNone        bool
	ExcludeProviders []claudia.Provider
	// RecoverPlanPark asks for a fresh destination even when the old provider
	// no longer vacates. Only the host can authorize lifting its own park.
	RecoverPlanPark bool
}

// PlanAction is Claudia's per-seat verdict. Only migrate and park are actions
// the sweep executes; stay and defer are reported for inspection.
type PlanAction struct {
	Name   string
	From   string
	To     string
	Model  string
	Action SeatAction
	Reason string
	// Execution is the host's result of acting on Claudia's verdict. A
	// placement choice is not proof that a migration finished.
	Execution string
	Failure   string
	// FailureCause is the typed provider cause of this result. Empty means
	// the failure arrived as a sentence. A sentence is not a cause.
	FailureCause string `json:"FailureCause,omitempty"`
	// ReauthAvailable is an owner action for a failed destination login.
	// The server decorates the read-only decision; Claudia owns recovery.
	ReauthAvailable bool
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
// The verdict is claudia.ClassifyWindow: the owner-tuned Spend Pressure Map
// (🎯T641), ln(current/required) against the amber/red and under/locked
// vertices, for overspend and leftover alike (🎯T1072). The only thing this
// adds is that a session bar never paints leftover. Dest/mint read
// WeeklyBandOf.
//
// 🎯T1072: this used to recompute leftover with fixed-15% arithmetic
// (🎯T390.1.1) and demote any under/locked that fell short of it to ok.
// A fixed share of the whole window is the wrong yardstick at the end of a
// window: on 2026-10-14 Cursor had 8.3% of the month left with 2.2% of it to
// go — pressure −1.38, under — and painted green. The pressure model
// already weighs leftover against the time left to burn it.
func BandOfWindow(w Window, now time.Time, th Thresholds) WeeklyBand {
	band := WeeklyBand(claudia.ClassifyWindow(windowToClaudia(w), now, thresholdsToClaudia(th)))
	if strings.EqualFold(strings.TrimSpace(w.Name), WindowSession) && (band == BandUnder || band == BandLocked) {
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
// eligibility. Same snapshot numbers the ticker paints; the cockpit classifies nothing.
func SessionStatusOf(be Backend, th Thresholds) SessionStatus {
	return SessionStatus(classifyPlan(be, time.Time{}, th).Session)
}

// MintIneligible reports a published dest that must not receive new work
// (session remaining-low / exhausted, or weekly ahead/hot/exhausted).
// Unpublished is not spent (🎯T677).
func MintIneligible(be Backend, now time.Time, th Thresholds) bool {
	if be.Override != nil {
		return !IsDestBandOverride(be.Override.Band)
	}
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
	if be.Override != nil {
		return !IsDestBandOverride(be.Override.Band) // 🎯T948
	}
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
	if be.Override != nil {
		return IsDestBandOverride(be.Override.Band) // 🎯T948
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
		exclusions = append(exclusions, cli.PlanProvider(claudia.Provider(p)))
		capped = append(capped, p)
	}
	// 🎯T948: a seat on a plan the owner has put in a dest band stays,
	// whatever the readings say. Claudia classifies from the readings alone.
	held := map[string]*Override{}
	for _, be := range snap.Backends {
		if be.Override != nil && IsDestBandOverride(be.Override.Band) {
			held[strings.ToLower(be.Provider)] = be.Override
		}
	}
	out := make([]PlanAction, 0, len(agents))
	for _, a := range agents {
		if strings.EqualFold(strings.TrimSpace(a.Purpose), "aside") {
			continue
		}
		plan := strings.ToLower(string(claudia.PlanProvider(claudia.Provider(a.Provider))))
		if ov := held[plan]; ov != nil && !a.RecoverPlanPark {
			out = append(out, PlanAction{Name: a.Name, From: plan, Action: SeatStay,
				Reason: "owner override (" + string(ov.Band) + "): " + ov.Reason, Author: "owner"})
			continue
		}
		seatExclusions := append([]claudia.Provider(nil), exclusions...)
		seatExclusions = append(seatExclusions, a.ExcludeProviders...)
		decision := resolveSeatPlacement(a, usage, now, th, seatExclusions)
		if decision.Action == SeatPark && len(capped) > 0 {
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

// seatDecision is one placement verdict: stay / defer / park / migrate,
// the same split the sweep already tests. It is the last 🎯T1008
// stand-in in this package: claudia.ResolveSeatPlacement (pin v0.52.0)
// makes the same decision over an existing seat, but its
// SeatPlacementArgs has no input for AgentRef.RecoverPlanPark (a
// plan-policy-parked seat being reconsidered: the source stays a
// candidate and a non-vacating source is not a stay) or AgentRef.AllowNone
// (an explicit park with no destination), both of which the sweep relies
// on. The Claudia API this waits for is claudia/T193; once
// SeatPlacementArgs carries those two inputs this split collapses to one
// ResolveSeatPlacement call. Everything else here is already Claudia's:
// the vacate test, the allowed/excluded/prefer inputs, and the
// ahead/hot/exhausted veto (ModelPredicates.Background).
type seatDecision struct {
	Action SeatAction
	From   claudia.Provider
	Pick   claudia.ModelPick
	Reason string
	Author string
}

// resolveSeatPlacement leaves a seat that is not vacating, defers when
// the feed names no other provider, parks an explicit allow-none (or a
// resolve with no eligible dest), and otherwise migrates through
// claudia.Resolve with Background set, which is Claudia's own
// ahead / hot / exhausted veto on destinations.
func resolveSeatPlacement(a AgentRef, usage []claudia.PlanUsage, now time.Time, th Thresholds, extraExclude []claudia.Provider) seatDecision {
	current := cli.PlanProvider(claudia.Provider(a.Provider))
	byProv := map[claudia.Provider]claudia.PlanUsage{}
	normalized := make([]claudia.PlanUsage, 0, len(usage))
	for _, u := range usage {
		u.Provider = cli.PlanProvider(u.Provider)
		if u.Provider == "" {
			continue
		}
		byProv[u.Provider] = u
		normalized = append(normalized, u)
	}
	cth := claudiaThresholdsPtr(th)
	reading, has := byProv[current]
	if !a.RecoverPlanPark && (!has || !claudia.ShouldVacate(reading, now, cth)) {
		return seatDecision{Action: SeatStay, From: current, Author: claudia.DecisionAuthor, Reason: "provider is not vacating"}
	}
	others := 0
	for p := range byProv {
		if p != current {
			others++
		}
	}
	if others == 0 && !a.RecoverPlanPark {
		return seatDecision{Action: SeatDefer, From: current, Author: claudia.DecisionAuthor, Reason: "destination feed is incomplete"}
	}
	if a.AllowNone || (a.AllowedProviders != nil && len(a.AllowedProviders) == 0) {
		return seatDecision{Action: SeatPark, From: current, Author: claudia.DecisionAuthor, Reason: "no allowed destination"}
	}
	excluded := map[claudia.Provider]bool{current: current != "" && !a.RecoverPlanPark}
	for _, p := range extraExclude {
		if id := cli.PlanProvider(p); id != "" {
			excluded[id] = true
		}
	}
	for _, p := range a.ExcludeProviders {
		if id := cli.PlanProvider(p); id != "" {
			excluded[id] = true
		}
	}
	if len(a.AllowedProviders) > 0 {
		allow := map[claudia.Provider]bool{}
		for _, p := range a.AllowedProviders {
			allow[cli.PlanProvider(p)] = true
		}
		for _, row := range claudia.ModelCatalog() {
			if !allow[row.Provider] {
				excluded[row.Provider] = true
			}
		}
	}
	var exclusions []claudia.Provider
	for _, row := range claudia.ModelCatalog() {
		if excluded[row.Provider] {
			exclusions = append(exclusions, row.Provider)
		}
	}
	prefer := cli.PlanProvider(a.PreferProvider)
	pick, err := claudia.Resolve(context.Background(), claudia.ModelPredicates{
		Mode: claudia.CapabilitySession, Quality: claudia.ModelQualityStandard,
		PreferPlan: true, RequireUsage: true, Background: true,
		PreferProvider: prefer, ExcludeProviders: exclusions,
		Usage: normalized, Now: now, Thresholds: cth,
	})
	if err != nil || pick.Provider == "" {
		reason := "no eligible destination"
		if err != nil {
			reason = err.Error()
		}
		return seatDecision{Action: SeatPark, From: current, Author: claudia.DecisionAuthor, Reason: reason}
	}
	reason := vacateReason(reading, now, cth)
	if a.RecoverPlanPark {
		reason = "plan-policy park reconsidered: " + pick.Reason
	}
	author := pick.Author
	if author == "" {
		author = claudia.DecisionAuthor
	}
	return seatDecision{Action: SeatMigrate, From: current, Pick: pick, Reason: reason, Author: author}
}

func vacateReason(u claudia.PlanUsage, now time.Time, th *claudia.PlanThresholds) string {
	v := claudia.ClassifyPlan(u, now, th)
	sess := v.Session == claudia.PlanSessionExhausted
	week := v.Weekly == claudia.PlanBandHot || v.Weekly == claudia.PlanBandExhausted
	switch {
	case sess && week:
		return "session or weekly exhausted"
	case sess:
		return "session exhausted"
	default:
		return "weekly hot or exhausted"
	}
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
		if decision.Action == SeatMigrate || decision.Action == SeatPark {
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
