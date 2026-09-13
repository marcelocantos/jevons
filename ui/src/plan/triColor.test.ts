// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { TRI_PERIOD_STOPS, triangleColorForRemaining } from './triColor';

function rgb(c: readonly [number, number, number]): string {
  return `rgb(${c[0]}, ${c[1]}, ${c[2]})`;
}

describe('remaining-period triangle colour', () => {
  it('is red at the start of the period and purple at the end', () => {
    expect(triangleColorForRemaining(100)).toBe(rgb(TRI_PERIOD_STOPS[0].rgb));
    expect(triangleColorForRemaining(0)).toBe(rgb(TRI_PERIOD_STOPS[4].rgb));
  });

  it('hits orange, green, and blue at the named mid stops', () => {
    expect(triangleColorForRemaining(75)).toBe(rgb(TRI_PERIOD_STOPS[1].rgb));
    expect(triangleColorForRemaining(50)).toBe(rgb(TRI_PERIOD_STOPS[2].rgb));
    expect(triangleColorForRemaining(25)).toBe(rgb(TRI_PERIOD_STOPS[3].rgb));
  });

  it('clamps past the ends instead of inventing a colour', () => {
    expect(triangleColorForRemaining(140)).toBe(rgb(TRI_PERIOD_STOPS[0].rgb));
    expect(triangleColorForRemaining(-3)).toBe(rgb(TRI_PERIOD_STOPS[4].rgb));
  });

  it('returns empty when remaining is not a number', () => {
    expect(triangleColorForRemaining(Number.NaN)).toBe('');
  });

  it('moves toward purple as remaining time shrinks', () => {
    const a = triangleColorForRemaining(90);
    const b = triangleColorForRemaining(10);
    expect(a).not.toBe(b);
    expect(a).not.toBe(rgb(TRI_PERIOD_STOPS[4].rgb));
    expect(b).not.toBe(rgb(TRI_PERIOD_STOPS[0].rgb));
  });
});
