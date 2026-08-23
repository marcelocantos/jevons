// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  distanceFromEnd,
  pinWriteScrollTop,
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

  it('distanceFromEnd is zero when pinned at integer max', () => {
    expect(distanceFromEnd(400, 1000, 600)).toBe(0);
    expect(distanceFromEnd(100, 1000, 600)).toBe(300);
  });
});
