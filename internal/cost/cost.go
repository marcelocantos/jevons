// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package cost is jevons's token-spend accounting and clamp-down system
// (🎯T36), forced by the 2026-07-06 token-runaway incident: a detached
// worker fleet burned ~$10.9k invisibly because nothing measured spend in
// real time and nothing could stop it automatically.
//
// Layer 1 (this file + store/tail/collector): every active billable
// transcript under the configured sessions root is tailed in
// near-real-time — not just jevons-registered workers, because the
// incident fleet was exactly the kind of thing registration misses.
// Grok Build: updates.jsonl turn_completed (costUsdTicks preferred).
// Claude Code: <session>.jsonl assistant lines (costUSD preferred).
// Pricing-table fallback only when the provider omits a cost figure.
//
// Layer 2 (monitor): rolling burn-rates per worker/fleet/global, a
// one-query "what is burning right now" view, and runaway signals
// (including spawn-storm for concurrent session storms).
//
// Layer 3 (budget/enforcer/killswitch): budgets with warn → throttle →
// pause → kill escalation, a hard ceiling that stops spawning, a global
// kill-switch that reaches launchd-detached tmux fleets, a dead-man's
// switch, and an auto-resume guard.
//
// Standing cost-safety auditor (🎯T334): trip-class policy (auditor.go)
// names sensor trips, resolves overseer protection (never budget-killed),
// dual-writes clamp decisions to eventlog (component=cost_clamp), and
// surfaces overseer-visible alerts on the same T36 spine — not a second
// shadow ledger. Optional LLM auditor is residual; harness sensors+clamp
// are the product path.
//
// Accounting honesty (🎯T137): budget.json accounting=list_price (default)
// treats USD as billable for enforcement. accounting=subscription
// (SuperGrok / no marginal $) reports API-equivalent estimates labeled
// non-bill and never pause/kills on them. disabled=true turns the
// collector and enforcer off entirely; /api/cost reports disabled.
package cost

import "time"

// Usage is the token counts from one billable API response.
type Usage struct {
	Input       int64 `json:"input_tokens"`
	Output      int64 `json:"output_tokens"`
	CacheCreate int64 `json:"cache_creation_input_tokens"`
	CacheRead   int64 `json:"cache_read_input_tokens"`
}

// Total is the total token count across all classes.
func (u Usage) Total() int64 {
	return u.Input + u.Output + u.CacheCreate + u.CacheRead
}

// IsZero reports whether the usage carries no tokens at all.
func (u Usage) IsZero() bool { return u.Total() == 0 }

// Event is one billable API response extracted from a session JSONL.
type Event struct {
	Timestamp time.Time
	SessionID string
	// Worker is the jevons-level attribution (thread/agent id), or ""
	// when the session is not one jevons knows about. Unattributed spend
	// is a first-class signal, not an error — the incident fleet would
	// have shown up here.
	Worker    string
	Model     string
	Usage     Usage
	CostUSD   float64
	RequestID string // dedup key: requestId, else message.id; "" = none
}
