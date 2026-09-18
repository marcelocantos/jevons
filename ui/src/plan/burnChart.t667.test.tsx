// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { BurnChart } from './BurnChart';
import { burnStops } from './burnGeom';
import { CLASS_AHEAD, CLASS_HOT, CLASS_LOCKED, CLASS_UNDER, paceClassForBand } from './pace';
import type { PlanWindow } from './tickerGroups';

// 🎯T667: the sparkline shifts colour along the period from the band the
// daemon stamped on each sample, instead of one colour for the whole curve.
// Colours come from the bar's own chain (served band → pace → class) and
// cockpit.css; this file never names a colour.

const start = Date.parse('2026-09-10T00:00:00Z');
const day = 24 * 3600 * 1000;
const iso = (ms: number) => new Date(ms).toISOString();

function week(history: PlanWindow['history']): PlanWindow {
  return {
    name: 'weekly',
    remaining_percent: 3,
    resets_at: iso(start + 7 * day),
    limit_window_seconds: 7 * 24 * 3600,
    history,
  } as PlanWindow;
}

describe('burn chart colour follows the band over time (🎯T667)', () => {
  it('reuses the served-band chain the bar paints with', () => {
    expect(paceClassForBand('ok')).toBe('');
    expect(paceClassForBand('ahead')).toBe(CLASS_AHEAD);
    expect(paceClassForBand('hot')).toBe(CLASS_HOT);
    expect(paceClassForBand('exhausted')).toBe(CLASS_HOT);
    expect(paceClassForBand('under')).toBe(CLASS_UNDER);
    expect(paceClassForBand('locked')).toBe(CLASS_LOCKED);
    expect(paceClassForBand('')).toBeNull();
    expect(paceClassForBand(undefined)).toBeNull();
  });

  it('places one stop per sample at its x, ok early and hot late', () => {
    const stops = burnStops(
      week([
        { at: iso(start + 1 * day), remaining_percent: 86, band: 'ok' },
        { at: iso(start + 3 * day), remaining_percent: 55, band: 'ahead' },
        { at: iso(start + 6 * day), remaining_percent: 3, band: 'hot' },
      ]),
    );
    expect(stops.map((s) => s.className)).toEqual(['', CLASS_AHEAD, CLASS_HOT]);
    expect(stops[0].offset).toBeCloseTo(1 / 7, 3);
    expect(stops[2].offset).toBeCloseTo(6 / 7, 3);
  });

  it('paints the line and fill through a gradient whose stops carry the pace classes', () => {
    const { container } = render(
      <BurnChart
        window={week([
          { at: iso(start + 1 * day), remaining_percent: 86, band: 'ok' },
          { at: iso(start + 6 * day), remaining_percent: 40, band: 'under' },
        ])}
      />,
    );
    const grad = container.querySelector('linearGradient');
    expect(grad).not.toBeNull();
    const id = grad!.getAttribute('id')!;
    const classes = [...grad!.querySelectorAll('stop')].map((s) => s.getAttribute('class'));
    expect(classes).toEqual(['plan-burn-stop', 'plan-burn-stop ' + CLASS_UNDER]);
    // Browsers and jsdom may serialise url(#id) as url("#id").
    const ref = new RegExp(`^url\\("?#${id}"?\\)$`);
    expect((container.querySelector('.plan-burn-line') as SVGPathElement).style.stroke).toMatch(ref);
    // 🎯T671: the line is the whole chart; there is no shaded area to paint.
    expect(container.querySelector('.plan-burn-fill')).toBeNull();
  });

  it('keeps the single inherited colour when no sample carries a band (older daemon)', () => {
    const { container } = render(
      <BurnChart
        window={week([
          { at: iso(start + 1 * day), remaining_percent: 86 },
          { at: iso(start + 6 * day), remaining_percent: 40 },
        ])}
      />,
    );
    expect(container.querySelector('linearGradient')).toBeNull();
    expect((container.querySelector('.plan-burn-line') as SVGPathElement).style.stroke).toBe('');
  });

  it('gives a stem-width cluster the latest band, flat', () => {
    const t = start + 6 * day;
    const stops = burnStops(
      week([
        { at: iso(t), remaining_percent: 40, band: 'ok' },
        { at: iso(t + 60_000), remaining_percent: 39, band: 'ahead' },
      ]),
    );
    expect(stops).toEqual([
      { offset: 0, className: CLASS_AHEAD },
      { offset: 1, className: CLASS_AHEAD },
    ]);
  });

  it('leaves the column dividers neutral — a pace colour is data, not furniture (🎯T668)', () => {
    const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../cockpit.css'), 'utf8');
    const rule = /\.plan-tip-table thead th,\s*\.plan-tip-table tbody td \{([^}]*)\}/.exec(css);
    expect(rule, 'tip-table border rule').toBeTruthy();
    expect(rule![1]).toContain('border-color');
    // currentColor here is the cell's pace colour, which drew red and blue
    // dividers between the columns.
    expect(rule![1]).not.toContain('currentColor');
  });

  it('colours the stops from the same cockpit.css rules as the cell — no second palette', () => {
    const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../cockpit.css'), 'utf8');
    for (const cls of [CLASS_AHEAD, CLASS_HOT, CLASS_UNDER, CLASS_LOCKED]) {
      expect(css).toContain(`.plan-tip-table td.plan-burn .plan-burn-stop.${cls}`);
    }
    expect(css).toMatch(/\.plan-burn-stop \{[^}]*stop-color: currentColor/);
    const burnGeom = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'burnGeom.ts'), 'utf8');
    expect(burnGeom).not.toMatch(/var\(--/);
  });
});
