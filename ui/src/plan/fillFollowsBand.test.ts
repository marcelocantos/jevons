// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { afterEach, describe, expect, it } from 'vitest';
import {
  PACE_COLOR_AHEAD,
  PACE_COLOR_HOT,
  PACE_COLOR_OK,
  PACE_OK,
  PACE_PANIC_AMBER_LN,
  PACE_PANIC_RED_LN,
  applyThresholds,
  dampedBurn,
  fillColorForWindow,
  formatWindow,
  resetThresholds,
  type PaceWindow,
} from './pace';
import { parseCssColor, rgbToCss } from './hsv';

afterEach(() => {
  resetThresholds();
});

const WEEK_SECONDS = 604800;
const NOW = Date.parse('2026-09-21T07:17:00Z');

function css(hex: string): string {
  return rgbToCss(parseCssColor(hex)!);
}

/** A weekly window `elapsedPercent` of the way through, as of NOW. */
function weekly(used: number, elapsedPercent: number, extra: Partial<PaceWindow>): PaceWindow {
  const leftMs = (1 - elapsedPercent / 100) * WEEK_SECONDS * 1000;
  return {
    name: 'weekly',
    used_percent: used,
    remaining_percent: 100 - used,
    resets_at: new Date(NOW + leftMs).toISOString(),
    limit_window_seconds: WEEK_SECONDS,
    ...extra,
  };
}

describe('the fill never contradicts the served band', () => {
  // The owner's screen on 2026-09-21: claude weekly, 8% used, 3.1% elapsed.
  // Band "ok", bar red.
  it('paints a served ok green even when the ratio model calls it hot', () => {
    const w = weekly(8, 3.1, { band: 'ok', pressure: 0.1 });
    // The control: without it this case would pass on the old code too.
    expect(dampedBurn(8, 3.1)!).toBeGreaterThan(1.5);

    expect(fillColorForWindow(w, NOW)).toBe(css(PACE_COLOR_OK));
    const painted = formatWindow(w, NOW);
    expect(painted.pace).toBe(PACE_OK);
    expect(painted.fillColor).toBe(css(PACE_COLOR_OK));
  });

  it('keeps the ratio ramp only for a payload with no band', () => {
    expect(fillColorForWindow(weekly(8, 3.1, {}), NOW)).toBe(css(PACE_COLOR_HOT));
  });

  it('ramps a served ahead on served pressure, green → amber → red', () => {
    const mid = (PACE_PANIC_AMBER_LN + PACE_PANIC_RED_LN) / 2;
    const at = (pressure: number) =>
      fillColorForWindow(weekly(40, 20, { band: 'ahead', pressure }), NOW);
    expect(at(PACE_PANIC_AMBER_LN)).toBe(css(PACE_COLOR_OK));
    expect(at(mid)).toBe(css(PACE_COLOR_AHEAD));
    expect(at(PACE_PANIC_RED_LN)).toBe(css(PACE_COLOR_HOT));
  });

  it('follows the served vertices, not the built-in ones', () => {
    applyThresholds({ panic_amber_ln: 0.2, panic_red_ln: 0.6 });
    const w = weekly(40, 20, { band: 'ahead', pressure: 0.4 });
    expect(fillColorForWindow(w, NOW)).toBe(css(PACE_COLOR_AHEAD));
  });

  it('paints a served ahead with no pressure flat amber, and a served hot red', () => {
    expect(fillColorForWindow(weekly(40, 20, { band: 'ahead' }), NOW)).toBe(css(PACE_COLOR_AHEAD));
    // Low ratio on purpose: the band, not the arithmetic, makes it red.
    expect(fillColorForWindow(weekly(10, 50, { band: 'hot' }), NOW)).toBe(css(PACE_COLOR_HOT));
  });
});
