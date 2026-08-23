// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { showOnBar, tickerGroups, tickerTitle } from './tickerGroups';

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

  it('treats Claude 429 as exhausted bars, not a missing group', () => {
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
    expect(groups[0]?.available).toBe(true);
    expect(groups[0]?.windows.map((w) => w.remaining_percent)).toEqual([0, 0]);
  });
});
