// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  distanceFromEnd,
  explainedByGrowth,
  followAfterScroll,
  pinWriteScrollTop,
  shouldAbortPinForMidList,
  shouldHoldFollow,
} from './followPin';

describe('followPin', () => {
  it('over-assigns scrollHeight, not sh − ch (🎯T351)', () => {
    expect(pinWriteScrollTop(1200)).toBe(1200);
    expect(pinWriteScrollTop(0)).toBe(0);
  });

  it('keeps follow during a pin even when measure growth looks like a leave', () => {
    const fromBottom = 480; // ~⅔ of a 720px pane after last-row measure
    expect(shouldHoldFollow({ fromBottom, pinning: true })).toBe(true);
    expect(shouldHoldFollow({ fromBottom, pinning: false })).toBe(false);
    expect(shouldHoldFollow({ fromBottom: 12, pinning: false })).toBe(true);
    expect(shouldHoldFollow({ fromBottom, pinning: true, clientHeight: 720 })).toBe(true);
    // First paint: prevHeight 0, canvas already tall (🎯T558).
    expect(
      shouldHoldFollow({
        fromBottom: 4000,
        pinning: true,
        clientHeight: 720,
        prevHeight: 0,
      }),
    ).toBe(true);
    expect(
      shouldHoldFollow({
        fromBottom: 4000,
        pinning: true,
        clientHeight: 720,
        prevHeight: 12000,
        heightGrew: true,
      }),
    ).toBe(true);
    expect(
      shouldHoldFollow({
        fromBottom: 4000,
        pinning: true,
        clientHeight: 720,
        prevHeight: 12000,
        heightGrew: false,
      }),
    ).toBe(false);
  });

  it('aborts mid-list re-pin after the first pin write, not on scrollTop=0 (🎯T556 / T558)', () => {
    expect(shouldAbortPinForMidList({ scrollTop: 0, fromBottom: 4000, clientHeight: 720 })).toBe(false);
    expect(shouldAbortPinForMidList({ scrollTop: 30000, fromBottom: 4000, clientHeight: 720 })).toBe(true);
    expect(shouldAbortPinForMidList({ scrollTop: 30000, fromBottom: 12, clientHeight: 720 })).toBe(false);
  });

  it('keeps follow when the transcript grew under a tracking viewport', () => {
    const fromBottom = 480;
    expect(
      shouldHoldFollow({ fromBottom, pinning: false, wasFollowing: true, heightGrew: true }),
    ).toBe(true);
    expect(
      shouldHoldFollow({ fromBottom, pinning: false, wasFollowing: true, heightGrew: false }),
    ).toBe(false);
    expect(
      shouldHoldFollow({ fromBottom, pinning: false, wasFollowing: false, heightGrew: true }),
    ).toBe(false);
  });

  it('followAfterScroll keeps track when height grows after hydrate (🎯T494.1.3)', () => {
    const grew = followAfterScroll({
      fromBottom: 480,
      pinning: false,
      wasFollowing: true,
      prevHeight: 2000,
      scrollHeight: 2800,
    });
    expect(grew.follow).toBe(true);
    expect(grew.height).toBe(2800);
    const userLeft = followAfterScroll({
      fromBottom: 480,
      pinning: false,
      wasFollowing: true,
      prevHeight: 2800,
      scrollHeight: 2800,
    });
    expect(userLeft.follow).toBe(false);
    const firstPaint = followAfterScroll({
      fromBottom: 480,
      pinning: false,
      wasFollowing: true,
      prevHeight: 0,
      scrollHeight: 2800,
    });
    expect(firstPaint.follow).toBe(false);
    const reloadTall = followAfterScroll({
      fromBottom: 8000,
      pinning: true,
      wasFollowing: true,
      prevHeight: 0,
      scrollHeight: 12000,
      clientHeight: 720,
    });
    expect(reloadTall.follow).toBe(true);
  });

  it('distanceFromEnd is zero when pinned at integer max', () => {
    expect(distanceFromEnd(400, 1000, 600)).toBe(0);
    expect(distanceFromEnd(100, 1000, 600)).toBe(300);
  });
});


// 🎯T587: the owner kept finding the transcript a page or two up without
// ever having scrolled. Growing content moves the live end away from a
// pinned scroller by exactly the height that arrived — which the older
// rules read as "more than a viewport from the end", i.e. a user leave.
describe('growth is not a leave (🎯T587)', () => {
  const PANE = 800;

  it('holds the pin when a report taller than the pane lands', () => {
    // Pinned at the end of a 5000px canvas, then a 2000px bubble arrives.
    // scrollTop never moved; fromBottom is exactly the new content.
    expect(
      shouldAbortPinForMidList({
        scrollTop: 4200,
        fromBottom: 2000,
        clientHeight: PANE,
        scrollHeight: 7000,
        prevHeight: 5000,
      }),
    ).toBe(false);
  });

  it('still abandons the pin when the owner really scrolled up', () => {
    // Canvas unchanged at 5000; the distance cannot be growth.
    expect(
      shouldAbortPinForMidList({
        scrollTop: 1200,
        fromBottom: 3000,
        clientHeight: PANE,
        scrollHeight: 5000,
        prevHeight: 5000,
      }),
    ).toBe(true);
  });

  it('abandons the pin for distance growth cannot account for', () => {
    // 500px arrived, but we are 3000px up: the owner moved 2500 of it.
    expect(
      shouldAbortPinForMidList({
        scrollTop: 1500,
        fromBottom: 3000,
        clientHeight: PANE,
        scrollHeight: 5500,
        prevHeight: 5000,
      }),
    ).toBe(true);
  });

  it('keeps following when the scroll handler sees measure growth', () => {
    // Twenty rows re-measuring past the 72px estimate is not a gesture.
    expect(
      shouldHoldFollow({
        fromBottom: 4500,
        pinning: false,
        wasFollowing: true,
        clientHeight: PANE,
        prevHeight: 6000,
        scrollHeight: 10500,
      }),
    ).toBe(true);
  });

  it('drops follow when the owner scrolls up with a still canvas', () => {
    expect(
      shouldHoldFollow({
        fromBottom: 4500,
        pinning: false,
        wasFollowing: true,
        clientHeight: PANE,
        prevHeight: 10500,
        scrollHeight: 10500,
      }),
    ).toBe(false);
  });

  it('does not invent a baseline on first paint', () => {
    // prevHeight 0 means nothing has been measured yet; growth is unknown
    // and must not be claimed as an explanation (🎯T558).
    expect(
      explainedByGrowth({ fromBottom: 3000, scrollHeight: 9000, prevHeight: 0 }),
    ).toBe(false);
  });

  it('tolerates the sub-pixel residual a pin leaves behind', () => {
    // T351 over-assigns and the browser clamps; a following scroller is
    // a hair off the end with no growth at all.
    expect(
      explainedByGrowth({ fromBottom: 1, scrollHeight: 5000, prevHeight: 5000 }),
    ).toBe(true);
    expect(
      explainedByGrowth({ fromBottom: 400, scrollHeight: 5000, prevHeight: 5000 }),
    ).toBe(false);
  });
});
