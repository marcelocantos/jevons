// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Duration marshals as a human-editable string ("6h", "5m") in
// budget.json rather than raw nanoseconds.
type Duration time.Duration

// MarshalJSON emits the duration in time.Duration string form.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON accepts either a duration string ("6h") or a number of
// nanoseconds.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		dur, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("bad duration %q: %w", s, err)
		}
		*d = Duration(dur)
		return nil
	}
	var ns int64
	if err := json.Unmarshal(data, &ns); err != nil {
		return fmt.Errorf("bad duration %s", data)
	}
	*d = Duration(ns)
	return nil
}

// Std returns the standard-library form.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Limits is one escalation ladder in USD/hour. Zero disables a rung.
type Limits struct {
	WarnUSDPerHour     float64 `json:"warn_usd_per_hour"`
	ThrottleUSDPerHour float64 `json:"throttle_usd_per_hour"`
	PauseUSDPerHour    float64 `json:"pause_usd_per_hour"`
	KillUSDPerHour     float64 `json:"kill_usd_per_hour"`
}

// LevelFor returns the highest escalation level the rate crosses.
func (l Limits) LevelFor(usdPerHour float64) Level {
	switch {
	case l.KillUSDPerHour > 0 && usdPerHour >= l.KillUSDPerHour:
		return LevelKill
	case l.PauseUSDPerHour > 0 && usdPerHour >= l.PauseUSDPerHour:
		return LevelPause
	case l.ThrottleUSDPerHour > 0 && usdPerHour >= l.ThrottleUSDPerHour:
		return LevelThrottle
	case l.WarnUSDPerHour > 0 && usdPerHour >= l.WarnUSDPerHour:
		return LevelWarn
	default:
		return LevelNone
	}
}

// Accounting modes for clamp honesty (🎯T137). list_price treats
// provider costUSD / table estimates as billable dollars for enforcement
// and UI. subscription is SuperGrok / flat-plan: those figures are
// API-equivalent estimates only — never pause/kill/spawn-halt on them.
const (
	AccountingListPrice    = "list_price"
	AccountingSubscription = "subscription"
	AccountingDisabled     = "disabled" // derived when Disabled=true; not stored
)

// BudgetConfig is the clamp-down policy, persisted at
// ~/.jevons/budget.json. The zero value is unusable; start from
// DefaultBudgetConfig and override.
type BudgetConfig struct {
	// Disabled turns off the entire cost subsystem for this daemon:
	// no collector, no enforcer (pause/kill/spawn-halt). GET /api/cost
	// still reports {"disabled":true}. Owner opt-out without code edits.
	Disabled bool `json:"disabled"`

	// Accounting selects how USD figures drive enforcement and UI
	// (🎯T137). Empty defaults to list_price. Use "subscription" for
	// SuperGrok / no-marginal-$ plans so API-list-price token burn is
	// never treated as real dollars for pause/kill. Ignored when
	// Disabled is true.
	Accounting string `json:"accounting,omitempty"`

	// Global covers ALL spend visible in the store, attributed or not —
	// the incident's invisible fleet lands here.
	Global Limits `json:"global"`
	// Fleet covers the sum of jevons-attributed workers.
	Fleet Limits `json:"fleet"`
	// Worker is the default per-worker ladder; Workers overrides by id.
	Worker  Limits            `json:"worker"`
	Workers map[string]Limits `json:"workers,omitempty"`

	// MaxSessions bounds distinct billable sessions per window before
	// the fleet-size signal trips.
	MaxSessions int `json:"max_sessions"`
	// ProviderSoftCaps soft-limits concurrent registered agents per LLM
	// harness provider (grok/claude/codex) for 🎯T325.2 portfolio spread.
	// Session-count only — never derived from API-equivalent USD (T137).
	// Empty = use compiled DefaultPortfolio soft caps. 0 removes a cap.
	ProviderSoftCaps map[string]int `json:"provider_soft_caps,omitempty"`
	// DailyBudgetUSD is the projection target: projected end-of-day
	// spend above it trips the projected-overspend signal.
	DailyBudgetUSD float64 `json:"daily_budget_usd"`
	// HardCeilingUSDPerDay hard-stops spawning once today's spend
	// crosses it. This is the "no matter what" line.
	HardCeilingUSDPerDay float64 `json:"hard_ceiling_usd_per_day"`

	// ProtectedWorkers are never killed or deregistered by the enforcer
	// — kill downgrades to pause. The overseer lives here by default:
	// deregistering the butler's own brain would brick the cockpit, and
	// the idle-context re-cache tax makes the overseer the most likely
	// worker to trip its own ladder. (The global kill-switch may still
	// kill a protected worker's PROCESS — that loses nothing durable;
	// it rehydrates on next owner contact.)
	ProtectedWorkers []string `json:"protected_workers"`

	// DeadManIdle: if no owner heartbeat for this long while the fleet
	// is burning, the fleet is stopped resumably.
	DeadManIdle Duration `json:"dead_man_idle"`
	// ResumeMaxAttempts caps automatic relaunches per worker between
	// owner contacts (the auto-resume amplifier guard).
	ResumeMaxAttempts int `json:"resume_max_attempts"`
	// Window is the rolling window burn-rates are computed over.
	Window Duration `json:"window"`
	// MinEventsForKill: a kill-level RATE must be backed by at least this
	// many billable events in the window to escalate to an irreversible
	// action; below it the rate is "thin" (a single expensive message —
	// e.g. the ~$11 idle-context re-cache tax — that reads as a huge
	// extrapolated rate) and caps at pause. The 2026-07-06 runaway was
	// hundreds of events across 47 sessions; a spike is a handful. 0
	// disables the guard.
	MinEventsForKill int `json:"min_events_for_kill"`
	// KillConfirmTicks requires a kill-level rate breach to persist for
	// this many consecutive enforcement ticks before the irreversible
	// action (kill worker / fire kill-switch) fires. A burn RATE from a
	// single window can be a one-off spike — the 2026-07-06 incident was
	// distinguished by PERSISTENCE (hours at ~$227/hr), not any single
	// message (the ~$11 idle-context re-cache tax alone reads as
	// ~$132/hr for one window). Warn/throttle/pause act immediately;
	// only the nuke waits for confirmation. 0 = act immediately.
	KillConfirmTicks int `json:"kill_confirm_ticks"`
}

