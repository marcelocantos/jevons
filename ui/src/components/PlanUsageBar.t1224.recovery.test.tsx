// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { MuxClient } from '../mux/client';
import { PLAN_USAGE_CHANNEL } from '../mux/protocol';
import { PlanUsageBar } from './PlanUsageBar';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('PlanUsageBar subscription recovery (T1224)', () => {
  it('recovers from unreadable to cached healthy mux frame without forced collection', async () => {
    const handlers = new Map<string, (env: { t: string; ch: string; body?: unknown }) => void>();
    const mux = {
      subscribe(ch: string, handler: (env: { t: string; ch: string; body?: unknown }) => void) {
        handlers.set(ch, handler);
        return () => handlers.delete(ch);
      },
      openChannel: vi.fn(),
      closeChannel: vi.fn(),
    } as unknown as MuxClient;
    const good = {
      backends: [
        { provider: 'claude', status: 'available', windows: [{ name: 'weekly', remaining_percent: 40 }] },
        { provider: 'grok', status: 'available', windows: [{ name: 'weekly', remaining_percent: 8 }] },
      ],
    };
    let refreshCalls = 0;
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo) => {
      const url = String(input);
      if (url.includes('/api/plan-usage?refresh=1')) {
        refreshCalls++;
        return { ok: true, json: async () => good };
      }
      return { ok: true, json: async () => ({ backends: [] }) };
    }));
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
    const { container } = render(createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux })));
    const send = handlers.get(PLAN_USAGE_CHANNEL)!;
    send({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          { provider: 'claude', status: 'unavailable', reason: 'meter 429', windows: [] },
          { provider: 'grok', status: 'unavailable', reason: 'meter 429', windows: [] },
        ],
      },
    });
    await waitFor(() => expect(container.textContent).toContain('usage unavailable'));
    send({ t: 'frame', ch: PLAN_USAGE_CHANNEL, body: good });
    await waitFor(() => expect(container.querySelector('[data-provider="grok"] .plan-bar-fill')).toBeTruthy());
    expect(refreshCalls).toBe(0);
  });

  it('tab visibility is read-only; a subsequent subscribed observation recovers without reload', async () => {
    const handlers = new Map<string, (env: { t: string; ch: string; body?: unknown }) => void>();
    const mux = {
      subscribe(ch: string, handler: (env: { t: string; ch: string; body?: unknown }) => void) {
        handlers.set(ch, handler);
        return () => handlers.delete(ch);
      },
      openChannel: vi.fn(),
      closeChannel: vi.fn(),
    } as unknown as MuxClient;
    const forced = {
      backends: [
        { provider: 'claude', status: 'available', windows: [{ name: 'weekly', remaining_percent: 33 }] },
        { provider: 'grok', status: 'available', windows: [{ name: 'weekly', remaining_percent: 6 }] },
      ],
    };
    let refreshCalls = 0;
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo) => {
      if (String(input).includes('/api/plan-usage?refresh=1')) {
        refreshCalls++;
        return { ok: true, json: async () => forced };
      }
      return { ok: true, json: async () => ({ backends: [] }) };
    }));
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
    const { container } = render(createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux })));
    const send = handlers.get(PLAN_USAGE_CHANNEL)!;
    send({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          { provider: 'claude', status: 'unavailable', reason: 'meter 429', windows: [] },
          { provider: 'grok', status: 'unavailable', reason: 'meter 429', windows: [] },
        ],
      },
    });
    await waitFor(() => expect(container.textContent).toContain('usage unavailable'));
    fireEvent(document, new Event('visibilitychange'));
    expect(refreshCalls).toBe(0);
    send({ t: 'frame', ch: PLAN_USAGE_CHANNEL, body: forced });
    await waitFor(() => {
      const claude = container.querySelector('[data-provider="claude"] .plan-bar-fill') as HTMLElement | null;
      expect(claude).toBeTruthy();
      expect(claude?.style.width).toBe('67%');
    });
  });
});

