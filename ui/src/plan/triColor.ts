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
 *     0% remaining → hot magenta (home stretch; reset is next)
 *
 * The last stop is a bright fuchsia, not --plan-locked violet: a 4px
 * marker of muted violet goes muddy-brown on the red cockpit. Blend in
 * HSL so the blue→magenta leg stays saturated instead of greying out.
 */
export const TRI_PERIOD_STOPS: ReadonlyArray<{
  at: number;
  rgb: readonly [number, number, number];
}> = [
  { at: 100, rgb: [239, 68, 68] },
  { at: 75, rgb: [249, 115, 22] },
  { at: 50, rgb: [74, 222, 128] },
  { at: 25, rgb: [96, 165, 250] },
  { at: 0, rgb: [232, 121, 249] },
];

function rgbCss(c: readonly [number, number, number]): string {
  return `rgb(${c[0]}, ${c[1]}, ${c[2]})`;
}

function lerp(a: number, b: number, u: number): number {
  return a + (b - a) * u;
}

function rgbToHsl(r: number, g: number, b: number): [number, number, number] {
  r /= 255;
  g /= 255;
  b /= 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  if (max === min) return [0, 0, l];
  const d = max - min;
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  let h = 0;
  if (max === r) h = ((g - b) / d + (g < b ? 6 : 0)) / 6;
  else if (max === g) h = ((b - r) / d + 2) / 6;
  else h = ((r - g) / d + 4) / 6;
  return [h * 360, s, l];
}

function hslToRgb(h: number, s: number, l: number): [number, number, number] {
  const hh = ((h % 360) + 360) % 360;
  const c = (1 - Math.abs(2 * l - 1)) * s;
  const hp = hh / 60;
  const x = c * (1 - Math.abs((hp % 2) - 1));
  let r = 0;
  let g = 0;
  let b = 0;
  if (hp < 1) [r, g, b] = [c, x, 0];
  else if (hp < 2) [r, g, b] = [x, c, 0];
  else if (hp < 3) [r, g, b] = [0, c, x];
  else if (hp < 4) [r, g, b] = [0, x, c];
  else if (hp < 5) [r, g, b] = [x, 0, c];
  else [r, g, b] = [c, 0, x];
  const m = l - c / 2;
  return [Math.round((r + m) * 255), Math.round((g + m) * 255), Math.round((b + m) * 255)];
}

function parseRgb(css: string): [number, number, number] | null {
  const m = /^rgb\((\d+), (\d+), (\d+)\)$/.exec(css);
  if (!m) return null;
  return [Number(m[1]), Number(m[2]), Number(m[3])];
}

/** Channel span — a muddy brown/grey mix collapses toward 0. */
export function triangleColorChroma(css: string): number {
  const rgb = parseRgb(css);
  if (!rgb) return 0;
  return Math.max(...rgb) - Math.min(...rgb);
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
      if (t === hi.at) return rgbCss(hi.rgb);
      if (t === lo.at) return rgbCss(lo.rgb);
      const span = hi.at - lo.at;
      const u = span === 0 ? 0 : (hi.at - t) / span;
      const a = rgbToHsl(hi.rgb[0], hi.rgb[1], hi.rgb[2]);
      const b = rgbToHsl(lo.rgb[0], lo.rgb[1], lo.rgb[2]);
      return rgbCss(hslToRgb(lerp(a[0], b[0], u), lerp(a[1], b[1], u), lerp(a[2], b[2], u)));
    }
  }
  return rgbCss(last.rgb);
}
