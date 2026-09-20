// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { hsvLerpRgb, lerpHue, parseCssColor, rgbToCss, rgbToHsv } from './hsv';

describe('HSV lerp (🎯T390.1.2)', () => {
  it('takes the short hue path green → amber (through yellow, not cyan)', () => {
    const green = parseCssColor('#4ade80');
    const amber = parseCssColor('#fbbf24');
    expect(green && amber).toBeTruthy();
    const mid = hsvLerpRgb(green!, amber!, 0.5);
    const h = rgbToHsv(mid).h;
    expect(h).toBeGreaterThan(40);
    expect(h).toBeLessThan(150);
    expect(lerpHue(142, 43, 0.5)).toBeCloseTo(92.5, 5);
    const rgbMid = {
      r: Math.round((green!.r + amber!.r) / 2),
      g: Math.round((green!.g + amber!.g) / 2),
      b: Math.round((green!.b + amber!.b) / 2),
    };
    expect(mid).not.toEqual(rgbMid);
  });

  it('parses hex and rgb and round-trips css', () => {
    expect(parseCssColor('#4ade80')).toEqual({ r: 74, g: 222, b: 128 });
    expect(parseCssColor('rgb(74, 222, 128)')).toEqual({ r: 74, g: 222, b: 128 });
    expect(rgbToCss({ r: 74, g: 222, b: 128 })).toBe('rgb(74, 222, 128)');
  });
});
