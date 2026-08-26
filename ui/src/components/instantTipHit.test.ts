// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  HIDE_GRACE_MS,
  bridgeCorridorBetween,
  computeHitParts,
  placeCardRect,
  pointInHitParts,
  pointInHitRect,
  shouldDismissOutsideHitParts,
  unionHitRect,
} from './instantTipHit';

const card = { left: 100, top: 50, right: 300, bottom: 400 };
const id = { left: 320, top: 200, right: 360, bottom: 220 };
const name = { left: 360, top: 200, right: 500, bottom: 220 };

describe('InstantTip hit geometry (🎯T231 / T271)', () => {
  it('HIDE_GRACE_MS is 0 — flicker is a geometry bug, not a timeout', () => {
    expect(HIDE_GRACE_MS).toBe(0);
  });

  it('T231 AABB of card ∪ hosts is one rect', () => {
    const u = unionHitRect([card, id, name]);
    expect(u).toEqual({ left: 100, top: 50, right: 500, bottom: 400 });
  });

  it('T271 corridor is the gap strip, not the tall AABB over other rows', () => {
    const corridor = bridgeCorridorBetween(card, [id, name]);
    expect(corridor).toEqual({ left: 300, top: 50, right: 320, bottom: 400 });
    const parts = computeHitParts({ cardRect: card, hostRects: [id, name] });
    expect(pointInHitParts(310, 210, parts)).toBe(true);
    expect(shouldDismissOutsideHitParts(310, 210, parts)).toBe(false);
    expect(shouldDismissOutsideHitParts(310, 30, parts)).toBe(true);
    expect(shouldDismissOutsideHitParts(310, 450, parts)).toBe(true);
    expect(shouldDismissOutsideHitParts(400, 80, parts)).toBe(true);
  });

  it('host→card along the corridor stays; above/below the row band dismisses', () => {
    const parts = computeHitParts({ cardRect: card, hostRects: [id, name] });
    expect(pointInHitRect(200, 100, parts.card)).toBe(true);
    expect(pointInHitParts(340, 210, parts)).toBe(true);
    expect(shouldDismissOutsideHitParts(200, 100, parts)).toBe(false);
    expect(shouldDismissOutsideHitParts(50, 210, parts)).toBe(true);
    expect(shouldDismissOutsideHitParts(600, 210, parts)).toBe(true);
  });

  it('T184/T186 left-of-host card is clamped off the frontier table, not pinned to a corner', () => {
    const host = { left: 1600, top: 620, right: 1680, bottom: 646 };
    const pos = placeCardRect({
      placement: 'left-of-host',
      host,
      tipW: 480,
      tipH: 260,
      viewW: 1920,
      viewH: 1080,
      clampRight: 1592,
    });
    expect(pos.side).toBe('left');
    expect(pos.left + 480).toBeLessThanOrEqual(1592);
    expect(pos.left).toBeGreaterThanOrEqual(8);
    expect(pos.top).toBeGreaterThan(8);
    expect(pos.top + 260).toBeLessThanOrEqual(1080 - 8);
  });
});
