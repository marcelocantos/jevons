// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Plan-usage window paint (🎯T390.1, 🎯T610, 🎯T1072).
 *
 * This file classifies nothing. Every band is Claudia's Spend Pressure Map
 * verdict (🎯T641: pressure = ln(current/required) against its amber/red and
 * under/locked vertices), served per window by the daemon as `band`. The
 * cockpit maps that word to a CSS class and cockpit.css owns the colour.
 *
 * It used to carry a second model: a ratio classifier, fixed-15% leftover
 * arithmetic, an HSV colour ramp, and local copies of every vertex, kept
 * "for a payload with no band". Twice that copy drifted from the daemon's
 * (🎯T610: a red bar the daemon did not consider hot; 2026-09-24: a green
 * bar beside an amber graph). A window with no served band now paints no
 * pace colour at all, rather than one this file made up.
 */

import { usedPercentOf } from './tickerGroups';
import { remainingTimePercent, type PlanWindow as GeomWindow } from './windowGeom';

export const PACE_OK = 'ok';
export const PACE_AHEAD = 'ahead';
export const PACE_HOT = 'hot';
export const PACE_UNDER = 'under';
export const PACE_LOCKED = 'locked';

export const CLASS_STALE = 'plan-stale';
export const CLASS_AHEAD = 'plan-ahead';
export const CLASS_HOT = 'plan-hot';
export const CLASS_UNDER = 'plan-under';
export const CLASS_LOCKED = 'plan-locked';
export const CLASS_EXHAUSTED = 'plan-exhausted';
/** Nothing spent yet: the empty bar's border is the "you can use this" signal. */
export const CLASS_OPEN = 'plan-open';
/**
 * Consumed below this still counts as unused. At 1% the green border
 * drops and the bar goes back to its ordinary stroke.
 */
export const OPEN_USED_PERCENT = 1;

/** Hover name for a served leftover band, in the daemon's own words (bands.go). */
export function leftoverHoverName(pace: string): string {
  if (pace === PACE_UNDER) return 'under pace';
  if (pace === PACE_LOCKED) return 'surplus locked in';
  return '—';
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

/** True while consumption has not yet hit {@link OPEN_USED_PERCENT}. */
export function isUntouchedUsage(used: number | null | undefined): boolean {
  return typeof used === 'number' && Number.isFinite(used) && used < OPEN_USED_PERCENT;
}

export type FormattedWindow = {
  pace: string;
  paceClass: string;
  remainingPercent: number | null;
  usedPercent?: number | null;
};

export function windowClassName(w: FormattedWindow | null | undefined, stale?: boolean): string {
  const parts: string[] = [];
  const paceClass = w && w.pace ? w.paceClass : stale ? CLASS_STALE : '';
  if (paceClass) parts.push(paceClass);
  if (isRockBottomRemaining(w && w.remainingPercent)) {
    parts.push(CLASS_EXHAUSTED);
  } else if (
    isUntouchedUsage(w && w.usedPercent) &&
    paceClass !== CLASS_UNDER &&
    paceClass !== CLASS_LOCKED
  ) {
    // A waste border already says the empty bar is blue or purple. Green
    // is only for the stroke that would otherwise stay the quiet grey.
    parts.push(CLASS_OPEN);
  }
  return parts.join(' ');
}

export type PaceWindow = GeomWindow & {
  used_percent?: number | null;
  /** The daemon's verdict for this window (🎯T610). The only colour source. */
  band?: string | null;
  /** The 🎯T596 pressure the band was cut from; shown, never repainted. */
  pressure?: number | null;
};

/**
 * Bands the daemon can send, mapped to what this cockpit paints.
 * "exhausted" paints as hot; the spent-to-zero case additionally picks up
 * CLASS_EXHAUSTED from isRockBottomRemaining, so the two are not redundant.
 */
const SERVED_BAND: Record<string, string> = {
  hot: PACE_HOT,
  ahead: PACE_AHEAD,
  ok: PACE_OK,
  under: PACE_UNDER,
  locked: PACE_LOCKED,
  exhausted: PACE_HOT,
};

/**
 * 🎯T667: the pace class for a served band, through the same SERVED_BAND →
 * paceClassName chain the bar uses. null when the band is empty or unknown
 * (nothing to paint); '' for ok, whose colour is the element's default.
 */
export function paceClassForBand(band: string | null | undefined): string | null {
  const served = typeof band === 'string' ? band.trim() : '';
  if (!served) return null;
  const pace = SERVED_BAND[served];
  return pace === undefined ? null : paceClassName(pace);
}

/** Served bands this build did not recognise, for tests and the notice. */
export const unknownServedBands: string[] = [];

/**
 * The verdict for one window: what the daemon said, or '' when it said
 * nothing (an unavailable backend, or a window with no usable numbers).
 * '' paints no pace colour; it is not "ok".
 */
export function paceOfWindow(w: PaceWindow): string {
  const served = typeof w.band === 'string' ? w.band.trim() : '';
  if (!served) return '';
  const mapped = SERVED_BAND[served];
  if (mapped !== undefined) return mapped;
  // A band this build cannot paint is reported, never silently swallowed:
  // silence is exactly how the last drift hid for a day.
  if (!unknownServedBands.includes(served)) {
    unknownServedBands.push(served);
    if (typeof console !== 'undefined' && console.warn) {
      console.warn('plan usage: daemon sent band "' + served + '" this cockpit cannot paint (🎯T610)');
    }
  }
  return '';
}

export function formatWindow(w: PaceWindow, nowMs: number): FormattedWindow & {
  remainingTimePercent: number | null;
  className: string;
} {
  const remaining = typeof w.remaining_percent === 'number' ? w.remaining_percent : null;
  const pace = paceOfWindow(w);
  const paceClass = paceClassName(pace);
  const formatted = { pace, paceClass, remainingPercent: remaining, usedPercent: usedPercentOf(w) };
  return {
    ...formatted,
    remainingTimePercent: remainingTimePercent(w, nowMs),
    className: windowClassName(formatted),
  };
}
