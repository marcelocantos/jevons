// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { afterEach, describe, expect, it } from 'vitest';
import {
  CLASS_EXHAUSTED,
  CLASS_HOT,
  PACE_AHEAD,
  PACE_AHEAD_RATIO,
  PACE_COLOR_AHEAD,
  PACE_COLOR_HOT,
  PACE_COLOR_LOCKED,
  PACE_COLOR_OK,
  PACE_COLOR_UNDER,
  PACE_HOT,
  PACE_HOT_RATIO,
  PACE_LOCKED,
  PACE_LOCKED_WASTE,
  PACE_OK,
  PACE_UNDER,
  PACE_UNDER_WASTE,
  applyThresholds,
  classifyPace,
  fillColorForWindow,
  formatWindow,
  leftoverHoverName,
  overspendStops,
  paceColor,
  resetThresholds,
  weeklyWaste,
} from './pace';
import { hsvLerpRgb, parseCssColor, rgbToCss } from './hsv';

afterEach(() => {
  resetThresholds();
});

describe('classifyPace (🎯T390.1)', () => {
  it('is green / orange / red at the 1.0 and 1.5 damped burn ratios', () => {
    expect(classifyPace(50, 50, 50)).toBe(PACE_OK);
    // 🎯T591 moved this vertex on purpose. 51% used at 50% elapsed is a
    // 1pp overspend — a damped burn of 1.018, i.e. microscopically past
    // parity — and painting that amber made the ticker twitch on noise.
    // Amber now needs overspend past the 2pp margin as well as the ratio.
    expect(classifyPace(51, 49, 50)).toBe(PACE_OK);
    expect(classifyPace(53, 47, 50)).toBe(PACE_AHEAD);
    expect(classifyPace(77.5, 22.5, 50)).toBe(PACE_AHEAD);
    expect(classifyPace(78, 22, 50)).toBe(PACE_HOT);
    expect(classifyPace(100, 0, 40)).toBe(PACE_HOT);
    expect(classifyPace(80, 20, 97)).toBe(PACE_HOT);
    expect(classifyPace(80, 20, null)).toBe('');
    expect(classifyPace(24, 76, 50)).toBe(PACE_OK);
  });

  it('weekly continuation is blue, locked is purple, session is exempt', () => {
    expect(classifyPace(0, 100, 81, 'weekly')).toBe(PACE_UNDER);
    const early = weeklyWaste(0, 100, 81);
    expect(early.continuation).toBeGreaterThanOrEqual(PACE_UNDER_WASTE);
    expect(early.locked ?? 0).toBeLessThan(PACE_LOCKED_WASTE);

    expect(classifyPace(0, 100, 50, 'weekly')).toBe(PACE_LOCKED);
    expect(classifyPace(0, 100, 97, 'weekly')).toBe(PACE_OK);
    expect(classifyPace(0, 100, 50, 'session')).toBe(PACE_OK);
    expect(classifyPace(87, 13, 12, 'weekly')).toBe(PACE_OK);
    expect(classifyPace(80, 20, 60, 'weekly')).toBe(PACE_HOT);
    expect(classifyPace(43, 57, 50, 'weekly')).toBe(PACE_OK);
    expect(classifyPace(42, 58, 50, 'weekly')).toBe(PACE_UNDER);
    // Owner 2026-09-12: late-window leftover is locked surplus, not continuation-blue.
    expect(classifyPace(64, 36, 9.5, 'monthly')).toBe(PACE_LOCKED);
    expect(classifyPace(64, 36, 9.5, 'weekly')).toBe(PACE_LOCKED);
    expect(leftoverHoverName(PACE_UNDER)).toBe('continuation leftover');
    expect(leftoverHoverName(PACE_LOCKED)).toBe('already-unrecoverable at 1.5×');
    expect(leftoverHoverName(PACE_OK)).toBe('—');
    expect(leftoverHoverName(PACE_HOT)).toBe('—');
  });

  it('applyThresholds moves the hot vertex', () => {
    expect(classifyPace(65, 35, 50)).toBe(PACE_AHEAD);
    applyThresholds({ hot_ratio: 1.2 });
    expect(classifyPace(65, 35, 50)).toBe(PACE_HOT);
  });

  it('early-window burn is damped', () => {
    const weekStart = classifyPace(9, 91, 94.4, 'weekly');
    expect(weekStart === PACE_OK || weekStart === PACE_AHEAD).toBe(true);
    applyThresholds({ damp_lambda_percent: 0 });
    expect(classifyPace(9, 91, 94.4, 'weekly')).toBe(PACE_HOT);
    expect(classifyPace(80, 20, 50, 'weekly')).toBe(PACE_HOT);
  });

  it('has no elapsed cutoff: Codex spent-early week is hot', () => {
    expect(classifyPace(26, 74, 95.1, 'weekly')).toBe(PACE_HOT);
  });
});

describe('formatWindow paint class', () => {
  it('paints Codex 75% used with most of the week left as hot, not green', () => {
    const now = Date.parse('2026-08-23T10:10:00Z');
    const painted = formatWindow(
      {
        name: 'weekly',
        remaining_percent: 25,
        used_percent: 75,
        resets_at: '2026-08-28T22:21:27Z',
        limit_window_seconds: 604800,
      },
      now,
    );
    expect(painted.pace).toBe(PACE_HOT);
    expect(painted.className.split(' ')).toContain(CLASS_HOT);
  });

  it('adds plan-exhausted when remaining is 0', () => {
    const painted = formatWindow(
      { name: 'weekly', remaining_percent: 0, used_percent: 100 },
      Date.now(),
    );
    expect(painted.className.split(' ')).toContain(CLASS_EXHAUSTED);
    expect(painted.className.split(' ')).toContain(CLASS_HOT);
  });
});

