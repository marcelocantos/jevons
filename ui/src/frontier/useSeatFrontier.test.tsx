// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { PropsWithChildren } from 'react';
import { useSeatFrontier } from './useSeatFrontier';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it('T996 isolates selected repos, late responses, errors, and deselection', async () => {
  const pending: Array<(r: unknown) => void> = [];
  const fetcher = vi.fn(() => new Promise((resolve) => pending.push(resolve)));
  vi.stubGlobal('fetch', fetcher);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: PropsWithChildren) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  const { result, rerender } = renderHook(({ cwd }) => {
    const query = useSeatFrontier(cwd);
    return { data: query.data, isError: query.isError };
  }, { initialProps: { cwd: '' }, wrapper });
  const answer = (name: string) => ({ ok: true, json: async () => ({ ledger_key: '/' + name + '/bullseye.yaml', targets: [{ id: 'T1', name, status: 'identified' }] }) });
  expect(fetcher).not.toHaveBeenCalled();
  rerender({ cwd: '/repo A' });
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
  expect(fetcher.mock.calls[0]).toEqual(['/api/frontier?cwd=%2Frepo%20A', { signal: expect.any(AbortSignal) }]);
  await act(async () => pending[0](answer('A')));
  await waitFor(() => expect(result.current.data?.rows[0].name).toBe('A'));

  rerender({ cwd: '/repo B' });
  expect(result.current.data).toBeUndefined();
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
  rerender({ cwd: '/repo C' });
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(3));
  await act(async () => pending[2](answer('C')));
  await waitFor(() => expect(result.current.data?.rows[0].name).toBe('C'));
  await act(async () => pending[1](answer('B')));
  expect(result.current.data?.ledgerKey).toBe('/C/bullseye.yaml');

  rerender({ cwd: '/broken' });
  expect(result.current.data).toBeUndefined();
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(4));
  await act(async () => pending[3]({ ok: false, status: 503 }));
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.data).toBeUndefined();

  rerender({ cwd: '' });
  expect(result.current.data).toBeUndefined();
  await act(async () => { await client.invalidateQueries({ queryKey: ['frontier'] }); });
  expect(fetcher).toHaveBeenCalledTimes(4);
  client.clear();
});
