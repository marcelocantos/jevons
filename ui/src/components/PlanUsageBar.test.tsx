// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement, type ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setNow, reset as resetClock } from '../clock';
import { PlanUsageBar } from './PlanUsageBar';
import { companyOfProvider } from '../plan/companyMark';
import { WEEKLY_LIMIT_SECONDS } from '../plan/windowGeom';
import type { MuxClient } from '../mux/client';
import { PLAN_USAGE_CHANNEL } from '../mux/protocol';

const here = dirname(fileURLToPath(import.meta.url));

describe('PlanUsageBar mux wiring', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }));
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('reloads kick GET /api/plan-usage?refresh=1', async () => {
    const mux = {
      subscribe() { return () => {}; },
      openChannel: vi.fn(),
      closeChannel: vi.fn(),
    } as unknown as MuxClient;
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
    render(createElement(QueryClientProvider, { client: qc }, createElement(PlanUsageBar, { mux })));
    await waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/plan-usage?refresh=1', expect.anything());
    });
  });

  it('subscribes to plan-usage mux and does not 60s-poll when mux is connected', () => {
    const src = readFileSync(join(here, 'PlanUsageBar.tsx'), 'utf8');
    expect(src).toContain('PLAN_USAGE_CHANNEL');
    expect(src).toContain('openChannel');
    expect(src).toContain('enabled: !props.mux');
    expect(src).toContain('if (props.mux) return false');
    expect(src).toContain("fetch('/api/plan-usage', { signal })");
    expect(src).toContain("fetch('/api/plan-usage?refresh=1'");
    expect(src).toContain('CompanyMark');
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
      expect(fill.style.background).toMatch(/^rgb\(/);
    } finally {
      resetClock();
    }
  });

  it('paints weekly continuation blue and locked surplus purple; session stays off waste (🎯T390.1.1)', async () => {
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
              },
              {
                name: 'weekly',
                remaining_percent: 58,
                used_percent: 42,
                resets_at: new Date(now + 3 * 24 * 3600 * 1000).toISOString(),
                limit_window_seconds: WEEKLY_LIMIT_SECONDS,
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
      expect(tip).toMatch(/continuation leftover/);
      expect(tip).toMatch(/already-unrecoverable at 1\.5×/);
    } finally {
      resetClock();
    }
  });

  it('paints mid-ahead and mid-under fills from paceColor, not a class snap (🎯T390.1.2)', async () => {
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
              },
              {
                name: 'weekly',
                remaining_percent: 53.75,
                used_percent: 46.25,
                resets_at: new Date(now + 0.5 * WEEKLY_LIMIT_SECONDS * 1000).toISOString(),
                limit_window_seconds: WEEKLY_LIMIT_SECONDS,
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
      expect(sessionFill.style.background).toMatch(/^rgb\(/);
      expect(weekFill.style.background).toMatch(/^rgb\(/);
      expect(sessionFill.style.background).not.toBe(weekFill.style.background);
      // Named class snaps: dark-theme amber / red / green / under-blue.
      for (const named of ['rgb(251, 191, 36)', 'rgb(239, 68, 68)', 'rgb(74, 222, 128)', 'rgb(96, 165, 250)']) {
        expect(sessionFill.style.background).not.toBe(named);
        expect(weekFill.style.background).not.toBe(named);
      }
    } finally {
      resetClock();
    }
  });
});
