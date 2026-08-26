// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Same window lengths plan_usage.js uses when the API omits limit_window_seconds. */
export const SESSION_LIMIT_SECONDS = 5 * 60 * 60;
export const WEEKLY_LIMIT_SECONDS = 7 * 24 * 60 * 60;
export const MONTHLY_LIMIT_SECONDS = 30 * 24 * 60 * 60;

export type PlanWindow = {
  name?: string;
  remaining_percent?: number | null;
  resets_at?: string | null;
  limit_window_seconds?: number | null;
};

export function clampPercent(v: number): number | null {
  if (typeof v !== 'number' || !Number.isFinite(v)) return null;
  if (v < 0) return 0;
  if (v > 100) return 100;
  return v;
}

export function limitSecondsFor(w: PlanWindow): number | null {
  const published = w.limit_window_seconds;
  if (typeof published === 'number' && Number.isFinite(published) && published > 0) {
    return published;
  }
  const n = String(w.name || '').toLowerCase();
  if (n === 'session') return SESSION_LIMIT_SECONDS;
  if (n === 'weekly') return WEEKLY_LIMIT_SECONDS;
  if (n === 'monthly') return MONTHLY_LIMIT_SECONDS;
  return null;
}

/**
 * Triangle left % — same formula as web/scripts/plan_usage.js formatWindow.
 * No resets_at or window length → no invented position (no triangle).
 */
export function remainingTimePercent(w: PlanWindow, nowMs: number): number | null {
  const resets = w.resets_at;
  if (!resets) return null;
  const at = Date.parse(resets);
  if (Number.isNaN(at)) return null;
  const limitSec = limitSecondsFor(w);
  if (limitSec == null) return null;
  return clampPercent((100 * ((at - nowMs) / 1000)) / limitSec);
}
