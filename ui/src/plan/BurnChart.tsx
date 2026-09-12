// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { burnPaths, BURN_HEIGHT, BURN_WIDTH } from './burnGeom';
import type { PlanWindow } from './tickerGroups';

/** Tiny area sparkline for one tooltip column (🎯T634 / T637). Plot frame always paints. */
export function BurnChart(props: { window: PlanWindow }) {
  const spec = burnPaths(props.window);
  return (
    <svg
      className="plan-burn-svg"
      viewBox={`0 0 ${BURN_WIDTH} ${BURN_HEIGHT}`}
      preserveAspectRatio="none"
      aria-hidden="true"
    >
      <rect
        className="plan-burn-plot"
        x="0"
        y="0"
        width={BURN_WIDTH}
        height={BURN_HEIGHT}
      />
      {spec ? <path className="plan-burn-fill" d={spec.fill} /> : null}
      {spec ? <path className="plan-burn-line" d={spec.line} /> : null}
    </svg>
  );
}