describe('PlanUsageBar stale HTTP fencing (T1224)', () => {
  it('does not let a late cached GET overwrite a newer mux frame, and aborts on unmount', async () => {
    const handlers = new Map<string, (env: { t: string; ch: string; body?: unknown }) => void>();
    const mux = {
      subscribe(ch: string, h: (env: { t: string; ch: string; body?: unknown }) => void) {
        handlers.set(ch, h);
        return () => handlers.delete(ch);
      }, openChannel: vi.fn(), closeChannel: vi.fn(),
    } as unknown as MuxClient;
    const old = { backends: [{ provider: 'claude', status: 'available', windows: [{ name: 'weekly', remaining_percent: 41 }] }] };
    const fresh = { backends: [{ provider: 'claude', status: 'available', windows: [{ name: 'weekly', remaining_percent: 33 }] }] };
    const pending: Array<{ resolve: (value: unknown) => void; signal?: AbortSignal }> = [];
    vi.stubGlobal('fetch', vi.fn((url: string, init?: { signal?: AbortSignal }) => {
      if (url === '/api/plan-usage' || url.includes('refresh=1')) return new Promise(resolve => pending.push({ resolve, signal: init?.signal }));
      return Promise.resolve({ ok: true, json: async () => ({ plans: [] }) });
    }));
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { container, unmount } = render(createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux })));
    handlers.get(PLAN_USAGE_CHANNEL)!({ t: 'reset', ch: PLAN_USAGE_CHANNEL });
    fireEvent(document, new Event('visibilitychange'));
    await waitFor(() => expect(pending.length).toBe(1));
    handlers.get(PLAN_USAGE_CHANNEL)!({ t: 'frame', ch: PLAN_USAGE_CHANNEL, body: fresh });
    await waitFor(() => expect((container.querySelector('.plan-bar-fill') as HTMLElement).style.width).toBe('67%'));
    for (const req of pending) req.resolve({ ok: true, json: async () => old });
    await new Promise(resolve => setTimeout(resolve, 0));
    expect((container.querySelector('.plan-bar-fill') as HTMLElement).style.width).toBe('67%');
    const beforeReset = pending.length;
    handlers.get(PLAN_USAGE_CHANNEL)!({ t: 'reset', ch: PLAN_USAGE_CHANNEL });
    await waitFor(() => expect(pending.length).toBeGreaterThan(beforeReset));
    const active = pending.slice(beforeReset);
    unmount();
    expect(active.every(req => req.signal?.aborted)).toBe(true);
  });
});

it('ages a silent observation using its SQL deadline without fetching', async () => {
  vi.useFakeTimers();
  const start = Date.now();
  let send: (env: unknown) => void = () => {};
  const mux = {
    subscribe(_ch: string, handler: (env: unknown) => void) { send = handler; return () => {}; },
    openChannel() {}, closeChannel() {},
  } as unknown as MuxClient;
  const fetcher = vi.fn(async () => ({ ok: true, json: async () => ({}) }));
  vi.stubGlobal('fetch', fetcher);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
  const { container, unmount } = render(createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux })));
  await act(async () => send({ t: 'frame', body: { backends: [{
    provider: 'claude', status: 'available', success_at: new Date(start).toISOString(),
    stale_at: new Date(start + 30_000).toISOString(),
    windows: [{ name: 'weekly', remaining_percent: 40 }],
  }] } }));
  expect(container.querySelector('.plan-stale')).toBeNull();
  await act(async () => { await vi.advanceTimersByTimeAsync(60_000); });
  expect(container.querySelector('[data-provider="claude"].plan-stale')).toBeTruthy();
  expect(container.textContent).toContain('last known');
  expect((container.querySelector('.plan-bar-fill') as HTMLElement).style.width).toBe('60%');
  expect(fetcher.mock.calls).toEqual([]);
  unmount();
});
