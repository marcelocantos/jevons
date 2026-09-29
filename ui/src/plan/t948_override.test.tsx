// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { PlanUsageBar } from '../components/PlanUsageBar';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

// 🎯T948: an overridden plan carries a ? inside its box whose label is the
// override's free-text reason; a plan without one has none.
it('marks an overridden plan with a ? that carries the reason', async () => {
  const reason = 'Owner has a Claude reset available; spend it before other plans.';
  const snap = {
    backends: [
      {
        provider: 'claude', status: 'available',
        override: { band: 'ok', reason },
        windows: [{ name: 'weekly', used_percent: 92, remaining_percent: 8, band: 'ok' }],
      },
      {
        provider: 'codex', status: 'available',
        windows: [{ name: 'weekly', used_percent: 40, remaining_percent: 60, band: 'ok' }],
      },
    ],
  };
  vi.stubGlobal('fetch', vi.fn(async (input: string) => {
    if (input === '/api/plan-usage') return { ok: true, json: async () => snap };
    if (input === '/api/plan-usage/decisions') return { ok: true, json: async () => [] };
    if (input === '/api/plan-usage/auth') return { ok: true, json: async () => ({ plans: [] }) };
    return { ok: true, json: async () => ({}) };
  }));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { container } = render(<QueryClientProvider client={client}><PlanUsageBar /></QueryClientProvider>);
  const mark = await screen.findByLabelText('Override: ' + reason);
  expect(mark.closest('[data-provider]')?.getAttribute('data-provider')).toBe('claude');
  expect(container.querySelectorAll('.plan-override')).toHaveLength(1);
});
