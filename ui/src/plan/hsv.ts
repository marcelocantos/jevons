// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** HSV lerp primitives for plan-usage bar colour (🎯T390.1.2). */

export type RGB = { r: number; g: number; b: number };
export type HSV = { h: number; s: number; v: number };

function clamp01(x: number): number {
  if (x < 0) return 0;
  if (x > 1) return 1;
  return x;
}

/** Shortest-path hue interpolation, degrees in [0, 360). */
export function lerpHue(h1: number, h2: number, t: number): number {
  let d = h2 - h1;
  if (d > 180) d -= 360;
  if (d < -180) d += 360;
  let h = h1 + d * t;
  h %= 360;
  if (h < 0) h += 360;
  return h;
}

export function rgbToHsv(c: RGB): HSV {
  const r = clamp01(c.r / 255);
  const g = clamp01(c.g / 255);
  const b = clamp01(c.b / 255);
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const d = max - min;
  let h = 0;
  if (d !== 0) {
    if (max === r) {
      h = 60 * (((g - b) / d) % 6);
    } else if (max === g) {
      h = 60 * ((b - r) / d + 2);
    } else {
      h = 60 * ((r - g) / d + 4);
    }
    if (h < 0) h += 360;
  }
  const s = max === 0 ? 0 : d / max;
  return { h, s, v: max };
}

export function hsvToRgb(hsv: HSV): RGB {
  const h = ((hsv.h % 360) + 360) % 360;
  const s = clamp01(hsv.s);
  const v = clamp01(hsv.v);
  const c = v * s;
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
  const m = v - c;
  let r = 0;
  let g = 0;
  let b = 0;
  if (h < 60) {
    r = c; g = x;
  } else if (h < 120) {
    r = x; g = c;
  } else if (h < 180) {
    g = c; b = x;
  } else if (h < 240) {
    g = x; b = c;
  } else if (h < 300) {
    r = x; b = c;
  } else {
    r = c; b = x;
  }
  return {
    r: Math.round((r + m) * 255),
    g: Math.round((g + m) * 255),
    b: Math.round((b + m) * 255),
  };
}

export function hsvLerpRgb(a: RGB, b: RGB, t: number): RGB {
  const u = clamp01(t);
  const ha = rgbToHsv(a);
  const hb = rgbToHsv(b);
  return hsvToRgb({
    h: lerpHue(ha.h, hb.h, u),
    s: ha.s + (hb.s - ha.s) * u,
    v: ha.v + (hb.v - ha.v) * u,
  });
}

export function rgbToCss(c: RGB): string {
  return 'rgb(' + c.r + ', ' + c.g + ', ' + c.b + ')';
}

export function parseCssColor(raw: string): RGB | null {
  const s = String(raw || '').trim();
  if (!s) return null;
  const hex = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(s);
  if (hex) {
    let h = hex[1];
    if (h.length === 3) {
      h = h[0] + h[0] + h[1] + h[1] + h[2] + h[2];
    }
    return {
      r: parseInt(h.slice(0, 2), 16),
      g: parseInt(h.slice(2, 4), 16),
      b: parseInt(h.slice(4, 6), 16),
    };
  }
  const rgb = /^rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/i.exec(s);
  if (rgb) {
    return { r: Number(rgb[1]), g: Number(rgb[2]), b: Number(rgb[3]) };
  }
  return null;
}
