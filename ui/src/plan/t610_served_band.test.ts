// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it, beforeEach } from 'vitest';
import {
  formatWindow, paceOfWindow,
  PACE_AHEAD, PACE_HOT, PACE_LOCKED, PACE_OK, PACE_UNDER,
  unknownServedBands,
} from './pace';

const WEEK = 7 * 24 * 3600;
const NOW = Date.UTC(2026, 8, 1, 12, 0, 0);

/** A window with `frac` of its time left. */
function win(used: number, remaining: number, frac: number, band?: string) {
  return {
    name: 'weekly',
    used_percent: used,
    remaining_percent: remaining,
    resets_at: new Date(NOW + frac * WEEK * 1000).toISOString(),
    limit_window_seconds: WEEK,
    ...(band === undefined ? {} : { band }),
  };
}

beforeEach(() => {
  unknownServedBands.length = 0;
});

describe('🎯T610 / 🎯T1072 the cockpit paints the daemon verdict and nothing else', () => {
  // The live specimen. 39% used at 18.5% elapsed is ratio 2.11, which the
  // cockpit's old local classifier called hot; the daemon computes pressure
  // +0.63 and says ahead. The owner saw red on a window the daemon did not
  // consider hot, so migrate-off correctly never fired.
  it('paints ahead when the daemon says ahead', () => {
    expect(paceOfWindow(win(39, 61, 0.815, 'ahead'))).toBe(PACE_AHEAD);
  });

  it('carries the verdict through to the painted class', () => {
    const f = formatWindow(win(39, 61, 0.815, 'ahead'), NOW);
    expect(f.pace).toBe(PACE_AHEAD);
    expect(f.className).toContain('plan-ahead');
    expect(f.className).not.toContain('plan-hot');
  });

  it.each([
    ['hot', PACE_HOT],
    ['ahead', PACE_AHEAD],
    ['ok', PACE_OK],
    ['under', PACE_UNDER],
    ['locked', PACE_LOCKED],
    ['exhausted', PACE_HOT],
  ])('maps served %s', (served, want) => {
    expect(paceOfWindow(win(50, 50, 0.5, served))).toBe(want);
  });

  // 🎯T1072: there is no local model to fall back to. 80% used at half-time
  // is no colour at all until the daemon says what it is.
  it('paints no pace when the daemon sends no band', () => {
    expect(paceOfWindow(win(80, 20, 0.5))).toBe('');
    expect(formatWindow(win(80, 20, 0.5), NOW).className).not.toMatch(/plan-(hot|ahead|under|locked)/);
  });

  it('reports a band it cannot paint instead of swallowing it', () => {
    expect(paceOfWindow(win(50, 50, 0.5, 'incandescent'))).toBe('');
    expect(unknownServedBands).toContain('incandescent');
  });
});
