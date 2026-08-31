// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Same spend-vs-time colours as web/scripts/plan_usage.js (🎯T390.1). */

import { remainingTimePercent, type PlanWindow as GeomWindow } from './windowGeom';

export const PACE_OK = 'ok';
export const PACE_AHEAD = 'ahead';
export const PACE_HOT = 'hot';
export const PACE_UNDER = 'under';
export const PACE_LOCKED = 'locked';

export const CLASS_CRITICAL = 'plan-crit';
export const CLASS_LOW = 'plan-low';
export const CLASS_STALE = 'plan-stale';
export const CLASS_AHEAD = 'plan-ahead';
export const CLASS_HOT = 'plan-hot';
export const CLASS_UNDER = 'plan-under';
export const CLASS_LOCKED = 'plan-locked';
export const CLASS_EXHAUSTED = 'plan-exhausted';

/** Served document only; classifyPace does not short-circuit on it (🎯T390.1.6.2). */
/**
 * How much of a window must have passed before a burning-fast verdict is
 * believable (🎯T595). Inert from 🎯T390.1.6.2 until 🎯T595: as elapsed
 * approaches 0 the damped burn collapses to 1 + used/λ, which contains no
 * rate at all — with λ=5, "hot" just means used ≥ 2.5pp. Claude's week was
 * painted red at 94% remaining, 2.13% in.
 */
export const PACE_WARMUP_PERCENT = 5;
/**
 * Absolute spend that earns a verdict before warmup, so the gate above
 * cannot mute a real emergency.
 */
export const PACE_EARLY_ALARM_USED = 25;
export const PACE_AHEAD_RATIO = 1.0;
export const PACE_HOT_RATIO = 1.5;
export const PACE_UNDER_WASTE = 15;
export const PACE_LOCKED_WASTE = 15;
export const PACE_DAMP_LAMBDA = 5;
/**
 * Percentage-point margin a window must overspend by before any
 * burning-fast verdict is reachable (🎯T591). Mirrors
 * planusage.Thresholds.AheadMarginPercent — the daemon owns the number,
 * this is the paint side of the same document.
 *
 * Damping cannot serve this purpose: (used+λ)/(elapsed+λ) approaches 1
 * from above and never crosses it, so at ahead_ratio 1.0 any overspend
 * at all — rounding included — is amber, for every λ.
 */
export const PACE_AHEAD_MARGIN = 2;
export const LOW_PERCENT = 15;
export const CRITICAL_PERCENT = 5;

export type ThresholdsDoc = {
  ahead_ratio?: number;
  hot_ratio?: number;
  under_waste_percent?: number;
  locked_waste_percent?: number;
  warmup_elapsed_percent?: number;
  low_remaining_percent?: number;
  critical_remaining_percent?: number;
  damp_lambda_percent?: number;
  ahead_margin_percent?: number;
  early_alarm_used_percent?: number;
};

let aheadRatio = PACE_AHEAD_RATIO;
let hotRatio = PACE_HOT_RATIO;
let underWaste = PACE_UNDER_WASTE;
let lockedWaste = PACE_LOCKED_WASTE;
let lowRemaining = LOW_PERCENT;
let criticalRemaining = CRITICAL_PERCENT;
let dampLambda = PACE_DAMP_LAMBDA;
let aheadMargin = PACE_AHEAD_MARGIN;
let warmupElapsed = PACE_WARMUP_PERCENT;
let earlyAlarmUsed = PACE_EARLY_ALARM_USED;

export function applyThresholds(doc: ThresholdsDoc | null | undefined): void {
  if (!doc || typeof doc !== 'object') return;
  if (typeof doc.ahead_ratio === 'number') aheadRatio = doc.ahead_ratio;
  if (typeof doc.hot_ratio === 'number') hotRatio = doc.hot_ratio;
  if (typeof doc.under_waste_percent === 'number') underWaste = doc.under_waste_percent;
  if (typeof doc.locked_waste_percent === 'number') lockedWaste = doc.locked_waste_percent;
  if (typeof doc.low_remaining_percent === 'number') lowRemaining = doc.low_remaining_percent;
  if (typeof doc.critical_remaining_percent === 'number') criticalRemaining = doc.critical_remaining_percent;
  if (typeof doc.damp_lambda_percent === 'number') dampLambda = doc.damp_lambda_percent;
  if (typeof doc.ahead_margin_percent === 'number') aheadMargin = doc.ahead_margin_percent;
  if (typeof doc.warmup_elapsed_percent === 'number') warmupElapsed = doc.warmup_elapsed_percent;
  if (typeof doc.early_alarm_used_percent === 'number') earlyAlarmUsed = doc.early_alarm_used_percent;
}

export function resetThresholds(): void {
  applyThresholds({
    ahead_ratio: PACE_AHEAD_RATIO,
    hot_ratio: PACE_HOT_RATIO,
    under_waste_percent: PACE_UNDER_WASTE,
    locked_waste_percent: PACE_LOCKED_WASTE,
    low_remaining_percent: LOW_PERCENT,
    critical_remaining_percent: CRITICAL_PERCENT,
    damp_lambda_percent: PACE_DAMP_LAMBDA,
    ahead_margin_percent: PACE_AHEAD_MARGIN,
    warmup_elapsed_percent: PACE_WARMUP_PERCENT,
    early_alarm_used_percent: PACE_EARLY_ALARM_USED,
  });
}

