// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { formatRolloverLocal, tickerTipBody, type TickerGroup } from './tickerGroups';

// A fixed instant, stated in UTC. Every expectation below is the same
// moment seen from somewhere else — which is the whole point of 🎯T588.
const INSTANT = '2026-08-31T00:52:28.417Z';

describe('rollover is local and to the minute (🎯T588)', () => {
  it('renders the viewer wall clock, not the UTC one', () => {
    // Melbourne is UTC+10 on this date: 00:52 UTC is 10:52 the same day.
    expect(formatRolloverLocal(INSTANT, 'Australia/Melbourne')).toBe('31 Aug 10:52');
    // The zone genuinely drives it — same instant, a different day even.
    expect(formatRolloverLocal(INSTANT, 'America/Los_Angeles')).toBe('30 Aug 17:52');
    expect(formatRolloverLocal(INSTANT, 'UTC')).toBe('31 Aug 00:52');
  });

  it('drops seconds and milliseconds', () => {
    const out = formatRolloverLocal(INSTANT, 'Australia/Melbourne');
    expect(out).not.toMatch(/28/); // the seconds
    expect(out).not.toMatch(/417/); // the milliseconds
    expect(out).not.toMatch(/Z|GMT|UTC/);
    expect(out).toMatch(/^\d{2} \w{3} \d{2}:\d{2}$/);
  });

  it('says nothing at all when the instant is unusable', () => {
    // Never the raw string (the bug) and never a substituted time: a wrong
    // rollover is worse than none, because the owner would plan around it.
    for (const bad of [null, undefined, '', 'not-a-date', '2026-13-45T99:99:99Z']) {
      expect(formatRolloverLocal(bad)).toBe('');
    }
    expect(formatRolloverLocal(INSTANT, 'Mars/Olympus_Mons')).toBe('');
  });

  it('omits the rollover clause rather than printing a raw ISO', () => {
    const groups = [
      {
        provider: 'claude',
        available: true,
        windows: [{ name: 'weekly', remaining_percent: 27, resets_at: 'not-a-date' }],
      },
    ] as unknown as TickerGroup[];
    const body = tickerTipBody(groups);
    expect(body).toMatch(/27% remaining/);
    expect(body).not.toMatch(/rollover/);
    expect(body).not.toMatch(/not-a-date/);
  });

  it('puts a readable local time in the tooltip when the instant is good', () => {
    const groups = [
      {
        provider: 'claude',
        available: true,
        windows: [{ name: 'weekly', remaining_percent: 27, resets_at: INSTANT }],
      },
    ] as unknown as TickerGroup[];
    const body = tickerTipBody(groups);
    expect(body).toMatch(/rollover \d{2} \w{3} \d{2}:\d{2}/);
    expect(body).not.toMatch(/T00:52:28|\.417/);
  });
});
