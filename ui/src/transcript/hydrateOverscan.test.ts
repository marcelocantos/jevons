// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  DEFAULT_ROW_OVERSCAN,
  HYDRATE_BAND_VIEWPORTS,
  HYDRATE_OVERSCAN_MAX,
  isHydrateSteadySize,
  measuredPxFromEnd,
  measuredSuffixFromEnd,
  nextHydrateOverscan,
  standingOverscan,
} from './hydrateOverscan';

describe('hydrateOverscan', () => {
  it('walks only the consecutive measured suffix', () => {
    const m = new Map<number, number>([
      [7, 40],
      [8, 50],
      [9, 60],
    ]);
    expect(measuredPxFromEnd(m, 10)).toBe(150);
    expect(measuredPxFromEnd(new Map([[8, 50]]), 10)).toBe(0);
  });

  it('treats unclipped-tall first paint as a gap (🎯T494.1.3)', () => {
    expect(isHydrateSteadySize(72)).toBe(true);
    expect(isHydrateSteadySize(224)).toBe(true);
    expect(isHydrateSteadySize(800)).toBe(false);
    const m = new Map<number, number>([
      [7, 40],
      [8, 800],
      [9, 60],
    ]);
    const suf = measuredSuffixFromEnd(m, 10);
    expect(suf.px).toBe(60);
    expect(suf.complete).toBe(false);
  });

  it('grows overscan until a PageUp band is measured, then collapses (🎯T494.1.3)', () => {
    const ch = 800;
    const want = ch * HYDRATE_BAND_VIEWPORTS;
    const first = nextHydrateOverscan({
      clientHeight: ch,
      count: 200,
      current: DEFAULT_ROW_OVERSCAN,
      measuredFromEndPx: 400,
      estimate: 72,
    });
    expect(first.settled).toBe(false);
    expect(first.overscan).toBeGreaterThan(DEFAULT_ROW_OVERSCAN);
    const done = nextHydrateOverscan({
      clientHeight: ch,
      count: 200,
      current: first.overscan,
      measuredFromEndPx: want,
      estimate: 72,
    });
    expect(done.settled).toBe(true);
    expect(done.overscan).toBe(standingOverscan(ch));
    expect(done.overscan).toBeGreaterThan(DEFAULT_ROW_OVERSCAN);
  });

  it('does not settle just because overscan covers the current count', () => {
    const pending = nextHydrateOverscan({
      clientHeight: 800,
      count: 30,
      current: 30,
      measuredFromEndPx: 400,
      estimate: 72,
      suffixComplete: false,
    });
    expect(pending.settled).toBe(false);
    expect(pending.overscan).toBe(30);
  });

  it('settles a short transcript once the whole suffix is steady', () => {
    const done = nextHydrateOverscan({
      clientHeight: 800,
      count: 20,
      current: 20,
      measuredFromEndPx: 600,
      estimate: 72,
      suffixComplete: true,
    });
    expect(done.settled).toBe(true);
    expect(done.overscan).toBe(Math.min(20, standingOverscan(800)));
  });

  it('keeps a PageUp-wide standing overscan on a short-row tail', () => {
    expect(standingOverscan(1036)).toBe(HYDRATE_OVERSCAN_MAX);
    expect(standingOverscan(400)).toBeGreaterThan(DEFAULT_ROW_OVERSCAN);
  });

  it('does not settle at the overscan cap while the band is short', () => {
    const stuck = nextHydrateOverscan({
      clientHeight: 800,
      count: 200,
      current: HYDRATE_OVERSCAN_MAX,
      measuredFromEndPx: 400,
      estimate: 72,
    });
    expect(stuck.settled).toBe(false);
    expect(stuck.overscan).toBe(HYDRATE_OVERSCAN_MAX);
  });

  it('does not settle while the pane has no clientHeight yet', () => {
    const pending = nextHydrateOverscan({
      clientHeight: 0,
      count: 50,
      current: DEFAULT_ROW_OVERSCAN,
      measuredFromEndPx: 0,
      estimate: 72,
    });
    expect(pending.settled).toBe(false);
  });
});
