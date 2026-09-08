// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { burnPaths, BURN_HEIGHT, BURN_WIDTH } from './burnGeom';
import type { PlanWindow } from './tickerGroups';

/** Tiny area sparkline for one tooltip column (🎯T634). Empty when no samples. */
export function BurnChart(props: { window: PlanWindow }) {
  const spec = burnPaths(props.window);
  if (!spec) return null;
  return (
    <svg
      className="plan-burn-svg"
      viewBox={`0 0 ${BURN_WIDTH} ${BURN_HEIGHT}`}
      preserveAspectRatio="none"
      aria-hidden="true"
    >
      <path className="plan-burn-fill" d={spec.fill} />
      <path className="plan-burn-line" d={spec.line} />
    </svg>
  );
}
