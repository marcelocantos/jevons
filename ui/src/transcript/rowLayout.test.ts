// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  BUBBLE_BOTTOM_CHROME_PX,
  COMFORTABLE_ROW_GAP_PX,
  DEFAULT_ROW_GAP_PX,
  alreadySnappedLock,
  idleSnapShouldSkipClear,
  nextSnappedMinHeightCss,
  rowLayoutHeight,
  snappedNaturalBox,
} from './rowLayout';

describe('rowLayoutHeight', () => {
  it('matches the old VirtualList extent cases (🎯T119.4)', () => {
    expect(BUBBLE_BOTTOM_CHROME_PX).toBe(19);
    expect(DEFAULT_ROW_GAP_PX).toBe(8);
    expect(COMFORTABLE_ROW_GAP_PX).toBe(9);
    expect(rowLayoutHeight(268)).toBe(268);
    expect(rowLayoutHeight(268, { marginBottomPx: 19 })).toBe(287);
    expect(rowLayoutHeight(268, { marginBottomPx: 19, timeOverflowPx: 13 })).toBe(287);
    expect(rowLayoutHeight(268, { marginBottomPx: 19, timeOverflowPx: 24 })).toBe(292);
    expect(rowLayoutHeight(13.2)).toBe(13.2);
    expect(
      rowLayoutHeight(268, { tabOverflowPx: 10, marginTopPx: 10, marginBottomPx: 19 }),
    ).toBe(297);
  });

  it('ceils the natural border-box (🎯T351)', () => {
    expect(snappedNaturalBox(66.78125)).toBe(67);
    expect(snappedNaturalBox(13.1875)).toBe(14);
    expect(snappedNaturalBox(0)).toBe(0);
  });
});

describe('T482 snap no-op write', () => {
  it('second pass on the same natural does not change css', () => {
    const first = nextSnappedMinHeightCss('', 54.8);
    expect(first.css).toBe('55px');
    expect(first.changed).toBe(true);
    const second = nextSnappedMinHeightCss(first.css, 54.8);
    expect(second.changed).toBe(false);
  });
});

describe('T486 shrink-on-oversize', () => {
  it('an oversized lock shrinks to the natural box', () => {
    const shrink = nextSnappedMinHeightCss('200px', 80);
    expect(shrink.css).toBe('80px');
    expect(shrink.changed).toBe(true);
  });
});

describe('T532 idle snap skip clear', () => {
  it('skips when locked box matches snap and inner is stable', () => {
    expect(idleSnapShouldSkipClear(55, 55, 40, 40)).toBe(true);
    expect(idleSnapShouldSkipClear(55, 54.8, 40, 40)).toBe(true);
    expect(idleSnapShouldSkipClear(200, 200, 80, 180)).toBe(false);
    expect(idleSnapShouldSkipClear(55, 80, 80, 40)).toBe(false);
    expect(idleSnapShouldSkipClear(55, 55, 40, 0)).toBe(false);
    expect(alreadySnappedLock(0, 55)).toBe(false);
    expect(alreadySnappedLock(55, 55)).toBe(true);
  });
});
