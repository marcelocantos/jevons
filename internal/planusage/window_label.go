// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import "strings"

// DefaultMonthlyWindowSeconds is used when a monthly window publishes
// resets_at but no length (~30d billing cycle).
const DefaultMonthlyWindowSeconds int64 = 30 * 24 * 3600

const (
	weeklyDurationMinSeconds  int64 = 6 * 24 * 3600
	weeklyDurationMaxSeconds  int64 = 8 * 24 * 3600
	monthlyDurationMinSeconds int64 = 25 * 24 * 3600
	monthlyDurationMaxSeconds int64 = 35 * 24 * 3600
)

// normalizeWindowLabel renames claudia's generic "weekly" to "monthly" when
// the producer meant a billing-cycle window (🎯T550). Seven-day windows stay
// weekly; Cursor's included-usage cycle is monthly.
func normalizeWindowLabel(provider string, w Window) Window {
	name := strings.ToLower(strings.TrimSpace(w.Name))
	p := strings.ToLower(strings.TrimSpace(provider))
	if name != WindowWeekly {
		return w
	}
	if isWeeklyDuration(w.LimitWindowSeconds) {
		return w
	}
	if p == "cursor" || isMonthlyDuration(w.LimitWindowSeconds) {
		w.Name = WindowMonthly
	}
	return w
}

func isWeeklyDuration(sec *int64) bool {
	if sec == nil || *sec <= 0 {
		return false
	}
	return *sec >= weeklyDurationMinSeconds && *sec <= weeklyDurationMaxSeconds
}

func isMonthlyDuration(sec *int64) bool {
	if sec == nil || *sec <= 0 {
		return false
	}
	return *sec >= monthlyDurationMinSeconds && *sec <= monthlyDurationMaxSeconds
}

// PrimaryAllowanceWindow returns the longer plan allowance window — weekly
// for Claude/Codex/Grok, monthly for Cursor's billing cycle (🎯T550).
func (b Backend) PrimaryAllowanceWindow() (Window, bool) {
	if w, ok := b.Window(WindowWeekly); ok {
		return w, true
	}
	return b.Window(WindowMonthly)
}

// allowanceWindowLabel is the owner-facing word for band/tooltip copy.
func allowanceWindowLabel(w Window) string {
	if strings.EqualFold(w.Name, WindowMonthly) {
		return WindowMonthly
	}
	return WindowWeekly
}
