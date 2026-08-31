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
    expect(rolloverCell(hoursOut(14), NOW, 'UTC')).toBe('Mon 14:00');
    expect(rolloverCell(hoursOut(24 * 6 + 8), NOW, 'UTC')).toBe('Sun 08:00');
    expect(rolloverCell(hoursOut(24 * 8), NOW, 'UTC')).toBe('8 Sept 00:00');
  });

  it('renders the rollover in the viewer zone, not UTC', () => {
    expect(rolloverCell('2026-08-31T00:52:00Z', NOW, 'Australia/Melbourne')).toBe('Mon 10:52');
    expect(rolloverCell('2026-08-31T00:52:00Z', NOW, 'America/Los_Angeles')).toBe('Sun 17:52');
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
    expect(rowLabels).toEqual(['tokens left', 'time left', 'tokens used', 'rollover']);
    const firstRow = [...container.querySelectorAll('tbody tr')][0];
    expect([...firstRow.querySelectorAll('td')].map((e) => e.textContent)).toEqual(['76%', '25%', '85%']);
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
