// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { holdGroupReadings, showOnBar, tickerGroups, tickerTitle } from './tickerGroups';

describe('tickerGroups', () => {
  it('keeps unavailable Cursor on the bar (mark only)', () => {
    const groups = tickerGroups({
      backends: [
        {
          provider: 'claude',
          status: 'available',
          windows: [{ name: 'weekly', remaining_percent: 27 }],
        },
        {
          provider: 'cursor',
          status: 'unavailable',
          reason: 'cursor usage: read cursor auth db: exit status 14',
        },
        { provider: 'codex', status: 'available', windows: [{ name: 'weekly', remaining_percent: 25 }] },
        { provider: 'grok', status: 'available', windows: [{ name: 'weekly', remaining_percent: 6 }] },
      ],
    });
    expect(groups.map((g) => g.provider)).toEqual(['claude', 'codex', 'grok', 'cursor']);
    const cursor = groups.find((g) => g.provider === 'cursor');
    expect(cursor?.available).toBe(false);
    expect(cursor?.windows).toEqual([]);
    expect(tickerTitle(groups)).toContain('cursor: unavailable — cursor usage: read cursor auth db');
  });

  it('hides idle Bedrock and keeps a running Bedrock mark', () => {
    expect(
      showOnBar({ provider: 'bedrock', available: false, running: false }),
    ).toBe(false);
    const hidden = tickerGroups({
      backends: [
        { provider: 'bedrock', status: 'unavailable', reason: 'AWS publishes no remaining', fleet_agents: 0 },
        { provider: 'claude', status: 'available', windows: [{ name: 'weekly', remaining_percent: 10 }] },
      ],
    });
    expect(hidden.map((g) => g.provider)).toEqual(['claude']);

    const running = tickerGroups({
      backends: [
        { provider: 'bedrock', status: 'unavailable', reason: 'AWS publishes no remaining', fleet_agents: 2 },
      ],
    });
    expect(running).toEqual([
      { provider: 'bedrock', available: false, stale: undefined, reason: 'AWS publishes no remaining', windows: [] },
    ]);
  });

  it('paints a rate-limited usage endpoint as unreadable, never as spent (\u{1F3AF}T681)', () => {
    // The 429 is the meter refusing us, not the plan running out. Reading
    // it as exhaustion painted a Claude with most of its session left as
    // fully spent, and parked a live worker on the daemon side (T677).
    const groups = tickerGroups({
      backends: [
        {
          provider: 'claude',
          status: 'unavailable',
          reason: 'Claude usage HTTP 429: rate_limit_error',
          windows: [],
        },
      ],
    });
    expect(groups[0]?.available).toBe(false);
    expect(groups[0]?.windows).toEqual([]);
    expect(groups[0]?.reason).toContain('429');
  });

  it('carries the last real reading forward while a provider is unreadable (\u{1F3AF}T681)', () => {
    const good = tickerGroups({
      backends: [
        {
          provider: 'claude',
          status: 'available',
          windows: [{ name: 'session', remaining_percent: 87, used_percent: 13 }],
        },
      ],
    });
    const first = holdGroupReadings(new Map(), good, 1_000);
    expect(first.groups[0]?.last).toBeUndefined();

    const broken = tickerGroups({
      backends: [
        { provider: 'claude', status: 'unavailable', reason: 'HTTP 429', windows: [] },
      ],
    });
    const second = holdGroupReadings(first.last, broken, 61_000);
    expect(second.groups[0]?.available).toBe(false);
    expect(second.groups[0]?.last?.at).toBe(1_000);
    expect(second.groups[0]?.last?.windows[0]?.used_percent).toBe(13);
  });

});
