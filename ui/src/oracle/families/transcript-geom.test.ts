// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { anyPartInViewport, isNearTranscriptEnd } from '../../conversation/clip';
import { displayRows } from '../../conversation/display';
import { shouldRequestPage } from '../../conversation/page';
import { pageScrollDelta } from '../../keys/pageScroll';
import { FOLLOW_END_PX, distanceFromEnd, followAfterScroll, pinWriteScrollTop, shouldHoldFollow } from '../../transcript/followPin';
import {
  DEFAULT_ROW_OVERSCAN,
  HYDRATE_BAND_VIEWPORTS,
  HYDRATE_OVERSCAN_MAX,
  measuredSuffixFromEnd,
  nextHydrateOverscan,
  standingOverscan,
} from '../../transcript/hydrateOverscan';
import { pinnedFirstY, pinnedLastY, type PinGeometry } from '../../transcript/pinGeometry';
import { DEFAULT_ROW_GAP_PX, rowLayoutHeight, snappedNaturalBox } from '../../transcript/rowLayout';
import { family } from '../catalog';
import { assistantProse, assistantTool, userTurn } from '../fixtures';
import { describeOracle, itOracle } from '../harness';

/** Tanstack residency: viewport rows plus overscan each side (T56 / T119.3). */
function materialisedCount(n: number, clientHeight: number, estimate: number, overscan: number): number {
  const est = Math.max(8, Number(estimate) || 8);
  const vis = Math.ceil(Math.max(0, clientHeight) / est) + 1;
  return Math.min(Math.max(0, n), vis + 2 * Math.max(0, overscan));
}

function canvasHeight(extents: readonly number[], gap: number): number {
  if (!extents.length) return 0;
  return extents.reduce((s, h) => s + h, 0) + gap * (extents.length - 1);
}

function prefixTops(extents: readonly number[], gap: number): number[] {
  const tops: number[] = [];
  let y = 0;
  for (let i = 0; i < extents.length; i++) {
    tops.push(y);
    y += extents[i] + (i < extents.length - 1 ? gap : 0);
  }
  return tops;
}

const VIEW: Omit<PinGeometry, 'contentHeight' | 'paddingEnd' | 'lastHeight'> = {
  messagesTop: 34,
  padTop: 24,
  padBottom: 24,
  clientHeight: 789,
};

