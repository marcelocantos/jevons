// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  distanceFromEnd,
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
