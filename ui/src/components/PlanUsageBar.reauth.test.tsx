// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { PlanUsageBar, type RefusedSeat } from './PlanUsageBar';
import type { MuxClient } from '../mux/client';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderBar(refusedSeats?: RefusedSeat[], mux?: MuxClient) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <PlanUsageBar refusedSeats={refusedSeats} mux={mux} />
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

// 🎯T1100: App only passes reauth_available seats; a missing-conversation
// broken seat must not be folded into Sign in.
it('does not show Sign in when refusedSeats is empty even if a seat is broken elsewhere', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: string) => {
    if (input === '/api/plan-usage/decisions') return { ok: true, json: async () => [] };
    if (input === '/api/plan-usage/auth') return { ok: true, json: async () => ({ plans: [{ provider: 'anthropic', state: 'ok' }] }) };
    return { ok: true, json: async () => ({ backends: [] }) };
  }));
  const { container } = renderBar([]);
  await waitFor(() => expect(screen.queryByRole('button', { name: /Sign in/ })).toBeNull());
  expect(container.querySelector('#plan-reauth-menu')).toBeNull();
});

// 🎯T1199: the billing reader and the agent can have different credentials.
// A failed meter must be visible without inventing a fleet sign-in failure.
it.each(['http', 'mux'])('reports usage failure independently of healthy agent auth: %s', async (transport) => {
  const reads: string[] = [];
  const unavailable = { backends: [{ provider: 'grok', status: 'unavailable', reason: 'grok billing: HTTP 401', windows: [] }] };
  vi.stubGlobal('fetch', vi.fn(async (input: string, init?: RequestInit) => {
    reads.push(input);
    expect(init?.method).not.toBe('POST');
    if (input === '/api/plan-usage/decisions') return { ok: true, json: async () => [] };
    if (input === '/api/plan-usage/auth') return { ok: true, json: async () => ({ plans: [{ provider: 'xai-oauth', state: 'ok' }] }) };
    return { ok: true, json: async () => unavailable };
  }));
  const mux = transport === 'mux' ? {
    subscribe(_channel: string, handler: (frame: unknown) => void) {
      queueMicrotask(() => handler({ t: 'frame', body: unavailable }));
      return () => {};
    },
    openChannel() {}, closeChannel() {},
  } as unknown as MuxClient : undefined;
  const { container } = renderBar([], mux);
  await waitFor(() => expect(container.textContent).toContain('usage unavailable'));
  fireEvent.pointerEnter(container.querySelector('[data-instant-tip-host]')!);
  await waitFor(() => expect(container.textContent).toContain('The agent login is healthy; this is a usage-reader failure.'));
  expect(container.textContent).toContain('grok billing: HTTP 401');
  expect(screen.queryByRole('button', { name: /Sign in/ })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Retry usage' })).toBeNull();
  expect(reads.filter((r) => r.includes('recover') || r.includes('refresh='))).toEqual([]);
});
