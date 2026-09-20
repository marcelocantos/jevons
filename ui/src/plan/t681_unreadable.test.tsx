// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement, type ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { setNow, reset as resetClock } from '../clock';
import { PlanUsageBar } from '../components/PlanUsageBar';
import { PlanTipTable } from './tipTable';
import type { MuxClient } from '../mux/client';
import { PLAN_USAGE_CHANNEL } from '../mux/protocol';
import type { TickerGroup } from './tickerGroups';

// 🎯T681: a provider whose meter could not be read is not a provider with
// nothing left. The owner saw both painted the same way while Claude's
// usage endpoint was rate-limited, and could not tell which had happened.

const NOW = Date.parse('2026-09-20T12:00:00Z');

function muxHarness() {
  const handlers = new Map<string, (env: { t: string; ch: string; body: unknown }) => void>();
  const mux = {
    subscribe(ch: string, handler: (env: { t: string; ch: string; body: unknown }) => void) {
      handlers.set(ch, handler);
      return () => handlers.delete(ch);
    },
    openChannel: vi.fn(),
    closeChannel: vi.fn(),
  } as unknown as MuxClient;
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
  const tree: ReactNode = createElement(
    QueryClientProvider,
    { client: qc },
    createElement(PlanUsageBar, { mux }),
  );
  return { handlers, ...render(tree) };
}

describe('an unreadable provider paints as unreadable (🎯T681)', () => {
  beforeEach(() => {
    setNow(NOW);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }));
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    resetClock();
  });

  it('gives a rate-limited meter a no-data track, not an empty usage bar', async () => {
    const { handlers, container } = muxHarness();
    handlers.get(PLAN_USAGE_CHANNEL)!({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          {
            provider: 'claude',
            status: 'unavailable',
            reason: 'Claude usage HTTP 429: rate_limit_error',
            windows: [],
          },
          {
            provider: 'grok',
            status: 'available',
            windows: [{ name: 'weekly', remaining_percent: 40, used_percent: 60 }],
          },
        ],
      },
    });
    const claude = await waitFor(() => {
      const el = container.querySelector('[data-provider="claude"]');
      expect(el).toBeTruthy();
      return el!;
    });
    expect(claude.className).toContain('plan-unavail');
    // The distinct state: a hatched track carrying no fill at all.
    expect(claude.querySelector('.plan-nodata')).toBeTruthy();
    expect(claude.querySelector('.plan-bar-fill')).toBeNull();
    // And it is not the empty bar a spent window gets.
    expect(claude.querySelector('.plan-exhausted')).toBeNull();
    // A provider that did answer still paints a real usage bar.
    const grok = container.querySelector('[data-provider="grok"]')!;
    expect(grok.querySelector('.plan-nodata')).toBeNull();
    expect(grok.querySelector<HTMLElement>('.plan-bar-fill')!.style.width).toBe('60%');
  });
});

describe('the tip says what was last known and how old it is (🎯T681)', () => {
  it('names the age and the last figures rather than a fresh-looking zero', () => {
    const groups = [
      {
        provider: 'claude',
        available: false,
        reason: 'Claude usage HTTP 429: rate_limit_error',
        windows: [],
        last: {
          at: NOW - 12 * 60_000,
          windows: [{ name: 'session', remaining_percent: 87, used_percent: 13 }],
        },
      },
    ] as unknown as TickerGroup[];
    const { container } = render(<PlanTipTable groups={groups} nowMs={NOW} timeZone="UTC" />);
    const text = container.textContent || '';
    expect(text).toContain('no reading');
    expect(text).toContain('429');
    expect(text).toContain('12m ago');
    expect(text).toContain('session 13%');
    // Never a zero standing in for a number nobody has.
    expect(text).not.toMatch(/\b0%/);
  });
});
