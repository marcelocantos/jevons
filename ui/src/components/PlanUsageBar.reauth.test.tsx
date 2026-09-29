// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { PlanUsageBar, type RefusedSeat } from './PlanUsageBar';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderBar(refusedSeats?: RefusedSeat[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <PlanUsageBar refusedSeats={refusedSeats} />
    </QueryClientProvider>,
  );
}

function menuItems(): string[] {
  return screen.queryAllByRole('menuitem').map((b) => b.getAttribute('aria-label') || '');
}

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
  const { container } = renderBar();
  const item = await screen.findByRole('menuitem', { name: 'Reauth claude: migration failed for jevons-po, ge-po' });
  expect(menuItems()).toEqual(['Reauth claude: migration failed for jevons-po, ge-po']);
  fireEvent.pointerEnter(container.querySelector('[data-instant-tip-host]')!);
  expect(container.querySelector('.plan-migration-failures')?.textContent).toContain('jevons-po: grok → claude — Destination refresh token was rejected (invalid_grant)');

  fireEvent.click(item);
  await waitFor(() => expect(screen.getByRole('status').textContent).toContain('Sign-in was canceled'));
  expect(item.hasAttribute('disabled')).toBe(false);
  fireEvent.click(item);
  await waitFor(() => expect(screen.getByRole('status').textContent).toContain('migration will retry automatically'));
  expect(recoveries).toBe(2);
});

// 🎯T924: a plan whose login is missing or rejected is listed with no failed
// migration and no broken seat; a healthy plan is not, and an entry goes once
// the login is good.
it('lists every plan whose login needs the owner, one entry each', async () => {
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
  renderBar();
  const cursor = await screen.findByRole('menuitem', { name: 'Reauth cursor: no saved login' });
  expect(menuItems()).toEqual(['Reauth cursor: no saved login', 'Reauth grok: login rejected (invalid_grant)']);
  expect(screen.getByRole('button', { name: 'Sign in (2)' })).toBeTruthy();
  expect(recovered).toEqual([]);

  fireEvent.click(cursor);
  await waitFor(() => expect(recovered).toEqual(['cursor']));
  await waitFor(() => expect(menuItems()).toEqual(['Reauth grok: login rejected (invalid_grant)']));
  expect(screen.getByRole('button', { name: 'Sign in (1)' })).toBeTruthy();
});

// 🎯T945: the menu exists only while some backend is known to be signed out.
it('is absent when every known login is healthy', async () => {
  const seen: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (input: string) => {
    seen.push(input);
    if (input === '/api/plan-usage/decisions') return { ok: true, json: async () => [] };
    if (input === '/api/plan-usage/auth') return { ok: true, json: async () => ({ plans: [{ provider: 'anthropic', state: 'ok' }] }) };
    return { ok: true, json: async () => ({ backends: [] }) };
  }));
  const { container } = renderBar([]);
  await waitFor(() => expect(seen).toContain('/api/plan-usage/auth'));
  expect(container.querySelector('#plan-reauth-menu')).toBeNull();
  expect(screen.queryByRole('button', { name: /Sign in/ })).toBeNull();
});

// A seat refused on a login the broker still reports healthy is one entry
// for its plan, however many seats share it, and signs in through a seat.
it('folds refused seats into one entry per plan', async () => {
  const posts: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (input: string, init?: RequestInit) => {
    if (init?.method === 'POST') {
      posts.push(input);
      return { ok: true, json: async () => ({ status: 'running' }) };
    }
    if (input === '/api/plan-usage/decisions') return { ok: true, json: async () => [] };
    if (input === '/api/plan-usage/auth') return { ok: true, json: async () => ({ plans: [{ provider: 'anthropic', state: 'ok' }] }) };
    return { ok: true, json: async () => ({ backends: [] }) };
  }));
  renderBar([
    { name: 'claudia-po', provider: 'anthropic' },
    { name: 'ge-po', provider: 'anthropic' },
  ]);
  const item = await screen.findByRole('menuitem', { name: 'Reauth claude: login refused for claudia-po, ge-po' });
  expect(menuItems()).toHaveLength(1);
  fireEvent.click(item);
  await waitFor(() => expect(posts).toEqual(['/api/agents/claudia-po/auth/recover']));
});
