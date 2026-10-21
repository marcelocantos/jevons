// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  CLASS_EXHAUSTED,
  CLASS_HOT,
  CLASS_LOCKED,
  CLASS_OPEN,
  CLASS_STALE,
  PACE_HOT,
  formatWindow,
  windowClassName,
} from './pace';

// 🎯T1072: pace.ts classifies nothing; the served band is the verdict. These
// cover the classes formatWindow layers on top of it.
describe('formatWindow paint class', () => {
  it('paints a served hot window hot', () => {
    const now = Date.parse('2026-08-23T10:10:00Z');
    const painted = formatWindow(
      {
        name: 'weekly',
        remaining_percent: 25,
        used_percent: 75,
        resets_at: '2026-08-28T22:21:27Z',
        limit_window_seconds: 604800,
        band: 'hot',
      },
      now,
    );
    expect(painted.pace).toBe(PACE_HOT);
    expect(painted.className.split(' ')).toContain(CLASS_HOT);
  });

  it('adds plan-exhausted when remaining is 0', () => {
    const painted = formatWindow(
      { name: 'weekly', remaining_percent: 0, used_percent: 100, band: 'exhausted' },
      Date.now(),
    );
    expect(painted.className.split(' ')).toContain(CLASS_EXHAUSTED);
    expect(painted.className.split(' ')).toContain(CLASS_HOT);
    expect(painted.className.split(' ')).not.toContain(CLASS_OPEN);
  });

  it('paints plan-open while usage is under 1%', () => {
    const zero = formatWindow({ name: 'session', remaining_percent: 100, used_percent: 0, band: 'ok' }, Date.now());
    expect(zero.className.split(' ')).toContain(CLASS_OPEN);
    const dust = formatWindow({ name: 'session', remaining_percent: 99.2, used_percent: 0.8, band: 'ok' }, Date.now());
    expect(dust.className.split(' ')).toContain(CLASS_OPEN);
    const started = formatWindow({ name: 'session', remaining_percent: 99, used_percent: 1, band: 'ok' }, Date.now());
    expect(started.className.split(' ')).not.toContain(CLASS_OPEN);
    const fromRemaining = formatWindow({ name: 'session', remaining_percent: 100 }, Date.now());
    expect(fromRemaining.className.split(' ')).toContain(CLASS_OPEN);
  });

  it('leaves a served locked empty week purple, without the open border', () => {
    const locked = formatWindow(
      { name: 'weekly', remaining_percent: 100, used_percent: 0, band: 'locked' },
      Date.now(),
    );
    expect(locked.className.split(' ')).toContain(CLASS_LOCKED);
    expect(locked.className.split(' ')).not.toContain(CLASS_OPEN);
  });

  it('paints a served-ok untouched week-start open', () => {
    const painted = formatWindow(
      { name: 'weekly', remaining_percent: 100, used_percent: 0, band: 'ok' },
      Date.now(),
    );
    expect(painted.className.split(' ')).toContain(CLASS_OPEN);
    expect(painted.className).not.toMatch(/plan-under|plan-locked/);
  });

  it('marks a band-less window stale only when asked, never low or critical', () => {
    const w = { pace: '', paceClass: '', remainingPercent: 3, usedPercent: 97 };
    expect(windowClassName(w, true).split(' ')).toContain(CLASS_STALE);
    expect(windowClassName(w)).toBe('');
  });
});
