// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  BUBBLE_BOTTOM_CHROME_PX,
  COMFORTABLE_ROW_GAP_PX,
  DEFAULT_ROW_GAP_PX,
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
