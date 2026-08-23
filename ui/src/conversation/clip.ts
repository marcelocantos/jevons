// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** 🎯T106 / T537.2: one size-clip decision for AgentTranscript. */

export const DEFAULT_COLLAPSED_PX = 224;
export const COLLAPSE_EPS_PX = 1;

export function shouldClip(
  fullH: number,
  collapsedH: number = DEFAULT_COLLAPSED_PX,
): boolean {
  if (!(fullH > 0) || !(collapsedH > 0)) return false;
  return fullH > collapsedH + COLLAPSE_EPS_PX;
}

export function clipClassName(base: string, fullH: number, collapsedH?: number): string {
  if (!shouldClip(fullH, collapsedH)) return base;
  return base ? `${base} msg-clipped` : 'msg-clipped';
}

/** Vanilla pocket-tab glyphs (U+25B4 ▴ / U+25BE ▾). */
export function expandTabChevron(expanded: boolean): string {
  return expanded ? '\u25B4' : '\u25BE';
}
