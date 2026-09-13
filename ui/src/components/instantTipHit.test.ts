// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  HIDE_GRACE_MS,
  bridgeCorridorBetween,
  cardCoversHost,
  computeHitParts,
  hitRectIsDegenerate,
  placeCardRect,
  pointInHitParts,
  pointInHitRect,
  samePointerSample,
  shouldDismissOutsideHitParts,
  shouldDismissPointerSample,
  sideTowardMid,
  stickCardRect,
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

  it('T648: same clientXY is not a leave; mermaid-grown card does not recenter as a dismiss', () => {
    expect(samePointerSample({ x: 200, y: 100 }, 200, 100)).toBe(true);
    expect(samePointerSample({ x: 200, y: 100 }, 201, 100)).toBe(false);
    expect(hitRectIsDegenerate({ left: 0, top: 0, right: 4, bottom: 4 })).toBe(true);
    const before = computeHitParts({ cardRect: card, hostRects: [id] });
    const jumped = computeHitParts({
      cardRect: { left: 100, top: 0, right: 300, bottom: 80 },
      hostRects: [id],
    });
    expect(shouldDismissPointerSample({ x: 200, y: 100, lastXY: { x: 200, y: 100 }, parts: jumped })).toBe(
      false,
    );
    expect(
      shouldDismissPointerSample({
        x: 50,
        y: 10,
        lastXY: { x: 200, y: 100 },
        parts: jumped,
        lastParts: before,
      }),
    ).toBe(true);
    const stuck = stickCardRect({ left: 120, top: 80, tipW: 400, tipH: 500, viewW: 800, viewH: 600 });
    expect(stuck.left).toBe(120);
    expect(stuck.top + 500).toBeLessThanOrEqual(600 - 8);
  });

  it('T186/T648: left-of-host never flips over the trigger, even without clamp', () => {
    const host = { left: 80, top: 200, right: 140, bottom: 226 };
    const pos = placeCardRect({
      placement: 'left-of-host',
      host,
      tipW: 400,
      tipH: 260,
      viewW: 800,
      viewH: 600,
    });
    expect(pos.side).toBe('left');
    const width = pos.maxWidth != null ? pos.maxWidth : 400;
    expect(pos.left + width).toBeLessThanOrEqual(host.left - 8);
    expect(cardCoversHost({ left: pos.left, width }, host)).toBe(false);
  });

  it('T648: mermaid growth pins the host-facing edge, not the left edge', () => {
    const host = { left: 420, top: 200, right: 480, bottom: 226 };
    const first = placeCardRect({
      placement: 'left-of-host',
      host,
      tipW: 200,
      tipH: 80,
      viewW: 800,
      viewH: 600,
      clampRight: 412,
    });
    expect(first.side).toBe('left');
    expect(first.left + 200).toBeLessThanOrEqual(412);
    const grown = stickCardRect({
      left: first.left,
      top: first.top,
      side: 'left',
      prevW: 200,
      tipW: 400,
      tipH: 240,
      viewW: 800,
      viewH: 600,
      clampRight: 412,
    });
    expect(grown.left + 400).toBeLessThanOrEqual(first.left + 200);
    expect(grown.left + (grown.maxWidth != null ? grown.maxWidth : 400)).toBeLessThanOrEqual(412);
    expect(cardCoversHost({ left: grown.left, width: grown.maxWidth != null ? grown.maxWidth : 400 }, host)).toBe(
      false,
    );
  });

  it('T326: toward-mid opens on the pane-center side and never covers the hotspot', () => {
    expect(sideTowardMid({ left: 80, top: 10, right: 140, bottom: 28 }, 800)).toBe('right');
    expect(sideTowardMid({ left: 620, top: 10, right: 700, bottom: 28 }, 800)).toBe('left');
    const leftHost = { left: 80, top: 40, right: 140, bottom: 58 };
    const rightish = placeCardRect({
      placement: 'toward-mid',
      host: leftHost,
      tipW: 280,
      tipH: 160,
      viewW: 800,
      viewH: 600,
    });
    expect(rightish.side).toBe('right');
    expect(rightish.left).toBeGreaterThanOrEqual(leftHost.right + 8);
    expect(cardCoversHost({ left: rightish.left, width: 280 }, leftHost)).toBe(false);
    const rightHost = { left: 620, top: 40, right: 700, bottom: 58 };
    const leftish = placeCardRect({
      placement: 'toward-mid',
      host: rightHost,
      tipW: 280,
      tipH: 160,
      viewW: 800,
      viewH: 600,
    });
    expect(leftish.side).toBe('left');
    expect(leftish.left + 280).toBeLessThanOrEqual(rightHost.left - 8);
    expect(cardCoversHost({ left: leftish.left, width: 280 }, rightHost)).toBe(false);
  });
});
