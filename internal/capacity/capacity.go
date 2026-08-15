// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package capacity is the pure admission-control policy for background fleet
// work (🎯T359).
//
// The fleet grew standing background cycles — ambient research (🎯T356),
// standing audits (🎯T357), the RSI coach (🎯T243), sentinel/staff ops, the
// frontier-consume drip — each with its own soft cap and none of them aware of
// the others. Per-agent caps in isolation cannot answer "is there room for
// this tick at all?", so the fleet ran everything until the 🎯T36 cost clamp
// fired. This package answers that question once, holistically, from a budget
// snapshot plus concurrent load, and ranks work so owner-facing turns and open
// Build missions always outrank ambient background.
//
// Composition, not replacement:
//
//   - 🎯T36 cost enforcer still owns the clamp ladder and spawn-halt; this
//     package consumes its snapshot (SpawnHalted, HighestAlert) as input.
//   - 🎯T137 subscription honesty: when Billable is false, USD figures are
//     API-equivalent estimates and never drive a deny on their own. Token and
//     load headroom stay binding — tokens are counted, not priced.
//   - 🎯T325.2 portfolio soft caps supply the per-provider load headroom;
//     routing (which provider) remains the portfolio's job.
//
// Everything here is pure: a Snapshot and a Policy in, Decisions out. No
// clocks, no I/O, no fleet mutation. The daemon wiring (Governor + adapters)
// turns those decisions into skipped ticks and owner notices.
package capacity

import (
	"sort"
	"strings"
)

// Class is a kind of work, ranked by priority. Owner-facing turns and open
// Build missions are foreground: they are never deferred to make room for
// ambient work — only the reverse.
type Class string

const (
	// ClassOwnerTurn is an owner-facing turn (chat, direct, cockpit action).
	// Never gated by this package.
	ClassOwnerTurn Class = "owner_turn"
	// ClassBuildMission is open Build work: implementers on a target, the
	// frontier-consume drip, PO spawn passes.
	ClassBuildMission Class = "build_mission"
	// ClassControlRepair is sentinel / cockpit converge repair — background,
	// but load-bearing: it is what unsticks the fleet, so it degrades last.
	ClassControlRepair Class = "control_repair"
	// ClassStaffOps is the staff-ops observe/classify cycle.
	ClassStaffOps Class = "staff_ops"
	// ClassAudit is a standing audit pass (🎯T357).
	ClassAudit Class = "audit"
	// ClassCoach is the ambient RSI coach (🎯T243).
	ClassCoach Class = "rsi_coach"
	// ClassResearch is the ambient research cycle (🎯T356).
	ClassResearch Class = "research"
	// ClassExperimental is opt-in residual ambience (RSI mint, spikes).
	// Unknown class names land here so novelty never outranks known work.
	ClassExperimental Class = "experimental"
)

// ranked lists every class from highest to lowest priority.
var ranked = []Class{
	ClassOwnerTurn,
	ClassBuildMission,
	ClassControlRepair,
	ClassStaffOps,
	ClassAudit,
	ClassCoach,
	ClassResearch,
	ClassExperimental,
}

// Classes returns every known class in priority order (highest first).
func Classes() []Class { return append([]Class(nil), ranked...) }

// Rank is the priority of c: lower wins. An unrecognised class ranks last.
func Rank(c Class) int {
	for i, known := range ranked {
		if known == c {
			return i
		}
	}
	return len(ranked)
}

// Foreground reports whether c is owner-facing or open Build work — the two
// classes background admission must never starve.
func (c Class) Foreground() bool {
	return c == ClassOwnerTurn || c == ClassBuildMission
}

// Background is the complement of Foreground.
func (c Class) Background() bool { return !c.Foreground() }

// NormalizeClass maps free-form loop names onto a class. Unknown names become
// ClassExperimental (lowest priority) rather than an error: a new ambient loop
// that forgot to declare itself must not silently outrank research.
func NormalizeClass(raw string) Class {
	switch s := Class(strings.ToLower(strings.TrimSpace(string(raw)))); s {
	case ClassOwnerTurn, "owner", "chat", "direct":
		return ClassOwnerTurn
	case ClassBuildMission, "build", "mission", "frontier_consume", "worker":
		return ClassBuildMission
	case ClassControlRepair, "sentinel", "converge", "cockpit", "repair":
		return ClassControlRepair
	case ClassStaffOps, "staffops", "staff":
		return ClassStaffOps
	case ClassAudit, "auditor", "security_audit":
		return ClassAudit
	case ClassCoach, "coach", "rsi", "rsi_coach_retro":
		return ClassCoach
	case ClassResearch, "research_feed":
		return ClassResearch
	default:
		return ClassExperimental
	}
}

