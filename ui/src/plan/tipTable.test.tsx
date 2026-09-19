// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { render } from '@testing-library/react';
import {
  PlanTipTable,
  humanDuration,
  pct,
  rolloverCell,
  timeLeft,
  tipColumns,
  unavailableNotes,
  windowLabel,
} from './tipTable';
import type { TickerGroup } from './tickerGroups';
import { formatWindow } from './pace';

const NOW = Date.parse('2026-08-31T00:00:00Z');
const hoursOut = (h: number) => new Date(NOW + h * 3600_000).toISOString();

const FLEET = [
  {
    provider: 'claude',
    available: true,
    windows: [
      { name: 'session', remaining_percent: 76, used_percent: 24, resets_at: hoursOut(14) },
      { name: 'weekly', remaining_percent: 25, used_percent: 75, resets_at: hoursOut(12) },
    ],
  },
  {
    provider: 'codex',
    available: true,
    windows: [{ name: 'weekly', remaining_percent: 85, used_percent: 15, resets_at: hoursOut(24 * 6 + 8) }],
  },
  { provider: 'bedrock', available: false, reason: 'no subscription surface', windows: [] },
] as unknown as TickerGroup[];

describe('plan tooltip table (🎯T588.1)', () => {
  it('gives claude two columns under one mark and codex one', () => {
    const header = tipColumns(FLEET);
    expect(header.map((h) => h.provider)).toEqual(['claude', 'codex']);
    expect(header[0].columns.map((c) => c.label)).toEqual(['session', 'week']);
    expect(header[1].columns).toHaveLength(1);
  });

  it('names the weekday inside a week and the date beyond one', () => {
    // 'Tue 14:20' answers "when" without arithmetic; past a week a weekday
    // is ambiguous — which Tuesday? — so the date replaces it.
    // 🎯T670: the cell also carries how long is left, so the owner does not
    // subtract dates in their head.
    expect(rolloverCell(hoursOut(14), NOW, 'UTC')).toBe('Mon 14:00 14h');
    expect(rolloverCell(hoursOut(24 * 6 + 8), NOW, 'UTC')).toBe('Sun 08:00 6d');
    expect(rolloverCell(hoursOut(24 * 8), NOW, 'UTC')).toBe('8 Sep 00:00 8d');
  });

  it('renders the rollover in the viewer zone, not UTC', () => {
    expect(rolloverCell('2026-08-31T00:52:00Z', NOW, 'Australia/Melbourne')).toBe('Mon 10:52 52m');
    expect(rolloverCell('2026-08-31T00:52:00Z', NOW, 'America/Los_Angeles')).toBe('Sun 17:52 52m');
  });

  it('says the duration the way the owner would', () => {
    expect(humanDuration(0)).toBe('now');
    expect(humanDuration(12 * 60)).toBe('12m');
    expect(humanDuration(3600 + 37 * 60)).toBe('1h37m');
    expect(humanDuration(2 * 86400 + 4 * 3600)).toBe('2d 4h');
  });

  it('shows an em dash where the feed published nothing, never a zero', () => {
    // A missing percentage rendered as 0% reads as "spent", which is the
    // opposite of "unknown" and would be acted on.
    expect(pct(null)).toBe('—');
    expect(pct(undefined)).toBe('—');
    expect(pct(0)).toBe('0%');
    expect(timeLeft({ resets_at: null }, NOW)).toBe('—');
    expect(timeLeft({ resets_at: 'not-a-date' }, NOW)).toBe('—');
    expect(rolloverCell(null, NOW)).toBe('—');
  });

  it('keeps an unavailable provider visible as a note rather than dropping it', () => {
    expect(tipColumns(FLEET).some((h) => h.provider === 'bedrock')).toBe(false);
    expect(unavailableNotes(FLEET)).toEqual(['bedrock: unavailable — no subscription surface']);
  });

  it('labels weekly as week and monthly as month', () => {
    expect(windowLabel('weekly')).toBe('week');
    expect(windowLabel('monthly')).toBe('month');
    expect(windowLabel('session')).toBe('session');
  });

  it('paints one row per measure, one column per window', () => {
    const { container } = render(<PlanTipTable groups={FLEET} nowMs={NOW} timeZone="UTC" />);
    const rowLabels = [...container.querySelectorAll('th[scope="row"]')].map((e) => e.textContent);
    // 🎯T670: usage leads, available and time-left are gone, and the
    // rollover cell carries the span.
    expect(rowLabels).toEqual(['usage', 'rollover', 'burn']);
    const firstRow = [...container.querySelectorAll('tbody tr')][0];
    expect([...firstRow.querySelectorAll('td')].map((e) => e.textContent)).toEqual(['24%', '75%', '15%']);
    // The single-window provider's mark spans both header rows, so the
    // second header row carries only claude's two window labels.
    const winHeads = [...container.querySelectorAll('.plan-tip-win')].map((e) => e.textContent);
    expect(winHeads).toEqual(['session', 'week']);
  });

  it('still says something when no provider published a window', () => {
    const { container } = render(
      <PlanTipTable groups={[{ provider: 'bedrock', available: false, reason: 'x', windows: [] }] as unknown as TickerGroup[]} nowMs={NOW} />,
    );
    expect(container.textContent).toMatch(/bedrock: unavailable/);
  });
});