describeOracle(family('transcript-geom'), () => {
  itOracle('T336', 'PageUp/PageDown step is ~0.8 of the viewport', () => {
    expect(pageScrollDelta('PageUp', 1000)).toBe(-800);
    expect(pageScrollDelta('PageDown', 1000)).toBe(800);
    expect(pageScrollDelta('Home', 1000)).toBe(0);
  });

  itOracle('T351', 'pin write uses full scrollHeight so the browser clamps the fractional max', () => {
    expect(pinWriteScrollTop(1234.7)).toBe(1234.7);
    expect(pinWriteScrollTop(0)).toBe(0);
    expect(pinWriteScrollTop(-5)).toBe(0);
    expect(snappedNaturalBox(66.78125)).toBe(67);
    expect(snappedNaturalBox(13.1875)).toBe(14);
  });

  itOracle('T30.2', 'in-flight follow-scroll stays pinned to the true bottom until seal', () => {
    const fromBottom = 480;
    expect(shouldHoldFollow({ fromBottom, pinning: true })).toBe(true);
    expect(
      shouldHoldFollow({ fromBottom, pinning: false, wasFollowing: true, heightGrew: true }),
    ).toBe(true);
    const grew = followAfterScroll({
      fromBottom,
      pinning: true,
      wasFollowing: true,
      prevHeight: 2000,
      scrollHeight: 2800,
    });
    expect(grew.follow).toBe(true);
    expect(pinWriteScrollTop(2800)).toBe(2800);
    expect(distanceFromEnd(2200, 2800, 600)).toBe(0);
    expect(
      shouldHoldFollow({ fromBottom, pinning: false, wasFollowing: true, heightGrew: false }),
    ).toBe(false);
    expect(shouldHoldFollow({ fromBottom: FOLLOW_END_PX - 1, pinning: false })).toBe(true);
  });

  itOracle('T56', 'only on-screen messages are in the DOM', () => {
    const ch = 800;
    const estimate = 72;
    const over = standingOverscan(ch);
    const band = materialisedCount(2000, ch, estimate, over);
    expect(band).toBeLessThan(250);
    expect(band).toBe(materialisedCount(500, ch, estimate, over));
    expect(materialisedCount(8, ch, estimate, over)).toBe(8);
    expect(anyPartInViewport(400, 50, 0, 300)).toBe(false);
    expect(anyPartInViewport(100, 50, 0, 300)).toBe(true);
    expect(HYDRATE_OVERSCAN_MAX).toBeLessThan(200);
    expect(over).toBeLessThanOrEqual(HYDRATE_OVERSCAN_MAX);
  });

  itOracle('T119', 'history is windowed, recent-first, whole-chunk', () => {
    const rows = displayRows([
      userTurn('hello'),
      assistantProse('# Title\n\nbody paragraph'),
      userTurn('next'),
      assistantProse('reply two'),
    ]);
    expect(rows.map((r) => r.kind)).toEqual(['user', 'assistant', 'user', 'assistant']);
    expect(rows.map((r) => r.text)).toEqual(['hello', '# Title\n\nbody paragraph', 'next', 'reply two']);
    expect(
      displayRows([userTurn('go'), assistantTool('Read'), assistantProse('done')]).map((r) => r.kind),
    ).toEqual(['user', 'steps', 'assistant']);

    const measured = new Map<number, number>([
      [197, 40],
      [198, 50],
      [199, 60],
    ]);
    const suf = measuredSuffixFromEnd(measured, 200);
    expect(suf.px).toBe(150);
    expect(measuredSuffixFromEnd(new Map([[0, 40]]), 200).px).toBe(0);

    const ch = 800;
    const plan = nextHydrateOverscan({
      clientHeight: ch,
      count: 200,
      current: DEFAULT_ROW_OVERSCAN,
      measuredFromEndPx: 400,
      estimate: 72,
    });
    expect(plan.settled).toBe(false);
    expect(materialisedCount(200, ch, 72, standingOverscan(ch))).toBeLessThan(200);
  });

  itOracle('T119.1', 'reload/reconnect does not scroll-parade to the live end', () => {
    expect(shouldHoldFollow({ fromBottom: 480, pinning: true })).toBe(true);
    expect(pinWriteScrollTop(4000)).toBe(4000);
    expect(pinWriteScrollTop(1000)).toBe(1000);
    const first = nextHydrateOverscan({
      clientHeight: 800,
      count: 2000,
      current: HYDRATE_OVERSCAN_MAX,
      measuredFromEndPx: 0,
      estimate: 72,
    });
    expect(first.settled).toBe(false);
    expect(first.overscan).toBeLessThanOrEqual(HYDRATE_OVERSCAN_MAX);
    const suf = measuredSuffixFromEnd(
      new Map([
        [1998, 72],
        [1999, 72],
      ]),
      2000,
    );
    expect(suf.px).toBe(144);
    expect(measuredSuffixFromEnd(new Map([[0, 72]]), 2000).px).toBe(0);
    expect(
      followAfterScroll({
        fromBottom: 0,
        pinning: true,
        wasFollowing: true,
        prevHeight: 0,
        scrollHeight: 4000,
      }).follow,
    ).toBe(true);
  });

  itOracle('T119.3', 'absolute-position virtual list: O(viewport) nodes', () => {
    const heights = [100, 50, 200];
    const tops = prefixTops(heights, DEFAULT_ROW_GAP_PX);
    expect(tops).toEqual([0, 108, 166]);
    expect(canvasHeight(heights, DEFAULT_ROW_GAP_PX)).toBe(366);
    const n = 1000;
    const ch = 600;
    const mat = materialisedCount(n, ch, 72, standingOverscan(ch));
    expect(mat).toBeLessThan(250);
    expect(mat).toBeLessThan(n / 4);
    expect(materialisedCount(n, ch, 72, HYDRATE_OVERSCAN_MAX)).toBeLessThan(n / 3);
  });

  itOracle('T341', 'main chat text does not jiggle from pin/reflow thrash', () => {
    expect(shouldHoldFollow({ fromBottom: 480, pinning: true })).toBe(true);
    expect(
      shouldHoldFollow({ fromBottom: 480, pinning: false, wasFollowing: true, heightGrew: true }),
    ).toBe(true);
    const natural = snappedNaturalBox(66.78125);
    const chrome = { marginBottomPx: 19 };
    const extent = rowLayoutHeight(natural, chrome);
    expect(rowLayoutHeight(natural, chrome)).toBe(extent);
    expect(rowLayoutHeight(extent, chrome)).toBeGreaterThan(extent);
    const base = { ...VIEW, contentHeight: 755, paddingEnd: 0, lastHeight: 13 };
    const extra = { ...base, paddingEnd: 14 };
    expect(pinnedLastY(extra) - pinnedLastY(base)).toBe(-14);
    expect(pinnedFirstY(extra) - pinnedFirstY(base)).toBe(-14);
    const tall = { ...base, contentHeight: 765 };
    expect(pinnedLastY(tall)).toBe(pinnedLastY(base));
  });

  itOracle('T347', 'reload paints end-first and only materializes viewport plus a few rows', () => {
    const ch = 800;
    const n = 2000;
    const want = ch * HYDRATE_BAND_VIEWPORTS;
    const mid = nextHydrateOverscan({
      clientHeight: ch,
      count: n,
      current: HYDRATE_OVERSCAN_MAX,
      measuredFromEndPx: 400,
      estimate: 72,
    });
    expect(mid.settled).toBe(false);
    const done = nextHydrateOverscan({
      clientHeight: ch,
      count: n,
      current: mid.overscan,
      measuredFromEndPx: want,
      estimate: 72,
    });
    expect(done.settled).toBe(true);
    expect(done.overscan).toBe(standingOverscan(ch));
    expect(done.overscan).toBeGreaterThanOrEqual(DEFAULT_ROW_OVERSCAN);
    const mat = materialisedCount(n, ch, 72, done.overscan);
    expect(mat).toBeLessThan(n / 10);
    const tail = measuredSuffixFromEnd(
      new Map([
        [n - 2, 72],
        [n - 1, 72],
      ]),
      n,
    );
    expect(tail.px).toBe(144);
    expect(measuredSuffixFromEnd(new Map([[0, 72]]), n).complete).toBe(false);
  });

  itOracle('T363', 'scroll-up preserves viewport when older content prepends', () => {
    const ch = 800;
    const sh = 4000;
    const st = 1200;
    const before = distanceFromEnd(st, sh, ch);
    const prepended = 600;
    expect(distanceFromEnd(st + prepended, sh + prepended, ch)).toBe(before);
    expect(distanceFromEnd(st, sh + prepended, ch)).toBe(before + prepended);
    expect(
      shouldRequestPage({
        scrollTop: 10,
        older: 20,
        inFlight: false,
        following: false,
        scrollHeight: sh,
        clientHeight: ch,
      }),
    ).toBe(true);
    expect(
      shouldRequestPage({
        scrollTop: 0,
        older: 20,
        inFlight: false,
        following: true,
        scrollHeight: sh,
        clientHeight: ch,
      }),
    ).toBe(false);
    expect(pageScrollDelta('PageUp', ch)).toBe(-Math.round(ch * 0.8));
    expect(
      shouldHoldFollow({ fromBottom: 480, pinning: false, wasFollowing: true, heightGrew: false }),
    ).toBe(false);
  });

  itOracle('T491', 'connect replay is one virtual-list row per owner turn', () => {
    const rows = displayRows([
      userTurn('one'),
      assistantProse('a'),
      userTurn('two'),
      assistantProse('b'),
      userTurn('three'),
      assistantProse('c'),
    ]);
    expect(rows.filter((r) => r.kind === 'user')).toHaveLength(3);
    expect(rows.filter((r) => r.kind === 'assistant')).toHaveLength(3);
    expect(rows.map((r) => r.kind)).toEqual([
      'user',
      'assistant',
      'user',
      'assistant',
      'user',
      'assistant',
    ]);
    const echo = displayRows([userTurn('one'), userTurn('one'), assistantProse('a')]);
    expect(echo.filter((r) => r.kind === 'user')).toHaveLength(1);
    expect(
      displayRows([userTurn('go'), assistantTool('Read'), assistantTool('Bash'), assistantProse('done')]).map(
        (r) => r.kind,
      ),
    ).toEqual(['user', 'steps', 'assistant']);
  });

  itOracle('T494', 'connect shows the replay tail in the viewport, not an empty pane', () => {
    const g = { ...VIEW, contentHeight: 765, paddingEnd: 0, lastHeight: 13 };
    expect(pinnedFirstY(g)).toBe(34);
    expect(pinnedLastY(g)).toBe(786);
    expect(pinnedLastY(g)).toBeGreaterThanOrEqual(g.messagesTop);
    expect(pinnedLastY(g) + g.lastHeight).toBeLessThanOrEqual(g.messagesTop + g.clientHeight);
    expect(isNearTranscriptEnd(pinWriteScrollTop(1000) - 300, 1000, 300)).toBe(true);
    expect(distanceFromEnd(700, 1000, 300)).toBe(0);
    const ch = 800;
    const done = nextHydrateOverscan({
      clientHeight: ch,
      count: 24,
      current: 24,
      measuredFromEndPx: 600,
      estimate: 72,
      suffixComplete: true,
    });
    expect(done.settled).toBe(true);
    expect(done.overscan).toBeGreaterThan(0);
    const suf = measuredSuffixFromEnd(
      new Map([
        [22, 72],
        [23, 72],
      ]),
      24,
    );
    expect(suf.px).toBeGreaterThan(0);
  });

  itOracle('T494.1.2', 'layout is a function of transcript and width, not scroll history', () => {
    const chrome = { marginBottomPx: 19 };
    const a = [268, 72, 13.2].map((h) => rowLayoutHeight(snappedNaturalBox(h) || h, chrome));
    const b = [268, 72, 13.2].map((h) => rowLayoutHeight(snappedNaturalBox(h) || h, chrome));
    expect(a).toEqual(b);
    expect(canvasHeight(a, DEFAULT_ROW_GAP_PX)).toBe(canvasHeight(b, DEFAULT_ROW_GAP_PX));
    expect(snappedNaturalBox(80)).toBe(80);
    expect(snappedNaturalBox(200)).toBe(200);
    expect(rowLayoutHeight(80, chrome)).not.toBe(rowLayoutHeight(200, chrome));
    const short = { ...VIEW, contentHeight: 755, paddingEnd: 0, lastHeight: 13 };
    const again = { ...VIEW, contentHeight: 755, paddingEnd: 0, lastHeight: 13 };
    expect(pinnedFirstY(short)).toBe(pinnedFirstY(again));
    expect(pinnedLastY(short)).toBe(pinnedLastY(again));
    const overscanA = nextHydrateOverscan({
      clientHeight: 800,
      count: 40,
      current: 12,
      measuredFromEndPx: 2000,
      estimate: 72,
    });
    const overscanB = nextHydrateOverscan({
      clientHeight: 800,
      count: 40,
      current: 12,
      measuredFromEndPx: 2000,
      estimate: 72,
    });
    expect(overscanA).toEqual(overscanB);
  });
});