// Pressure is how tight capacity is right now.
type Pressure int

const (
	// PressureNormal: everything runs.
	PressureNormal Pressure = iota
	// PressureElevated: ambient work runs at a reduced tier.
	PressureElevated
	// PressureTight: only load-bearing background runs; the rest defers.
	PressureTight
	// PressureCritical: owner and Build work only.
	PressureCritical
)

// String renders the pressure for logs, the API, and oracles.
func (p Pressure) String() string {
	switch p {
	case PressureElevated:
		return "elevated"
	case PressureTight:
		return "tight"
	case PressureCritical:
		return "critical"
	default:
		return "normal"
	}
}

// MarshalJSON emits the pressure as its string form.
func (p Pressure) MarshalJSON() ([]byte, error) { return []byte(`"` + p.String() + `"`), nil }

// Snapshot is the capacity picture one admission pass reasons over: what is
// left of the budget, and what is already running. Every field is optional;
// an unknown quantity (zero budget, empty load map) contributes no pressure
// rather than a fabricated one.
type Snapshot struct {
	// Billable reports whether the USD fields are real money (🎯T137
	// list_price accounting). Under a flat subscription they are
	// API-equivalent estimates: informative, never grounds to deny on their
	// own.
	Billable bool `json:"billable"`
	// Accounting is the cost package's effective mode, carried for honest
	// reasons strings ("list_price", "subscription", "disabled").
	Accounting string `json:"accounting,omitempty"`

	SpentTodayUSD        float64 `json:"spent_today_usd,omitempty"`
	ProjectedTodayUSD    float64 `json:"projected_today_usd,omitempty"`
	DailyBudgetUSD       float64 `json:"daily_budget_usd,omitempty"`
	HardCeilingUSDPerDay float64 `json:"hard_ceiling_usd_per_day,omitempty"`

	// TokensUsed / TokensBudget are the honest lever under any accounting
	// mode: tokens are counted, not priced. 0 budget = unbounded/unknown.
	TokensUsed   int64 `json:"tokens_used,omitempty"`
	TokensBudget int64 `json:"tokens_budget,omitempty"`

	// ActiveSessions is billable sessions in the cost window; MaxSessions is
	// the configured bound (0 = unbounded).
	ActiveSessions int `json:"active_sessions,omitempty"`
	MaxSessions    int `json:"max_sessions,omitempty"`
	// ProviderLoad / ProviderSoftCaps are the 🎯T325.2 portfolio spread.
	ProviderLoad     map[string]int `json:"provider_load,omitempty"`
	ProviderSoftCaps map[string]int `json:"provider_soft_caps,omitempty"`

	// RunningBackground counts background cycles already in flight by class.
	RunningBackground map[Class]int `json:"running_background,omitempty"`
	// OwnerActive is true while an owner-facing turn is in flight (🎯T291:
	// owner chat is not starved by fleet churn).
	OwnerActive bool `json:"owner_active,omitempty"`
	// OpenBuildMissions is the count of engaged Build implementers.
	OpenBuildMissions int `json:"open_build_missions,omitempty"`

	// HostLoad1 / HostCores are the host's own saturation: the 1-minute
	// run-queue length and the core count it must be judged against (🎯T463).
	// Zero means unread — the fleet melted a 16-core host at load 247 while
	// every dimension this snapshot did carry said normal, so the host is a
	// first-class dimension here rather than something inferred from agent
	// counts. Cores is read at runtime by the producer, never assumed.
	HostLoad1 float64 `json:"host_load1,omitempty"`
	HostCores int     `json:"host_cores,omitempty"`
	// HostSwapUsedBytes / HostSwapTotalBytes are swap occupancy. A zero total
	// is unknown (or a host with no swap), not a full one.
	HostSwapUsedBytes  int64 `json:"host_swap_used_bytes,omitempty"`
	HostSwapTotalBytes int64 `json:"host_swap_total_bytes,omitempty"`
	// HostSource names how the reading was obtained ("darwin sysctl"), so a
	// surprising number can be traced to its origin.
	HostSource string `json:"host_source,omitempty"`

	// SpawnHalted is the 🎯T36 clamp: the budget has already stopped launches.
	SpawnHalted bool `json:"spawn_halted,omitempty"`
	// HighestAlert is the top cost alert level in the snapshot
	// ("none"/"warn"/"throttle"/"pause"/"kill"). The cost monitor already
	// caps these at warn under subscription accounting, so this composes
	// with 🎯T137 without a second honesty rule here.
	HighestAlert string `json:"highest_alert,omitempty"`
}

