// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  VOID_BELOW_VISIBLE_PX,
  desertGapFail,
  emptySlotDesertFail,
  latestOnHardReloadFail,
  liveEndDisagreeFail,
  maxInkGapPx,
  packedPaneFail,
  voidBelowLastFail,
  wheelDownFoughtFail,
  wheelRoundTripSnapFail,
} from './viewportCensus';

describe('T494.1 viewport census', () => {
  it('Latest on hard-reload is a fail', () => {
    expect(
      latestOnHardReloadFail({
        fabHidden: false,
        followMode: 'track',
        atBottom: false,
      }),
    ).toBe(true);
    expect(
      latestOnHardReloadFail({
        fabHidden: true,
        followMode: 'track',
        atBottom: true,
      }),
    ).toBe(false);
  });

  it('empty unlabelled slots are a desert', () => {
    expect(emptySlotDesertFail(255)).toBe(true);
    expect(emptySlotDesertFail(0)).toBe(false);
  });

  it('packed pane needs two bubbles on the oracle viewport', () => {
    expect(packedPaneFail(1, 2)).toBe(true);
    expect(packedPaneFail(3, 2)).toBe(false);
  });

  it('maxInkGap is the void between consecutive bubbles', () => {
    const desert = maxInkGapPx([
      { top: 80, bottom: 150 },
      { top: 900, bottom: 970 },
    ]);
    expect(desert).toBe(750);
    expect(desertGapFail(desert, 1000)).toBe(true);
  });

  it('void under the last turn is a fail', () => {
    expect(voidBelowLastFail(400, 1200, VOID_BELOW_VISIBLE_PX)).toBe(true);
    expect(voidBelowLastFail(16989, 17040, VOID_BELOW_VISIBLE_PX)).toBe(false);
  });

  it('wheel-down from pin must not fight', () => {
    expect(wheelDownFoughtFail(218, 22366, 22366, true)).toBe(true);
    expect(wheelDownFoughtFail(218, 22366, 22446, true)).toBe(false);
  });

  it('pin and canvas-end must agree', () => {
    expect(liveEndDisagreeFail(22366.6, 22585, 16)).toBe(true);
    expect(liveEndDisagreeFail(22585, 22585, 16)).toBe(false);
  });
});
