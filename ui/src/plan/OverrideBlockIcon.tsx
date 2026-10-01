// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * 🎯T982: the prohibition sign on a plan box whose override blocks the plan
 * outright — a ring with one diagonal from top-left to bottom-right, the ⊘
 * family (Material "block", Font Awesome "ban"), not a stop sign. Drawn in
 * currentColor so cockpit.css decides the ink; the ring and the bar share
 * one stroke so the sign reads at the 12px dot scale of the ticker strip.
 */
export const BLOCK_ICON_VIEWBOX = 16;
const CENTRE = BLOCK_ICON_VIEWBOX / 2;
const STROKE = 2;
/** The ring's radius, inset so the stroke stays inside the viewBox. */
const RADIUS = CENTRE - STROKE / 2 - 0.5;
/** Where the diagonal meets the ring: RADIUS along the 45° line from the centre. */
const DIAGONAL_END = RADIUS / Math.SQRT2;

export function OverrideBlockIcon() {
  const vb = '0 0 ' + BLOCK_ICON_VIEWBOX + ' ' + BLOCK_ICON_VIEWBOX;
  return (
    <svg className="plan-override-ban" viewBox={vb} aria-hidden="true" focusable="false">
      <circle cx={CENTRE} cy={CENTRE} r={RADIUS} fill="none" stroke="currentColor" strokeWidth={STROKE} />
      <line
        x1={CENTRE - DIAGONAL_END} y1={CENTRE - DIAGONAL_END}
        x2={CENTRE + DIAGONAL_END} y2={CENTRE + DIAGONAL_END}
        stroke="currentColor" strokeWidth={STROKE}
      />
    </svg>
  );
}
