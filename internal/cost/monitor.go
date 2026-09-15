// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Level is an escalation severity, ordered: each level implies every
// action below it.
type Level int

const (
	LevelNone Level = iota
	LevelWarn
	LevelThrottle
	LevelPause
	LevelKill
)

// String renders the level for logs and the API.
func (l Level) String() string {
	switch l {
	case LevelWarn:
		return "warn"
	case LevelThrottle:
		return "throttle"
	case LevelPause:
		return "pause"
	case LevelKill:
		return "kill"
	default:
		return "none"
	}
}

// MarshalJSON emits the level as its string form.
func (l Level) MarshalJSON() ([]byte, error) { return []byte(`"` + l.String() + `"`), nil }

// Alert kinds — the runaway signals from the post-mortem, each decidable.
const (
	AlertGlobalRate     = "global-rate"
	AlertFleetRate      = "fleet-rate"
	AlertWorkerRate     = "worker-rate"
	AlertSessionCount   = "session-count"
	AlertOrphanSessions = "orphan-sessions"
	AlertProjection     = "projected-overspend"
	AlertHardCeiling    = "hard-ceiling"
	AlertCollectorStale = "collector-stale"
	// AlertSpawnStorm is defined in auditor.go (🎯T334 standing cost-safety).
)

// collectorStaleAfter: a collector that hasn't completed a poll pass in
// this long means the monitor may be blind, which must itself be an
// alarm — the incident's core failure was silent invisibility.
//
// One stale observation is not yet an alarm (🎯T654). On 2026-09-15
// the whole daemon was starved for two minutes under a load average of
// 32 — every ticker in the process slipped, not just the collector's —
// and the first monitor pass after the stall found a poll two minutes
// old that the collector completed seconds later. A monitor that has
// itself just woken cannot tell a wedged collector from a stalled
// host, so it notes the staleness and lets the NEXT pass decide: a
// collector still stale when the monitor is demonstrably running is
// blind and alarms. collectorDeadAfter is the bound past which the
// alarm fires regardless — a monitor that is only consulted rarely must
// still not sit on a collector that has been silent this long.
const (
	collectorStaleAfter = 2 * time.Minute
	collectorDeadAfter  = 5 * collectorStaleAfter
)

// Alert is one tripped runaway signal.
type Alert struct {
	Kind     string   `json:"kind"`
	Level    Level    `json:"level"`
	Worker   string   `json:"worker,omitempty"`
	Sessions []string `json:"sessions,omitempty"`
	Detail   string   `json:"detail"`
}

// Snapshot is the live answer to "what is burning right now": rates,
// per-session rows, and every tripped signal, computed in one call.
type Snapshot struct {
	At     time.Time     `json:"at"`
	Window time.Duration `json:"window_ns"`

	// Accounting / Billable / CurrencyNote describe how USD fields must
	// be read (🎯T137). subscription → not real SuperGrok dollars.
	Accounting   string `json:"accounting"`
	Billable     bool   `json:"billable"`
	CurrencyNote string `json:"currency_note,omitempty"`

	GlobalUSDPerHour float64            `json:"global_usd_per_hour"`
	FleetUSDPerHour  float64            `json:"fleet_usd_per_hour"`
	WorkerUSDPerHour map[string]float64 `json:"worker_usd_per_hour,omitempty"`

	SpentTodayUSD     float64 `json:"spent_today_usd"`
	ProjectedTodayUSD float64 `json:"projected_today_usd"`

	Sessions []BurnRow `json:"sessions"`
	Alerts   []Alert   `json:"alerts,omitempty"`
}

