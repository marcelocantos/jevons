// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import type { PlanOverride } from './tickerGroups';

/**
 * 🎯T979: what the owner's override on a plan means for the mark on its
 * box. A forced band (ok, under, locked) keeps the plan a destination and
 * is painted in that band's own colour, the same colour its bars take. Any
 * other band (exhausted, hot, ahead, unpublished) is an outright block: the
 * plan takes no seat, and the mark must not carry a colour that could read
 * as "fine" at a glance. Mirrors planusage.IsDestBandOverride.
 */
export type OverrideMark = { kind: 'band' | 'block'; band: string };

/** The bands an override can force a plan into while it stays a destination. */
export const OVERRIDE_DEST_BANDS: readonly string[] = ['ok', 'under', 'locked'];

export function overrideMark(ov: PlanOverride): OverrideMark {
  const band = typeof ov.band === 'string' ? ov.band.trim().toLowerCase() : '';
  return { kind: OVERRIDE_DEST_BANDS.includes(band) ? 'band' : 'block', band };
}

/** The one-line heading on the ticker's tip card while the pointer is on the mark. */
export function overrideTipHeading(provider: string, ov: PlanOverride): string {
  const mark = overrideMark(ov);
  if (mark.kind === 'block') return provider + ' blocked outright by override (' + mark.band + ')';
  return provider + ' shown ' + mark.band + ' by override';
}
