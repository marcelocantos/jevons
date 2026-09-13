// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { clampPercent } from './windowGeom';

/**
 * Remaining-period triangle colour. The bar fill still paints spend-vs-time
 * pace; the triangle answers how much of this window is left.
 *
 *   100% remaining → red   (long way to go; make the tokens count)
 *    75% remaining → orange
 *    50% remaining → green
 *    25% remaining → blue
 *     0% remaining → purple (home stretch; reset is next)
 *
 * Stops use the same dark-theme hexes as --red / orange-500 / --green /
 * --plan-under / --plan-locked so the marker stays readable on both themes.
 */
export const TRI_PERIOD_STOPS: ReadonlyArray<{
  at: number;
  rgb: readonly [number, number, number];
}> = [
  { at: 100, rgb: [239, 68, 68] },
  { at: 75, rgb: [249, 115, 22] },
  { at: 50, rgb: [74, 222, 128] },
  { at: 25, rgb: [96, 165, 250] },
  { at: 0, rgb: [192, 132, 252] },
];

function rgbCss(c: readonly [number, number, number]): string {
  return `rgb(${c[0]}, ${c[1]}, ${c[2]})`;
}

function lerp(a: number, b: number, u: number): number {
  return a + (b - a) * u;
}

/** CSS `rgb()` for a remaining-period percent, or empty when unusable. */
export function triangleColorForRemaining(remainingPercent: number): string {
  const t = clampPercent(remainingPercent);
  if (t == null) return '';
  const stops = TRI_PERIOD_STOPS;
  if (t >= stops[0].at) return rgbCss(stops[0].rgb);
  const last = stops[stops.length - 1];
  if (t <= last.at) return rgbCss(last.rgb);
  for (let i = 0; i < stops.length - 1; i++) {
    const hi = stops[i];
    const lo = stops[i + 1];
    if (t <= hi.at && t >= lo.at) {
      const span = hi.at - lo.at;
      const u = span === 0 ? 0 : (hi.at - t) / span;
      return rgbCss([
        Math.round(lerp(hi.rgb[0], lo.rgb[0], u)),
        Math.round(lerp(hi.rgb[1], lo.rgb[1], u)),
        Math.round(lerp(hi.rgb[2], lo.rgb[2], u)),
      ]);
    }
  }
  return rgbCss(last.rgb);
}