// MonitorArgs parameterises NewMonitor. Store and Config are required.
// IsOrphan classifies a burning session as ownerless (cockpit gone) —
// the production classifier lives in the wiring layer where tmux/ps
// access belongs; nil means no orphan detection. Now is injected for
// deterministic tests.
type MonitorArgs struct {
	Store    *Store
	Config   func() *BudgetConfig
	IsOrphan func(BurnRow) bool
	// CollectorLastPoll reports the collector's last completed poll, so
	// a broken collector becomes an alarm rather than silent blindness.
	// nil disables the staleness check (pure-store tests).
	CollectorLastPoll func() time.Time
	// CollectorHealth, when set, supersedes CollectorLastPoll and lets the
	// staleness alarm say what the collector is doing (🎯T654).
	CollectorHealth func() CollectorHealth
	Now             func() time.Time
}

// Monitor computes burn-rates and runaway signals from the store.
type Monitor struct {
	store             *Store
	config            func() *BudgetConfig
	isOrphan          func(BurnRow) bool
	collectorLastPoll func() time.Time
	collectorHealth   func() CollectorHealth
	now               func() time.Time

	// staleMu guards staleSeen: whether the previous Snapshot found the
	// collector stale (the confirm-on-second-observation rule above).
	staleMu   sync.Mutex
	staleSeen bool
}

// NewMonitor constructs a Monitor.
func NewMonitor(args *MonitorArgs) *Monitor {
	m := &Monitor{store: args.Store, config: args.Config, isOrphan: args.IsOrphan,
		collectorLastPoll: args.CollectorLastPoll, collectorHealth: args.CollectorHealth, now: args.Now}
	if m.collectorHealth != nil {
		m.collectorLastPoll = func() time.Time { return m.collectorHealth().LastPoll }
	}
	if m.isOrphan == nil {
		m.isOrphan = func(BurnRow) bool { return false }
	}
	if m.now == nil {
		m.now = time.Now
	}
	return m
}

// Snapshot computes the current burn picture and tripped signals.
func (m *Monitor) Snapshot() (*Snapshot, error) {
	cfg := m.config()
	now := m.now()
	from := now.Add(-cfg.Window.Std())
	hours := cfg.Window.Std().Hours()

	rows, err := m.store.Burning(from, now)
	if err != nil {
		return nil, err
	}

	snap := &Snapshot{
		At:           now,
		Window:       cfg.Window.Std(),
		Sessions:     rows,
		Accounting:   cfg.EffectiveAccounting(),
		Billable:     cfg.EffectiveAccounting() == AccountingListPrice,
		CurrencyNote: cfg.CurrencyNote(),
	}

	var globalCost, fleetCost float64
	var globalEvents, fleetEvents int
	workerCost := map[string]float64{}
	workerEvents := map[string]int{}
	var orphans []string
	for _, r := range rows {
		globalCost += r.CostUSD
		globalEvents += r.Events
		orphan := m.isOrphan(r)
		// The "fleet" clamp scope is what jevons OWNS: attributed workers
		// plus orphans (lost workers still inside the fleet). Foreign
		// spend (the owner's own sessions) lands in global only, which is
		// informational — jevons can neither kill nor should nuke its
		// fleet over it.
		if r.Worker != "" || orphan {
			fleetCost += r.CostUSD
			fleetEvents += r.Events
		}
		if r.Worker != "" {
			workerCost[r.Worker] += r.CostUSD
			workerEvents[r.Worker] += r.Events
		}
		if orphan {
			orphans = append(orphans, r.SessionID)
		}
	}
	snap.GlobalUSDPerHour = globalCost / hours
	snap.FleetUSDPerHour = fleetCost / hours
	if len(workerCost) > 0 {
		snap.WorkerUSDPerHour = map[string]float64{}
		for w, c := range workerCost {
			snap.WorkerUSDPerHour[w] = c / hours
		}
	}

	// Today's spend and end-of-day projection, in local time — this is a
	// single-host system and the owner's day is the billing rhythm that
	// matters here.
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	snap.SpentTodayUSD, err = m.store.SpentUSD(midnight, now)
	if err != nil {
		return nil, err
	}
	remaining := midnight.Add(24 * time.Hour).Sub(now).Hours()
	snap.ProjectedTodayUSD = snap.SpentTodayUSD + snap.GlobalUSDPerHour*remaining

	snap.Alerts = m.evaluate(cfg, snap, orphans, globalEvents, fleetEvents, workerEvents)

	// The watchdog's own watchdog: a stale collector means everything
	// above may be an undercount, and that must be loud.
	if alert, ok := m.collectorStale(now); ok {
		snap.Alerts = append(snap.Alerts, alert)
	}
	return snap, nil
}

