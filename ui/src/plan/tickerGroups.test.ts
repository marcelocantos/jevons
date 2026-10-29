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

  it('uses the durable last success even on first load during an outage', () => {
    const snapshot = {
      backends: [{
        provider: 'claude', status: 'unavailable', reason: 'HTTP 429',
        success_at: '2026-10-25T01:00:00Z', attempted_at: '2026-10-25T01:10:00Z',
        next_attempt_at: '2026-10-25T01:15:00Z', stale: true,
        windows: [{ name: 'session', remaining_percent: 87, used_percent: 13 }],
      }],
    };
    const group = tickerGroups(snapshot)[0]!;
    expect(group.available).toBe(false);
    expect(group.last?.at).toBe(Date.parse('2026-10-25T01:00:00Z'));
    expect(group.windows[0]?.used_percent).toBe(13);
    expect(tickerGroups(snapshot)[0]?.last?.at).toBe(group.last?.at);
  });
});
