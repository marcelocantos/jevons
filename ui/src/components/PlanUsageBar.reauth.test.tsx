// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { PlanUsageBar } from './PlanUsageBar';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

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

// 🎯T924: a plan whose login is missing or rejected gets Reauth with no
// failed migration and no broken seat; a healthy plan gets none, and the
// button goes once the login is good.
it('offers Reauth for any plan whose login needs the owner', async () => {
  let cursorState = 'missing';
  const recovered: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (input: string, init?: RequestInit) => {
    if (input === '/api/plan-usage/decisions') return { ok: true, json: async () => [] };
    if (input === '/api/plan-usage/auth') {
      return { ok: true, json: async () => ({ plans: [
        { provider: 'anthropic', state: 'ok' },
        { provider: 'cursor', state: cursorState },
        { provider: 'xai-oauth', state: 'rejected', detail: 'invalid_grant' },
      ] }) };
    }
    if (input.startsWith('/api/plan-usage/auth/recover/') && init?.method === 'POST') {
      recovered.push(input.slice('/api/plan-usage/auth/recover/'.length));
      cursorState = 'ok';
      return { ok: true, json: async () => ({ status: 'recovered' }) };
    }
    return { ok: true, json: async () => ({ backends: [] }) };
  }));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { container } = render(<QueryClientProvider client={client}><PlanUsageBar /></QueryClientProvider>);
  const cursor = await screen.findByRole('button', { name: 'Reauth cursor — no saved login' });
  expect(screen.getByRole('button', { name: 'Reauth grok — login rejected (invalid_grant)' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: /Reauth claude/ })).toBeNull();
  expect(recovered).toEqual([]);
  fireEvent.pointerEnter(container.querySelector('[data-instant-tip-host]')!);
  expect(container.querySelector('.plan-login-health')?.textContent).toContain('cursor: no saved login');

  fireEvent.click(cursor);
  await waitFor(() => expect(screen.getByRole('status').textContent).toContain('Claudia recovered the cursor login.'));
  expect(recovered).toEqual(['cursor']);
  await waitFor(() => expect(screen.queryByRole('button', { name: /Reauth cursor/ })).toBeNull());
  expect(screen.getByRole('button', { name: /Reauth grok/ })).toBeTruthy();
});