// collectorStale decides the collector-stale alarm (🎯T654): stale on two
// consecutive passes, or stale past collectorDeadAfter, alarms; a first
// stale observation is noted and logged, because the monitor cannot yet
// tell a blind collector from a host stall it was itself part of.
func (m *Monitor) collectorStale(now time.Time) (Alert, bool) {
	if m.collectorLastPoll == nil {
		return Alert{}, false
	}
	lp := m.collectorLastPoll()
	stale := lp.IsZero() || now.Sub(lp) > collectorStaleAfter
	m.staleMu.Lock()
	confirmed := m.staleSeen
	m.staleSeen = stale
	m.staleMu.Unlock()
	if !stale {
		return Alert{}, false
	}
	since := "never"
	if !lp.IsZero() {
		since = lp.Format(time.RFC3339)
	}
	detail := fmt.Sprintf("cost collector has not polled since %s", since)
	if m.collectorHealth != nil {
		detail += " (" + m.collectorHealth().Describe(now) + ")"
	}
	dead := !lp.IsZero() && now.Sub(lp) > collectorDeadAfter
	if !confirmed && !dead {
		slog.Info("cost monitor: collector poll overdue — confirming on the next pass before alarming (🎯T654)", "detail", detail)
		return Alert{}, false
	}
	return Alert{Kind: AlertCollectorStale, Level: LevelWarn,
		Detail: detail + " — burn figures may be blind"}, true
}

