// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it, beforeEach } from 'vitest';
import {
  formatWindow, paceOfWindow, resetThresholds,
  PACE_AHEAD, PACE_HOT, PACE_OK, PACE_UNDER,
  unknownServedBands, unknownThresholdKeys, applyThresholds,
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
  resetThresholds();
  unknownServedBands.length = 0;
});

describe('🎯T610 the cockpit paints the daemon verdict', () => {
  // The live specimen. 39% used at 18.5% elapsed is ratio 2.11, which this
  // file's own classifier calls hot; the daemon computes pressure +0.63 and
  // says ahead. The owner saw red on a window the daemon did not consider
  // hot, so migrate-off correctly never fired.
  it('paints ahead when the daemon says ahead, even though the local model says hot', () => {
    const w = win(39, 61, 0.815, 'ahead');
    expect(paceOfWindow(w, NOW)).toBe(PACE_AHEAD);
    // The control: without the served band this build really does say hot,
    // so the case keeps exercising the disagreement it exists for.
    expect(paceOfWindow(win(39, 61, 0.815), NOW)).toBe(PACE_HOT);
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
    ['exhausted', PACE_HOT],
  ])('maps served %s', (served, want) => {
    expect(paceOfWindow(win(50, 50, 0.5, served), NOW)).toBe(want);
  });

  it('falls back to the local model when the daemon sends no band', () => {
    // An older daemon, or a window with no usable numbers. The fallback is
    // for absence, not a second opinion.
    expect(paceOfWindow(win(80, 20, 0.5), NOW)).toBe(PACE_HOT);
  });

  it('reports a band it cannot paint instead of swallowing it', () => {
    paceOfWindow(win(50, 50, 0.5, 'incandescent'), NOW);
    expect(unknownServedBands).toContain('incandescent');
  });

  it('reports an unrecognised threshold key', () => {
    applyThresholds({ some_future_vertex: 1 } as never);
    expect(unknownThresholdKeys).toContain('some_future_vertex');
  });
});
