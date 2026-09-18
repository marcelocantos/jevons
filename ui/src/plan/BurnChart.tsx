// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useId } from 'react';
import { burnPaths, burnStops, BURN_HEIGHT, BURN_WIDTH } from './burnGeom';
import type { PlanWindow } from './tickerGroups';

/**
 * Tiny area sparkline for one tooltip column (🎯T634 / T637). Plot frame
 * always paints. 🎯T667: when the daemon stamps a band on each sample, the
 * line and fill shift colour along the period through a horizontal gradient;
 * without bands the chart keeps its single inherited pace colour.
 */
export function BurnChart(props: { window: PlanWindow }) {
  const spec = burnPaths(props.window);
  const stops = spec ? burnStops(props.window) : [];
  const gradId = 'plan-burn-grad-' + useId().replace(/[^A-Za-z0-9_-]/g, '');
  const paint = stops.length ? `url(#${gradId})` : undefined;
  return (
    <svg
      className="plan-burn-svg"
      viewBox={`0 0 ${BURN_WIDTH} ${BURN_HEIGHT}`}
      preserveAspectRatio="none"
      aria-hidden="true"
    >
      {stops.length ? (
        <defs>
          <linearGradient id={gradId} gradientUnits="userSpaceOnUse" x1="0" y1="0" x2={BURN_WIDTH} y2="0">
            {stops.map((st, i) => (
              <stop key={i} offset={st.offset} className={('plan-burn-stop ' + st.className).trim()} />
            ))}
          </linearGradient>
        </defs>
      ) : null}
      <rect
        className="plan-burn-plot"
        x="0"
        y="0"
        width={BURN_WIDTH}
        height={BURN_HEIGHT}
      />
      {spec ? <path className="plan-burn-line" d={spec.line} style={paint ? { stroke: paint } : undefined} /> : null}
    </svg>
  );
}