// alertPressure maps a cost alert level onto the pressure it implies on its
// own. Unknown/absent levels imply nothing.
func alertPressure(level string) Pressure {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "kill", "pause":
		return PressureCritical
	case "throttle":
		return PressureTight
	case "warn":
		return PressureElevated
	default:
		return PressureNormal
	}
}

// Request is one unit of work asking for capacity.
type Request struct {
	// Class ranks the request. Empty normalizes to ClassExperimental.
	Class Class `json:"class"`
	// Name identifies the caller for logs and the status API
	// ("research.schedule", "audit.standing").
	Name string `json:"name,omitempty"`
	// EstTokens is an optional cost estimate for this tick. When both this
	// and Snapshot.TokensBudget are non-zero, Plan spends against the
	// remaining background allowance rather than only counting slots.
	EstTokens int64 `json:"est_tokens,omitempty"`
	// Degradable declares that the caller can run a cheaper pass (shorter
	// lookback, smaller model tier). Non-degradable work defers instead.
	Degradable bool `json:"degradable,omitempty"`
}

// Running is one background cycle already in flight, for preemption.
type Running struct {
	Class Class  `json:"class"`
	Name  string `json:"name,omitempty"`
	// Preemptible is false for work that must finish once started (a pass
	// mid-write). Non-preemptible work is reported but never cancelled.
	Preemptible bool `json:"preemptible,omitempty"`
}

// Verdict is the outcome of an admission decision.
type Verdict string

const (
	// VerdictAdmit: run this tick in full.
	VerdictAdmit Verdict = "admit"
	// VerdictDegrade: run, but at the reduced tier.
	VerdictDegrade Verdict = "degrade"
	// VerdictDefer: skip this tick, retry on the next one.
	VerdictDefer Verdict = "defer"
	// VerdictCancel: stop work already in flight to make room.
	VerdictCancel Verdict = "cancel"
)

// Tier labels how much work an admitted request may do.
const (
	TierFull    = "full"
	TierReduced = "reduced"
)

// Reason codes are stable machine tokens (oracles assert on these; Detail is
// for humans).
const (
	ReasonOwnerPriority     = "owner_priority"
	ReasonHeadroomOK        = "headroom_ok"
	ReasonElevatedDegrade   = "elevated_degrade"
	ReasonOwnerTurnActive   = "owner_turn_active"
	ReasonTightLoadBearing  = "tight_load_bearing_only"
	ReasonCriticalOwnerOnly = "critical_owner_only"
	ReasonSpawnHalted       = "spawn_halted"
	ReasonBackgroundSlots   = "background_saturated"
	ReasonClassSlots        = "class_saturated"
	ReasonTokenReserve      = "token_reserve"
	ReasonHostSaturated     = "host_saturated"
	ReasonPreempted         = "preempted_for_capacity"
)

// Decision is one admission outcome.
type Decision struct {
	Class   Class   `json:"class"`
	Name    string  `json:"name,omitempty"`
	Verdict Verdict `json:"verdict"`
	// Tier is TierFull or TierReduced on admitted work; empty otherwise.
	Tier string `json:"tier,omitempty"`
	// Reason is a stable machine token; Detail is the human sentence.
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
	// Pressure is the pressure this decision was taken under.
	Pressure Pressure `json:"pressure"`
}

// Admitted reports whether the decision lets the work run at all.
func (d Decision) Admitted() bool {
	return d.Verdict == VerdictAdmit || d.Verdict == VerdictDegrade
}

// sortByRank orders decisions by class priority, stable within a class.
func sortByRank[T any](items []T, class func(T) Class) {
	sort.SliceStable(items, func(i, j int) bool {
		return Rank(class(items[i])) < Rank(class(items[j]))
	})
}
