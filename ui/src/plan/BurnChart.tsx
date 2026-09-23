// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useLayoutEffect, useRef, useState } from 'react';
import { burnRuns, currentMark, BURN_HEIGHT, BURN_WIDTH } from './burnGeom';
import type { PlanWindow } from './tickerGroups';

/**
 * Tiny sparkline for one tooltip column (🎯T634 / T637). Plot frame
 * always paints, and the current reading always carries a mark (🎯T687). 🎯T667: when the daemon stamps a band on each sample, the
 * line shifts colour along the period by painting each band as its own
 * stretch. Without bands the chart keeps its single inherited pace colour.
 * A horizontal wash is not used: it would colour a circle by whatever
 * x positions the circle covers, and the end dot came out two colours.
 */
export function BurnChart(props: { window: PlanWindow }) {
  const svgRef = useRef<SVGSVGElement>(null);
  const [pixelWidth, setPixelWidth] = useState(0);
  useLayoutEffect(() => {
    const el = svgRef.current;
    if (!el) return;
    const read = () => {
      const w = el.clientWidth;
      setPixelWidth((prev) => (prev === w ? prev : w));
    };
    read();
    if (typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(read);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  // Before layout, sample at one vertex per viewBox unit. After layout,
  // and again whenever the cell width changes, sample at one pixel.
  const width = pixelWidth > 0 ? pixelWidth : BURN_WIDTH;
  const runs = burnRuns(props.window, width);
  // 🎯T687: the current reading is its own mark, drawn last so it sits in
  // front of the line and outside the plot's clip, whole even when the
  // value lands on an edge. One colour: the cell's, never a wash.
  const mark = currentMark(props.window);
  return (
    <svg
      ref={svgRef}
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
      {runs.map((run, i) => (
        <path
          key={i}
          className={run.className === null ? 'plan-burn-line' : ('plan-burn-line plan-band ' + run.className).trim()}
          d={run.d}
        />
      ))}
      {mark ? <path className="plan-burn-now" d={mark} /> : null}
    </svg>
  );
}
