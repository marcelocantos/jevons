// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement, type ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setNow, reset as resetClock } from '../clock';
import { PlanUsageBar } from './PlanUsageBar';
import { companyOfProvider } from '../plan/companyMark';
import { triangleColorForRemaining } from '../plan/triColor';
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
      expect(tri.style.borderBottomColor).toBe(triangleColorForRemaining(25));
      // 🎯T670: the chevron marks time SPENT and travels rightward with the
      // fill — a quarter of this window is left, so three quarters are gone.
      // Only the colour was pinned before, so the flip was invisible here.
      expect(tri.style.left).toBe('75%');
      const fill = container.querySelector('.plan-bar-fill') as HTMLElement;
      expect(fill.style.width).toBe('90%');
    } finally {
      resetClock();
    }
  });
});
