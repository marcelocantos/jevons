// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

export const RHS_MIN = 240;
export const RHS_MAX_FRAC = 0.55;
const KEY = 'jevons-rhs-width';

export function clampRhsWidth(px: number, viewport: number): number {
  const max = Math.max(RHS_MIN, Math.floor(viewport * RHS_MAX_FRAC));
  if (!(px > 0)) return 360;
  return Math.min(max, Math.max(RHS_MIN, Math.round(px)));
}

export function readRhsWidth(viewport: number): number {
  try {
    const n = Number(localStorage.getItem(KEY));
    if (n > 0) return clampRhsWidth(n, viewport);
  } catch {
    /* ignore */
  }
  return clampRhsWidth(360, viewport);
}

export function persistRhsWidth(px: number): void {
  try {
    localStorage.setItem(KEY, String(px));
  } catch {
    /* ignore */
  }
}