function rgbOf(css: string): { r: number; g: number; b: number } | null {
  return parseCssColor(css);
}

function far(
  a: { r: number; g: number; b: number } | null,
  b: { r: number; g: number; b: number } | null,
  n = 30,
): boolean {
  if (!a || !b) return true;
  return Math.abs(a.r - b.r) + Math.abs(a.g - b.g) + Math.abs(a.b - b.b) > n;
}

/** Wiring referent: same HSV helper paceColor uses, plus far() for snap-mutants. */
function refLerp(hexA: string, hexB: string, t: number): string {
  const a = parseCssColor(hexA);
  const b = parseCssColor(hexB);
  if (!a || !b) throw new Error('bad hex');
  return rgbToCss(hsvLerpRgb(a, b, t));
}

describe('paceColor HSV lerp (🎯T390.1.2)', () => {
  it('samples A, mid(A,B), B, mid(B,C), C on the overspend axis', () => {
    const { a, b, c } = overspendStops();
    expect(a).toBe(PACE_AHEAD_RATIO);
    expect(c).toBe(PACE_HOT_RATIO);
    expect(b).toBeCloseTo((a + c) / 2, 10);

    const colA = paceColor(a);
    const colMidAB = paceColor((a + b) / 2);
    const colB = paceColor(b);
    const colMidBC = paceColor((b + c) / 2);
    const colC = paceColor(c);

    const ok = rgbOf(refLerp(PACE_COLOR_OK, PACE_COLOR_OK, 0));
    const amber = rgbOf(refLerp(PACE_COLOR_AHEAD, PACE_COLOR_AHEAD, 0));
    const red = rgbOf(refLerp(PACE_COLOR_HOT, PACE_COLOR_HOT, 0));
    expect(rgbOf(colA)).toEqual(ok);
    expect(rgbOf(colB)).toEqual(amber);
    expect(rgbOf(colC)).toEqual(red);
    expect(colMidAB).toBe(refLerp(PACE_COLOR_OK, PACE_COLOR_AHEAD, 0.5));
    expect(colMidBC).toBe(refLerp(PACE_COLOR_AHEAD, PACE_COLOR_HOT, 0.5));

    // Snap-to-named-class mutant: midpoints equal a stop colour.
    expect(far(rgbOf(colMidAB), ok)).toBe(true);
    expect(far(rgbOf(colMidAB), amber)).toBe(true);
    expect(far(rgbOf(colMidBC), amber)).toBe(true);
    expect(far(rgbOf(colMidBC), red)).toBe(true);
  });

  it('samples waste counterparts A′, mid, B′, mid locked, C′', () => {
    const green = paceColor(1, { continuation: 0, locked: 0 });
    const midUnder = paceColor(0.5, { continuation: PACE_UNDER_WASTE / 2, locked: 0 });
    const blue = paceColor(0.5, { continuation: PACE_UNDER_WASTE, locked: 0 });
    const midLocked = paceColor(0.2, { continuation: PACE_UNDER_WASTE, locked: PACE_LOCKED_WASTE / 2 });
    const purple = paceColor(0.2, { continuation: PACE_UNDER_WASTE, locked: PACE_LOCKED_WASTE });

    expect(green).toBe(refLerp(PACE_COLOR_OK, PACE_COLOR_OK, 0));
    expect(midUnder).toBe(refLerp(PACE_COLOR_OK, PACE_COLOR_UNDER, 0.5));
    expect(blue).toBe(refLerp(PACE_COLOR_UNDER, PACE_COLOR_UNDER, 0));
    expect(midLocked).toBe(refLerp(PACE_COLOR_UNDER, PACE_COLOR_LOCKED, 0.5));
    expect(purple).toBe(refLerp(PACE_COLOR_LOCKED, PACE_COLOR_LOCKED, 0));

    expect(far(rgbOf(midUnder), rgbOf(green))).toBe(true);
    expect(far(rgbOf(midUnder), rgbOf(blue))).toBe(true);
    expect(far(rgbOf(midLocked), rgbOf(blue))).toBe(true);
    expect(far(rgbOf(midLocked), rgbOf(purple))).toBe(true);
  });

  it('session waste is ignored: only the burn axis paints', () => {
    const sessionMid = paceColor((PACE_AHEAD_RATIO + PACE_HOT_RATIO) / 2);
    expect(sessionMid).toBe(refLerp(PACE_COLOR_AHEAD, PACE_COLOR_AHEAD, 0));
    const withWaste = paceColor((PACE_AHEAD_RATIO + PACE_HOT_RATIO) / 2, {
      continuation: PACE_UNDER_WASTE,
      locked: 0,
    });
    // burn is at B (orange); continuation does not override overspend.
    expect(withWaste).toBe(sessionMid);
  });

  it('applyThresholds moves the lerp vertices', () => {
    const before = paceColor(1.2);
    applyThresholds({ hot_ratio: 1.2 });
    const after = paceColor(1.2);
    expect(after).toBe(refLerp(PACE_COLOR_HOT, PACE_COLOR_HOT, 0));
    expect(before).not.toBe(after);
  });

  it('exhausted remaining paints stop C (red)', () => {
    const painted = formatWindow(
      { name: 'weekly', remaining_percent: 0, used_percent: 100 },
      Date.now(),
    );
    expect(painted.fillColor).toBe(refLerp(PACE_COLOR_HOT, PACE_COLOR_HOT, 0));
    expect(fillColorForWindow({ name: 'session', remaining_percent: 0, used_percent: 100 }, Date.now()))
      .toBe(painted.fillColor);
  });
});