// 🎯T588.2: the usage figure must carry the bar's own pace class, or
// the number and the bar above it can disagree about the same window.
describe('usage wears the bar colour (🎯T588.2 / 🎯T670)', () => {
  it('puts the pace class on the usage cell only', () => {
    const hot = [
      {
        provider: 'claude',
        available: true,
        windows: [{ name: 'weekly', remaining_percent: 2, used_percent: 98, resets_at: hoursOut(3) }],
      },
    ] as unknown as TickerGroup[];
    const { container } = render(<PlanTipTable groups={hot} nowMs={NOW} timeZone="UTC" />);
    const avail = container.querySelector('td.plan-avail');
    expect(avail).toBeTruthy();
    expect(avail?.textContent).toBe('98%');
    // The cell's class is whatever the bar would paint for this window —
    // asserted against pace.ts itself rather than a hardcoded name, so the
    // test pins the wiring (they cannot drift apart) without freezing the
    // threshold policy that decides which colour it is.
    const expected = formatWindow(hot[0].windows[0], NOW).className;
    expect(avail?.className).toBe(('plan-avail ' + expected).trim());
    // Only the usage row is marked; rollover stays plain.
    expect(container.querySelectorAll('td.plan-avail')).toHaveLength(1);
    const burn = container.querySelector('td.plan-burn');
    expect(burn?.className).toBe(('plan-burn ' + expected).trim());
    expect(burn?.querySelector('rect.plan-burn-plot')).toBeTruthy();
    expect(burn?.querySelector('path.plan-burn-line')).toBeNull();
  });
});

describe('burn-down row (🎯T634)', () => {
  it('plots only fixture samples and keeps an empty plot frame when there are none', () => {
    const reset = hoursOut(7 * 24);
    const start = NOW;
    const withHist = [
      {
        provider: 'claude',
        available: true,
        windows: [
          {
            name: 'weekly',
            remaining_percent: 50,
            used_percent: 50,
            resets_at: reset,
            limit_window_seconds: 7 * 24 * 3600,
            history: [
              { at: new Date(start + 2 * 3600_000).toISOString(), remaining_percent: 80 },
              { at: new Date(start + 4 * 3600_000).toISOString(), remaining_percent: 50 },
            ],
          },
        ],
      },
    ] as unknown as TickerGroup[];
    const empty = [
      {
        provider: 'codex',
        available: true,
        windows: [{ name: 'weekly', remaining_percent: 85, used_percent: 15, resets_at: reset }],
      },
    ] as unknown as TickerGroup[];

    const painted = render(<PlanTipTable groups={withHist} nowMs={NOW} timeZone="UTC" />);
    const svg = painted.container.querySelector('td.plan-burn svg.plan-burn-svg');
    expect(svg).toBeTruthy();
    const plot = svg?.querySelector('rect.plan-burn-plot');
    expect(plot).toBeTruthy();
    expect(plot?.getAttribute('width')).toBe('100');
    expect(plot?.getAttribute('height')).toBe('32');
    const line = svg?.querySelector('path.plan-burn-line')?.getAttribute('d') || '';
    expect(line.startsWith('M')).toBe(true);
    expect(line.split(/[ML]/).filter(Boolean)).toHaveLength(2);

    const blank = render(<PlanTipTable groups={empty} nowMs={NOW} timeZone="UTC" />);
    const emptySvg = blank.container.querySelector('td.plan-burn svg.plan-burn-svg');
    expect(emptySvg?.querySelector('rect.plan-burn-plot')).toBeTruthy();
    expect(emptySvg?.querySelector('path.plan-burn-line')).toBeNull();
  });
});