// evaluate applies the budget policy to a computed snapshot. Split out
// so tests can exercise the policy against synthetic snapshots too. The
// event counts back the thin-rate guard: a kill-level rate from too few
// events is a spike, not a runaway, and is capped at pause.
//
// Under accounting=subscription (🎯T137), USD ladder / hard-ceiling
// signals are capped at LevelWarn and labeled as non-bill estimates so
// the enforcer never pause/kills on fake SuperGrok dollars. Session-
// count and orphan signals stay available (resource hygiene, not $).
func (m *Monitor) evaluate(cfg *BudgetConfig, snap *Snapshot, orphans []string, globalEvents, fleetEvents int, workerEvents map[string]int) []Alert {
	var alerts []Alert
	sub := cfg.IsSubscription()

	// capThin downgrades a kill-level rate to pause when the window holds
	// fewer than MinEventsForKill events in that scope.
	capThin := func(lvl Level, events int) Level {
		if lvl == LevelKill && cfg.MinEventsForKill > 0 && events < cfg.MinEventsForKill {
			return LevelPause
		}
		return lvl
	}
	// capSub: under subscription accounting, USD rates never escalate
	// past warn — estimates are informational only.
	capSub := func(lvl Level) Level {
		if sub && lvl > LevelWarn {
			return LevelWarn
		}
		return lvl
	}
	usdLabel := "USD"
	if sub {
		usdLabel = "API-eq est USD (not billed)"
	}

	if lvl := capSub(capThin(cfg.Global.LevelFor(snap.GlobalUSDPerHour), globalEvents)); lvl != LevelNone {
		alerts = append(alerts, Alert{Kind: AlertGlobalRate, Level: lvl,
			Detail: fmt.Sprintf("global burn %.2f %s/hr (%s ≥ %.2f); %d events", snap.GlobalUSDPerHour, usdLabel, lvl, cfg.Global.thresholdFor(lvl), globalEvents)})
	}
	if lvl := capSub(capThin(cfg.Fleet.LevelFor(snap.FleetUSDPerHour), fleetEvents)); lvl != LevelNone {
		alerts = append(alerts, Alert{Kind: AlertFleetRate, Level: lvl,
			Detail: fmt.Sprintf("fleet burn %.2f %s/hr (%s ≥ %.2f); %d events", snap.FleetUSDPerHour, usdLabel, lvl, cfg.Fleet.thresholdFor(lvl), fleetEvents)})
	}
	for w, rate := range snap.WorkerUSDPerHour {
		limits := cfg.Worker
		if o, ok := cfg.Workers[w]; ok {
			limits = o
		}
		if lvl := capSub(capThin(limits.LevelFor(rate), workerEvents[w])); lvl != LevelNone {
			alerts = append(alerts, Alert{Kind: AlertWorkerRate, Level: lvl, Worker: w,
				Detail: fmt.Sprintf("worker %s burn %.2f %s/hr (%s ≥ %.2f)", w, rate, usdLabel, lvl, limits.thresholdFor(lvl))})
		}
	}
	if cfg.MaxSessions > 0 && len(snap.Sessions) > cfg.MaxSessions {
		alerts = append(alerts, Alert{Kind: AlertSessionCount, Level: LevelWarn,
			Detail: fmt.Sprintf("%d billable sessions in window (bound %d)", len(snap.Sessions), cfg.MaxSessions)})
		// Spawn storm (🎯T334): postmortem signature was dozens of concurrent
		// sessions. At ≥ SpawnStormFactor × MaxSessions escalate beyond warn
		// so the standing auditor + enforcer can throttle/pause, not only note.
		stormBound := cfg.MaxSessions * SpawnStormFactor
		if stormBound < cfg.MaxSessions+1 {
			stormBound = cfg.MaxSessions + 1
		}
		if len(snap.Sessions) >= stormBound {
			alerts = append(alerts, Alert{Kind: AlertSpawnStorm, Level: LevelThrottle,
				Detail: fmt.Sprintf("spawn storm: %d billable sessions (storm bound %d = %dx max_sessions)",
					len(snap.Sessions), stormBound, SpawnStormFactor)})
		}
	}
	if len(orphans) > 0 {
		alerts = append(alerts, Alert{Kind: AlertOrphanSessions, Level: LevelWarn, Sessions: orphans,
			Detail: fmt.Sprintf("%d burning session(s) with no owner attached", len(orphans))})
	}
	if cfg.DailyBudgetUSD > 0 && snap.ProjectedTodayUSD > cfg.DailyBudgetUSD {
		detail := fmt.Sprintf("projected %.2f %s today (budget %.2f)", snap.ProjectedTodayUSD, usdLabel, cfg.DailyBudgetUSD)
		alerts = append(alerts, Alert{Kind: AlertProjection, Level: LevelWarn, Detail: detail})
	}
	if cfg.HardCeilingUSDPerDay > 0 && snap.SpentTodayUSD >= cfg.HardCeilingUSDPerDay {
		// list_price: kill-level hard ceiling halts spawning.
		// subscription: warn only — estimate is not real SuperGrok $.
		if sub {
			alerts = append(alerts, Alert{Kind: AlertHardCeiling, Level: LevelWarn,
				Detail: fmt.Sprintf("spent %.2f %s today ≥ hard ceiling %.2f — informational only under subscription accounting", snap.SpentTodayUSD, usdLabel, cfg.HardCeilingUSDPerDay)})
		} else {
			alerts = append(alerts, Alert{Kind: AlertHardCeiling, Level: LevelKill,
				Detail: fmt.Sprintf("spent %.2f USD today ≥ hard ceiling %.2f — spawning halted", snap.SpentTodayUSD, cfg.HardCeilingUSDPerDay)})
		}
	}
	return alerts
}

// thresholdFor returns the dollar threshold of a given level, for alert
// messages.
func (l Limits) thresholdFor(lvl Level) float64 {
	switch lvl {
	case LevelKill:
		return l.KillUSDPerHour
	case LevelPause:
		return l.PauseUSDPerHour
	case LevelThrottle:
		return l.ThrottleUSDPerHour
	case LevelWarn:
		return l.WarnUSDPerHour
	default:
		return 0
	}
}