export function weeklyWaste(
  usedPercent: number | null | undefined,
  remainingPercent: number | null | undefined,
  remainingTime: number | null | undefined,
): { continuation: number | null; locked: number | null } {
  if (typeof remainingTime !== 'number' || !Number.isFinite(remainingTime)) {
    return { continuation: null, locked: null };
  }
  const rem =
    typeof remainingPercent === 'number' && Number.isFinite(remainingPercent)
      ? remainingPercent
      : null;
  const used =
    typeof usedPercent === 'number' && Number.isFinite(usedPercent)
      ? usedPercent
      : rem !== null
        ? 100 - rem
        : null;
  const elapsed = 100 - remainingTime;
  let continuation: number | null = null;
  if (used !== null && elapsed > 0) {
    continuation = Math.max(0, 100 - (used / elapsed) * 100);
  }
  const locked = rem === null ? null : Math.max(0, rem - hotRatio * remainingTime);
  return { continuation, locked };
}

/**
 * Two independent ways to be over-confident about a window, so two guards:
 * the overspend must be real rather than a rounding step (🎯T591), and
 * enough of the window must have passed for a rate to mean anything
 * (🎯T595) — unless the absolute spend is already alarming by itself.
 */
function burningFastReachable(used: number, elapsed: number): boolean {
  if (used - elapsed <= aheadMargin) return false;
  return elapsed >= warmupElapsed || (earlyAlarmUsed > 0 && used >= earlyAlarmUsed);
}

export function classifyPace(
  usedPercent: number | null | undefined,
  remainingPercent: number | null | undefined,
  remainingTime: number | null | undefined,
  windowName?: string,
): string {
  if (typeof remainingPercent === 'number' && remainingPercent <= 0) return PACE_HOT;
  if (typeof remainingTime !== 'number' || !Number.isFinite(remainingTime)) return '';
  const used =
    typeof usedPercent === 'number' && Number.isFinite(usedPercent)
      ? usedPercent
      : typeof remainingPercent === 'number'
        ? 100 - remainingPercent
        : null;
  if (used === null) return '';
  const elapsed = 100 - remainingTime;
  // No elapsed cutoff (🎯T390.1.6.2) — λ eases early-window extremes.
  const lambda = dampLambda < 0 ? 0 : dampLambda;
  // A burning-fast verdict needs real overspend, not a rounding step
  // (🎯T591): providers publish used as whole percentage points, so
  // early in a window the numerator's quantum can exceed elapsed itself.
  if (burningFastReachable(used, elapsed)) {
    const burn = (used + lambda) / (elapsed + lambda);
    if (burn > hotRatio) return PACE_HOT;
    if (burn > aheadRatio) return PACE_AHEAD;
  }
  const weekly = String(windowName || '').toLowerCase() === 'weekly';
  const monthly = String(windowName || '').toLowerCase() === 'monthly';
  if (weekly || monthly) {
    const w = weeklyWaste(used, remainingPercent, remainingTime);
    if (w.locked !== null && w.locked >= lockedWaste) return PACE_LOCKED;
    if (w.continuation !== null && w.continuation >= underWaste) return PACE_UNDER;
  }
  return PACE_OK;
}

export function paceClassName(pace: string): string {
  if (pace === PACE_HOT) return CLASS_HOT;
  if (pace === PACE_AHEAD) return CLASS_AHEAD;
  if (pace === PACE_LOCKED) return CLASS_LOCKED;
  if (pace === PACE_UNDER) return CLASS_UNDER;
  return '';
}

export function isRockBottomRemaining(remaining: number | null | undefined): boolean {
  return typeof remaining === 'number' && Number.isFinite(remaining) && remaining <= 0;
}

export function chipClassForRemaining(remaining: number | null | undefined, stale?: boolean): string {
  if (typeof remaining === 'number' && remaining <= criticalRemaining) return CLASS_CRITICAL;
  if (typeof remaining === 'number' && remaining <= lowRemaining) return CLASS_LOW;
  if (stale) return CLASS_STALE;
  return '';
}

export type FormattedWindow = {
  pace: string;
  paceClass: string;
  remainingPercent: number | null;
};

export function windowClassName(w: FormattedWindow | null | undefined, stale?: boolean): string {
  const parts: string[] = [];
  const paceOrRem = w && w.pace ? w.paceClass : chipClassForRemaining(w && w.remainingPercent, stale);
  if (paceOrRem) parts.push(paceOrRem);
  if (isRockBottomRemaining(w && w.remainingPercent)) parts.push(CLASS_EXHAUSTED);
  return parts.join(' ');
}

export type PaceWindow = GeomWindow & { used_percent?: number | null };

export function formatWindow(w: PaceWindow, nowMs: number): FormattedWindow & {
  remainingTimePercent: number | null;
  className: string;
} {
  const remaining = typeof w.remaining_percent === 'number' ? w.remaining_percent : null;
  const used = typeof w.used_percent === 'number' ? w.used_percent : null;
  const remainingTime = remainingTimePercent(w, nowMs);
  const pace = classifyPace(used, remaining, remainingTime, w.name);
  const paceClass = paceClassName(pace);
  const formatted = { pace, paceClass, remainingPercent: remaining };
  return {
    ...formatted,
    remainingTimePercent: remainingTime,
    className: windowClassName(formatted),
  };
}