// EffectiveAccounting returns the accounting mode that governs this
// config: disabled, subscription, or list_price.
func (c *BudgetConfig) EffectiveAccounting() string {
	if c == nil || c.Disabled {
		return AccountingDisabled
	}
	if c.Accounting == AccountingSubscription {
		return AccountingSubscription
	}
	return AccountingListPrice
}

// IsSubscription reports whether USD figures are non-bill estimates
// (SuperGrok / flat subscription) and must not drive pause/kill.
func (c *BudgetConfig) IsSubscription() bool {
	return c != nil && c.EffectiveAccounting() == AccountingSubscription
}

// CurrencyNote is the honest label for USD fields under this config.
func (c *BudgetConfig) CurrencyNote() string {
	switch c.EffectiveAccounting() {
	case AccountingSubscription:
		return "API-equivalent USD estimate — not billed under SuperGrok / flat subscription"
	case AccountingDisabled:
		return "cost monitoring disabled"
	default:
		return "USD at API list-price / provider costUSD (billable estimate)"
	}
}

// DefaultBudgetConfig returns the shipped defaults. They are set from
// the 2026-07-06 incident's shape (~$227/hr sustained, 47 sessions):
// generous enough not to clamp normal interactive work, tight enough
// that the incident would have been killed inside its first hour.
// Accounting defaults to list_price (paid API). SuperGrok owners set
// accounting=subscription in budget.json (🎯T137).
func DefaultBudgetConfig() *BudgetConfig {
	return &BudgetConfig{
		Accounting:           AccountingListPrice,
		Global:               Limits{WarnUSDPerHour: 10, ThrottleUSDPerHour: 20, PauseUSDPerHour: 40, KillUSDPerHour: 60},
		Fleet:                Limits{WarnUSDPerHour: 5, ThrottleUSDPerHour: 10, PauseUSDPerHour: 20, KillUSDPerHour: 40},
		Worker:               Limits{WarnUSDPerHour: 2, ThrottleUSDPerHour: 5, PauseUSDPerHour: 10, KillUSDPerHour: 20},
		MaxSessions: 20,
		// Daily figures track GLOBAL machine spend (incl. the owner's own
		// sessions), so the defaults sit well above a heavy owner's
		// baseline (~$300/day observed) yet far below the 2026-07-06
		// incident (~$5.4k/day). Tune in budget.json to your reality.
		DailyBudgetUSD:       500,
		HardCeilingUSDPerDay: 1500,
		ProtectedWorkers:     []string{"jevons"},
		DeadManIdle:          Duration(6 * time.Hour),
		ResumeMaxAttempts:    3,
		Window:               Duration(5 * time.Minute),
		MinEventsForKill:     30,
		KillConfirmTicks:     2,
	}
}

// LoadBudgetConfig reads path, layering the file's fields over the
// defaults. A missing file yields the defaults (and is not an error);
// a malformed file is an error — silently falling back to defaults
// would mask a typo'd policy.
func LoadBudgetConfig(path string) (*BudgetConfig, error) {
	cfg := DefaultBudgetConfig()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config to path (pretty-printed, atomic enough for a
// single-writer daemon).
func (c *BudgetConfig) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
