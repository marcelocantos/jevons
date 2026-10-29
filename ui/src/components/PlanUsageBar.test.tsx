// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement, type ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { setNow, reset as resetClock } from '../clock';
import { PlanUsageBar } from './PlanUsageBar';
import { companyOfProvider } from '../plan/companyMark';
import { WEEKLY_LIMIT_SECONDS } from '../plan/windowGeom';
import type { MuxClient } from '../mux/client';
import { PLAN_USAGE_CHANNEL } from '../mux/protocol';


describe.sequential('PlanUsageBar mux wiring', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }));
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('opening and revisiting the tab never collect usage', () => {
    const mux = {
      subscribe() { return () => {}; },
      openChannel: vi.fn(), closeChannel: vi.fn(),
    } as unknown as MuxClient;
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
    render(createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux })));
    fireEvent(document, new Event('visibilitychange'));
    fireEvent(window, new Event('focus'));
    expect(mux.openChannel).toHaveBeenCalledWith(PLAN_USAGE_CHANNEL);
    expect(vi.mocked(globalThis.fetch).mock.calls.filter(([url]) => String(url).startsWith('/api/plan-usage?'))).toEqual([]);
  });

  it('shows usage unavailable when mux reports explicit unreadable (no stale green)', async () => {
    const handlers = new Map<string, (env: { t: string; ch: string; body?: unknown }) => void>();
    const mux = {
      subscribe(ch: string, handler: (env: { t: string; ch: string; body?: unknown }) => void) {
        handlers.set(ch, handler);
        return () => handlers.delete(ch);
      },
      openChannel: vi.fn(),
      closeChannel: vi.fn(),
    } as unknown as MuxClient;
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
    const { container } = render(createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux })));
    const send = handlers.get(PLAN_USAGE_CHANNEL)!;
    send({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          { provider: 'claude', status: 'available', windows: [{ name: 'weekly', remaining_percent: 40 }] },
          { provider: 'grok', status: 'available', windows: [{ name: 'weekly', remaining_percent: 8 }] },
        ],
      },
    });
    await waitFor(() => expect(container.querySelector('[data-provider="grok"] .plan-bar-fill')).toBeTruthy());
    send({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          { provider: 'claude', status: 'unavailable', reason: 'HTTP 429', windows: [] },
          { provider: 'grok', status: 'unavailable', reason: 'HTTP 429', windows: [] },
        ],
      },
    });
    await waitFor(() => expect(container.querySelector('[data-provider="claude"] .plan-nodata')).toBeTruthy());
    expect(container.textContent).toContain('usage unavailable');
    expect(container.querySelector('[data-provider="claude"] .plan-bar-fill')).toBeNull();
  });

  it('re-hydrates from GET /api/plan-usage on mux reconnect reset', async () => {
    const handlers = new Map<string, (env: { t: string; ch: string; body?: unknown }) => void>();
    const mux = {
      subscribe(ch: string, handler: (env: { t: string; ch: string; body?: unknown }) => void) {
        handlers.set(ch, handler);
        return () => handlers.delete(ch);
      },
      openChannel: vi.fn(),
      closeChannel: vi.fn(),
    } as unknown as MuxClient;
    vi.mocked(globalThis.fetch).mockImplementation(async (input) => {
      if (input === '/api/plan-usage') {
        return {
          ok: true,
          json: async () => ({
            backends: [{ provider: 'grok', status: 'available', windows: [{ name: 'weekly', remaining_percent: 21 }] }],
          }),
        };
      }
      if (input === '/api/plan-usage?refresh=1') {
        return { ok: true, json: async () => ({ backends: [] }) };
      }
      return { ok: true, json: async () => ({}) };
    });
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
    const { container } = render(createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux })));
    handlers.get(PLAN_USAGE_CHANNEL)!({ t: 'reset', ch: PLAN_USAGE_CHANNEL });
    await waitFor(() => expect(container.querySelector('[data-provider="grok"] .plan-bar-fill')).toBeTruthy());
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/plan-usage', expect.anything());
  });

  it('maps cursor through the shared company mark', () => {
    expect(companyOfProvider('cursor')).toBe('cursor');
  });

  it('paints a mux frame without HTTP polling', async () => {
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
    const tree: ReactNode = createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux }));
    const { container } = render(tree);
    expect(mux.openChannel).toHaveBeenCalledWith(PLAN_USAGE_CHANNEL);
    handlers.get(PLAN_USAGE_CHANNEL)!({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          {
            provider: 'claude',
            status: 'available',
            windows: [{ name: 'weekly', remaining_percent: 36 }],
          },
        ],
      },
    });
    await waitFor(() => expect(container.querySelector('[data-provider="claude"]')).toBeTruthy());
  });

  it('colours the triangle from remaining period, not bar pace', async () => {
    const now = Date.parse('2026-01-01T00:00:00Z');
    setNow(now);
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
    const tree: ReactNode = createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux }));
    const { container } = render(tree);
    handlers.get(PLAN_USAGE_CHANNEL)!({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          {
            provider: 'claude',
            status: 'available',
            windows: [
              {
                name: 'weekly',
                remaining_percent: 10,
                band: 'hot',
                resets_at: new Date(now + WEEKLY_LIMIT_SECONDS * 1000 * 0.25).toISOString(),
              },
            ],
          },
        ],
      },
    });
    try {
      await waitFor(() => expect(container.querySelector('.plan-tri')).toBeTruthy());
      const tri = container.querySelector('.plan-tri') as HTMLElement;
      const win = container.querySelector('.plan-win') as HTMLElement;
      expect(win.className).toContain('plan-hot');
      // 🎯T673: no inline colour — the chevron is grey from the stylesheet,
      // never the bar's colour and never a ramp of its own.
      expect(tri.style.borderBottomColor).toBe('');
      // 🎯T670: the chevron marks time SPENT and travels rightward with the
      // fill — a quarter of this window is left, so three quarters are gone.
      // Only the colour was pinned before, so the flip was invisible here.
      expect(tri.style.left).toBe('75%');
      const fill = container.querySelector('.plan-bar-fill') as HTMLElement;
      expect(fill.style.width).toBe('90%');
      expect(fill.style.background).toBe('');
    } finally {
      resetClock();
    }
  });

  it('paints served leftover blue (under) and purple (locked); session stays off waste (🎯T390.1.1 / 🎯T1072)', async () => {
    const now = Date.parse('2026-09-12T12:00:00Z');
    setNow(now);
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
    const tree: ReactNode = createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux }));
    const { container } = render(tree);
    handlers.get(PLAN_USAGE_CHANNEL)!({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          {
            provider: 'claude',
            status: 'available',
            windows: [
              {
                name: 'session',
                remaining_percent: 86,
                used_percent: 14,
                resets_at: new Date(now + 97 * 60 * 1000).toISOString(),
                limit_window_seconds: 5 * 3600,
                band: 'ok', // the daemon never serves leftover on a session
              },
              {
                name: 'weekly',
                remaining_percent: 58,
                used_percent: 42,
                resets_at: new Date(now + 3 * 24 * 3600 * 1000).toISOString(),
                limit_window_seconds: WEEKLY_LIMIT_SECONDS,
                band: 'under',
              },
            ],
          },
          {
            provider: 'codex',
            status: 'available',
            windows: [
              {
                name: 'weekly',
                remaining_percent: 100,
                used_percent: 0,
                resets_at: new Date(now + 3600 * 1000).toISOString(),
                limit_window_seconds: WEEKLY_LIMIT_SECONDS,
                band: 'locked',
              },
            ],
          },
          {
            provider: 'cursor',
            status: 'available',
            windows: [
              {
                name: 'monthly',
                remaining_percent: 36,
                used_percent: 64,
                resets_at: new Date(now + 0.095 * 30 * 24 * 3600 * 1000).toISOString(),
                limit_window_seconds: 30 * 24 * 3600,
                band: 'locked',
              },
            ],
          },
        ],
      },
    });
    try {
      await waitFor(() => expect(container.querySelector('[data-provider="codex"]')).toBeTruthy());
      const session = container.querySelector('[data-provider="claude"] [data-window="session"]') as HTMLElement;
      const claudeWeek = container.querySelector('[data-provider="claude"] [data-window="weekly"]') as HTMLElement;
      const codexWeek = container.querySelector('[data-provider="codex"] [data-window="weekly"]') as HTMLElement;
      const cursorMonth = container.querySelector('[data-provider="cursor"] [data-window="monthly"]') as HTMLElement;
      expect(session.className).not.toMatch(/plan-under|plan-locked/);
      expect(claudeWeek.className).toContain('plan-under');
      expect(codexWeek.className).toContain('plan-locked');
      expect(cursorMonth.className).toContain('plan-locked');
      fireEvent.pointerEnter(container.querySelector('[data-instant-tip-host]')!);
      const tip = container.querySelector('.instant-tip-show')?.textContent || '';
      expect(tip).not.toMatch(/continuation leftover|already-unrecoverable/);
    } finally {
      resetClock();
    }
  });

  it('paints the bar from the same pace class as the popup graph', async () => {
    const now = Date.parse('2026-09-12T12:00:00Z');
    setNow(now);
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
    const tree: ReactNode = createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux }));
    const { container } = render(tree);
    handlers.get(PLAN_USAGE_CHANNEL)!({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          {
            provider: 'claude',
            status: 'available',
            windows: [
              {
                name: 'session',
                remaining_percent: 29.375,
                used_percent: 70.625,
                resets_at: new Date(now + 2.5 * 3600 * 1000).toISOString(),
                limit_window_seconds: 5 * 3600,
                band: 'ahead',
              },
              {
                name: 'weekly',
                remaining_percent: 53.75,
                used_percent: 46.25,
                resets_at: new Date(now + 0.5 * WEEKLY_LIMIT_SECONDS * 1000).toISOString(),
                limit_window_seconds: WEEKLY_LIMIT_SECONDS,
                band: 'ok',
              },
            ],
          },
        ],
      },
    });
    try {
      await waitFor(() => expect(container.querySelector('[data-window="weekly"]')).toBeTruthy());
      const sessionFill = container.querySelector(
        '[data-window="session"] .plan-bar-fill',
      ) as HTMLElement;
      const weekFill = container.querySelector(
        '[data-window="weekly"] .plan-bar-fill',
      ) as HTMLElement;
      const session = container.querySelector('[data-window="session"]') as HTMLElement;
      const week = container.querySelector('[data-window="weekly"]') as HTMLElement;
      // The server's band is the colour. No inline blend on top of it.
      expect(sessionFill.style.background).toBe('');
      expect(weekFill.style.background).toBe('');
      expect(session.className).not.toBe(week.className);
    } finally {
      resetClock();
    }
  });

  // 🎯T967.1: a dollar figure appears in bold red only while the backend
  // is actually spending extra usage, and is absent otherwise.
  it('shows the spend figure in bold red only while spending', async () => {
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
    const tree: ReactNode = createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux }));
    const { container } = render(tree);
    handlers.get(PLAN_USAGE_CHANNEL)!({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          {
            provider: 'claude',
            status: 'available',
            windows: [{ name: 'session', remaining_percent: 0, used_percent: 100, band: 'hot' }],
            spend: { used_aud: 72.84, limit_aud: 100 },
          },
        ],
      },
    });
    await waitFor(() => expect(container.querySelector('.plan-spend')).toBeTruthy());
    const spend = container.querySelector('.plan-spend') as HTMLElement;
    expect(spend.textContent).toBe('A$72.84 / A$100.00');
  });

  it('shows no dollar figure when the backend is not spending', async () => {
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
    const tree: ReactNode = createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux }));
    const { container } = render(tree);
    handlers.get(PLAN_USAGE_CHANNEL)!({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          {
            provider: 'claude',
            status: 'available',
            windows: [{ name: 'session', remaining_percent: 44, used_percent: 56, band: 'ok' }],
          },
        ],
      },
    });
    await waitFor(() => expect(container.querySelector('[data-provider="claude"]')).toBeTruthy());
    expect(container.querySelector('.plan-spend')).toBeFalsy();
  });

  // 🎯T967.4: Cursor never paints a dollar figure. Live DashboardService
  // carries unitless spend counters without currency or on-demand
  // enablement; the served payload therefore has no spend, and the bar
  // must stay silent even when the monthly window is full.
  it('shows no Cursor dollar figure at a full monthly window (T967.4 silence)', async () => {
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
    const tree: ReactNode = createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux }));
    const { container } = render(tree);
    handlers.get(PLAN_USAGE_CHANNEL)!({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          {
            provider: 'cursor',
            status: 'available',
            windows: [{ name: 'monthly', remaining_percent: 0, used_percent: 100, band: 'exhausted' }],
            // no spend — T967.4 residue: unit/enablement not established
          },
        ],
      },
    });
    await waitFor(() => expect(container.querySelector('[data-provider="cursor"]')).toBeTruthy());
    expect(container.querySelector('.plan-spend')).toBeFalsy();
    expect(container.textContent || '').not.toMatch(/A\$/);
  });

  it('spend figure survives an override that paints the bar ok (override-proof)', async () => {
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
    const tree: ReactNode = createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux }));
    const { container } = render(tree);
    handlers.get(PLAN_USAGE_CHANNEL)!({
      t: 'frame',
      ch: PLAN_USAGE_CHANNEL,
      body: {
        backends: [
          {
            provider: 'claude',
            status: 'available',
            windows: [{ name: 'session', remaining_percent: 0, used_percent: 100, band: 'ok' }],
            spend: { used_aud: 72.84, limit_aud: 100 },
            override: { band: 'ok', reason: 'owner override' },
          },
        ],
      },
    });
    await waitFor(() => expect(container.querySelector('.plan-spend')).toBeTruthy());
    const win = container.querySelector('.plan-win') as HTMLElement;
    expect(win.className).not.toContain('plan-hot');
    const spend = container.querySelector('.plan-spend') as HTMLElement;
    expect(spend.textContent).toBe('A$72.84 / A$100.00');
  });

  it('renders an unavailable reason and a distinct limit-reached state on a full Claude window', async () => {
    const handlers = new Map<string, (env: { t: string; ch: string; body: unknown }) => void>();
    const mux = {
      subscribe(ch: string, handler: (env: { t: string; ch: string; body: unknown }) => void) {
        handlers.set(ch, handler);
        return () => handlers.delete(ch);
      },
      openChannel: vi.fn(), closeChannel: vi.fn(),
    } as unknown as MuxClient;
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
    const { container } = render(createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux })));
    const frame = (spend: unknown) => handlers.get(PLAN_USAGE_CHANNEL)!({
      t: 'frame', ch: PLAN_USAGE_CHANNEL,
      body: { backends: [{ provider: 'claude', status: 'available',
        windows: [{ name: 'session', remaining_percent: 0, used_percent: 100, band: 'ok' }],
        override: { band: 'ok', reason: 'owner override' }, spend }] },
    });
    frame({ unavailable_reason: 'spend block missing from Claude usage reading' });
    await waitFor(() => expect(container.querySelector('.plan-spend')?.textContent).toContain('spend unavailable'));
    expect(container.querySelector('.plan-spend')?.textContent).toContain('spend block missing');
    expect(container.querySelector('.plan-spend')?.textContent).not.toContain('A$');
    frame({ used_aud: 72.84, limit_aud: 100, limit_reached: true });
    await waitFor(() => expect(container.querySelector('.plan-spend')?.textContent).toContain('limit reached'));
    expect(container.querySelector('.plan-spend')?.textContent).toContain('A$72.84 / A$100.00');
  });

});
