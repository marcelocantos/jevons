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
export const PACE_WARMUP_PERCENT = 5;
export const PACE_AHEAD_RATIO = 1.0;
export const PACE_HOT_RATIO = 1.5;
export const PACE_UNDER_WASTE = 15;
export const PACE_LOCKED_WASTE = 15;
export const PACE_DAMP_LAMBDA = 5;
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
};

let aheadRatio = PACE_AHEAD_RATIO;
let hotRatio = PACE_HOT_RATIO;
let underWaste = PACE_UNDER_WASTE;
let lockedWaste = PACE_LOCKED_WASTE;
let lowRemaining = LOW_PERCENT;
let criticalRemaining = CRITICAL_PERCENT;
let dampLambda = PACE_DAMP_LAMBDA;

export function applyThresholds(doc: ThresholdsDoc | null | undefined): void {
  if (!doc || typeof doc !== 'object') return;
  if (typeof doc.ahead_ratio === 'number') aheadRatio = doc.ahead_ratio;
  if (typeof doc.hot_ratio === 'number') hotRatio = doc.hot_ratio;
  if (typeof doc.under_waste_percent === 'number') underWaste = doc.under_waste_percent;
  if (typeof doc.locked_waste_percent === 'number') lockedWaste = doc.locked_waste_percent;
  if (typeof doc.low_remaining_percent === 'number') lowRemaining = doc.low_remaining_percent;
  if (typeof doc.critical_remaining_percent === 'number') criticalRemaining = doc.critical_remaining_percent;
  if (typeof doc.damp_lambda_percent === 'number') dampLambda = doc.damp_lambda_percent;
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
  const burn = (used + lambda) / (elapsed + lambda);
  if (burn > hotRatio) return PACE_HOT;
  if (burn > aheadRatio) return PACE_AHEAD;
  const weekly = String(windowName || '').toLowerCase() === 'weekly';
  if (weekly) {
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
