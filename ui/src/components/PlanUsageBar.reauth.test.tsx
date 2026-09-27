// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { PlanUsageBar } from './PlanUsageBar';

afterEach(() => vi.unstubAllGlobals());

it('shows Claudia destination failures for running seats and lets the owner retry canceled reauth', async () => {
  let recoveries = 0;
  const decisions = [
    { Name: 'jevons-po', From: 'grok', To: 'claude', Execution: 'failed', Failure: 'invalid_grant: refresh token expired', ReauthAvailable: true },
    { Name: 'ge-po', From: 'grok', To: 'claude', Execution: 'failed', Failure: 'invalid_grant: refresh token expired', ReauthAvailable: true },
    { Name: 'healthy', From: 'grok', To: 'codex', Execution: 'migrated', Failure: '', ReauthAvailable: false },
  ];
  vi.stubGlobal('fetch', vi.fn(async (input: string, init?: RequestInit) => {
    if (input === '/api/plan-usage/decisions') return { ok: true, json: async () => decisions };
    if (input === '/api/plan-usage/auth/recover/claude' && init?.method === 'POST') {
      recoveries++;
      return recoveries === 1
        ? { ok: false, json: async () => ({ error: 'Sign-in was canceled' }) }
        : { ok: true, json: async () => ({ status: 'recovered' }) };
    }
    return { ok: true, json: async () => ({ backends: [] }) };
  }));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { container } = render(<QueryClientProvider client={client}><PlanUsageBar /></QueryClientProvider>);
  const button = await screen.findByRole('button', { name: 'Reauth claude for failed migration' });
  expect(screen.getAllByRole('button', { name: 'Reauth claude for failed migration' })).toHaveLength(1);
  expect(screen.queryByRole('button', { name: /Reauth codex/ })).toBeNull();
  fireEvent.pointerEnter(container.querySelector('[data-instant-tip-host]')!);
  expect(container.querySelector('.plan-migration-failures')?.textContent).toContain('jevons-po: grok → claude — Destination refresh token was rejected (invalid_grant)');
  expect(container.querySelector('.plan-migration-failures')?.textContent).toContain('ge-po: grok → claude — Destination refresh token was rejected (invalid_grant)');

  fireEvent.click(button);
  await waitFor(() => expect(screen.getByRole('status').textContent).toContain('Sign-in was canceled'));
  expect(button.hasAttribute('disabled')).toBe(false);
  fireEvent.click(button);
  await waitFor(() => expect(screen.getByRole('status').textContent).toContain('migration will retry automatically'));
  expect(recoveries).toBe(2);
});
