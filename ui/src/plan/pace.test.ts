// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { afterEach, describe, expect, it } from 'vitest';
import {
  CLASS_EXHAUSTED,
  CLASS_HOT,
  PACE_AHEAD,
  PACE_HOT,
  PACE_LOCKED,
  PACE_OK,
  PACE_UNDER,
  PACE_UNDER_WASTE,
  PACE_LOCKED_WASTE,
  applyThresholds,
  classifyPace,
  formatWindow,
  resetThresholds,
  weeklyWaste,
} from './pace';

afterEach(() => {
  resetThresholds();
});

describe('classifyPace (🎯T390.1)', () => {
  it('is green / orange / red at the 1.0 and 1.5 damped burn ratios', () => {
    expect(classifyPace(50, 50, 50)).toBe(PACE_OK);
    expect(classifyPace(51, 49, 50)).toBe(PACE_AHEAD);
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
    expect(classifyPace(0, 100, 97, 'weekly')).toBe(PACE_UNDER);
    expect(classifyPace(0, 100, 50, 'session')).toBe(PACE_OK);
    expect(classifyPace(87, 13, 12, 'weekly')).toBe(PACE_OK);
    expect(classifyPace(80, 20, 60, 'weekly')).toBe(PACE_HOT);
    expect(classifyPace(43, 57, 50, 'weekly')).toBe(PACE_OK);
    expect(classifyPace(42, 58, 50, 'weekly')).toBe(PACE_UNDER);
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
