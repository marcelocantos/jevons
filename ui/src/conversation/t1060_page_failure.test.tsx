// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { act, cleanup, renderHook, render } from '@testing-library/react';
import { createElement } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { MuxClient } from '../mux/client';
import type { MuxEnvelope } from '../mux/protocol';
import { userTurn } from '../oracle/fixtures';
import { AgentTranscript } from '../components/AgentTranscript';
import { useConversation } from './useConversation';

afterEach(cleanup);

it('a tagged page storage failure shows retry for the exact cursor without mutating transcript; success clears it (T1060)', () => {
  let receive: (env: MuxEnvelope) => void = () => {};
  const sent: { before?: string; limit?: number }[] = [];
  const mux = {
    isOpen: () => true,
    subscribe: (_ch: string, fn: (env: MuxEnvelope) => void) => { receive = fn; return () => {}; },
    openTranscript: () => {}, closeTranscript: () => {},
    pageTranscript: (_name: string, spec: { before?: string; limit?: number }) => sent.push(spec),
  } as unknown as MuxClient;
  const view = renderHook(() => useConversation(mux, 'jevons'));
  const env = (t: MuxEnvelope['t'], body: unknown): MuxEnvelope => ({ v: 1, ch: 'transcript:jevons', t, body });
  act(() => {
    receive(env('frame', { id: 'e:90', index: 90, event: userTurn('loaded 90') }));
    receive(env('meta', { start: 90, older: 90, total: 150, following: false }));
  });
  act(() => view.result.current.pageOlder());
  expect(sent).toEqual([{ before: 'e:90', limit: 50 }]);
  act(() => receive(env('error', { op: 'page', before: 'e:89', error: 'stale request' })));
  expect(view.result.current.olderPage?.loading).toBe(true); // stale page cannot hijack current request
  act(() => receive(env('error', { op: 'page', before: 'e:90', error: 'Could not load earlier history' })));
  expect(view.result.current.olderPage).toEqual({ before: 'e:90', loading: false, error: 'Could not load earlier history.' });
  expect(view.result.current.frames).toHaveLength(1); // not a send_error bubble
  act(() => { view.result.current.retryOlder(); view.result.current.retryOlder(); });
  expect(sent).toEqual([{ before: 'e:90', limit: 50 }, { before: 'e:90', limit: 50 }]);
  act(() => receive(env('page', { before: 'e:90', lines: [], start: 1, older: 0, total: 150, truncated: false })));
  expect(view.result.current.olderPage).toBeNull();
  expect(view.result.current.meta?.older).toBe(0);
  expect(view.result.current.frames).toHaveLength(1);
  // Another page can now start. The delayed first-page reply must not
  // overwrite its cursor, metadata or rows.
  act(() => view.result.current.pageOlder());
  const pending = view.result.current.olderPage;
  act(() => receive(env('page', { before: 'e:old-stale', lines: [], start: 10, older: 10, total: 150 })));
  expect(view.result.current.olderPage).toEqual(pending);
  expect(view.result.current.meta?.older).toBe(0);
  act(() => receive(env('error', { error: 'send failed' })));
  expect(view.result.current.frames).toHaveLength(2); // unrelated send failure stays diagnostic
});

it('failed page and true exhaustion are distinct visible states (T1060)', () => {
  const retry = vi.fn();
  const frame = userTurn('oldest preserved');
  const props = (older: number, page: { before: string; loading: boolean; error: string | null } | null, truncated = false) =>
    createElement(AgentTranscript, {
      name: 'jevons', frames: [frame], meta: { start: older, older, total: 90, following: true, truncated }, following: false,
      ready: true, onPageOlder: () => {}, olderPage: page, onRetryOlder: retry,
    });
  const view = render(props(50, { before: 'e:50', loading: false, error: 'Could not load earlier history.' }));
  expect(view.getByRole('alert').textContent).toContain('Retry');
  expect(view.queryByText('Start of history')).toBeNull();
  act(() => view.getByText('Retry').click());
  expect(retry).toHaveBeenCalledOnce();
  view.rerender(props(50, null, true)); // empty successful page, still truncated
  expect(view.queryByText('Start of history')).toBeNull();
  view.rerender(props(0, null));
  expect(view.getByText('Start of history')).toBeTruthy();
});

it('offline paging is not queued and a reconnect on the same client clears pending/error (T1060)', () => {
  class Socket {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 3;
    static instances: Socket[] = [];
    readyState = Socket.CONNECTING;
    sent: string[] = [];
    onopen: (() => void) | null = null;
    onmessage: ((event: { data: string }) => void) | null = null;
    onclose: (() => void) | null = null;
    constructor(_url: string) { Socket.instances.push(this); }
    send(message: string) { this.sent.push(message); }
    open() { this.readyState = Socket.OPEN; this.onopen?.(); }
    close() { this.readyState = Socket.CLOSED; this.onclose?.(); }
    receive(t: string, body: unknown) {
      this.onmessage?.({ data: JSON.stringify({ v: 1, ch: 'transcript:jevons', t, body }) });
    }
    pages() { return this.sent.filter((message) => JSON.parse(message).t === 'page'); }
  }
  vi.stubGlobal('WebSocket', Socket);
  vi.useFakeTimers();
  const client = new MuxClient('ws://test/ws/mux', () => 1);
  const view = renderHook(() => useConversation(client, 'jevons'));
  try {
    // The hook opens a watch before connect; that queued open is fine, but
    // an explicit page must never join that offline queue or paint loading.
    act(() => view.result.current.pageOlder());
    expect(view.result.current.olderPage).toBeNull();
    client.connect();
    const first = Socket.instances[0];
    act(() => {
      first.open();
      first.receive('frame', { id: 'e:90', index: 90, event: userTurn('loaded 90') });
      first.receive('meta', { start: 90, older: 90, total: 150, following: false });
      view.result.current.pageOlder();
    });
    expect(view.result.current.olderPage).toEqual({ before: 'e:90', loading: true, error: null });
    expect(first.pages()).toHaveLength(1);
    act(() => first.receive('error', { op: 'page', before: 'e:90' }));
    act(() => first.close());
    expect(view.result.current.olderPage?.error).toBeTruthy();
    act(() => view.result.current.retryOlder());
    expect(view.result.current.olderPage?.loading).toBe(false);
    expect(first.pages()).toHaveLength(1);
    const offline = renderHook(() => useConversation(null, 'jevons'));
    act(() => offline.result.current.pageOlder());
    expect(offline.result.current.olderPage).toBeNull();
    offline.unmount();

    // Reconnect dispatches t:reset for the watched transcript. The hook's
    // existing subscription must clear error without a new MuxClient object.
    act(() => vi.advanceTimersByTime(500));
    const second = Socket.instances[1];
    expect(second).toBeTruthy();
    act(() => second.open());
    expect(view.result.current.olderPage).toBeNull();
    expect(second.pages()).toHaveLength(0);
    act(() => {
      second.receive('frame', { id: 'e:80', index: 80, event: userTurn('loaded 80') });
      second.receive('meta', { start: 80, older: 80, total: 150, following: false });
      view.result.current.pageOlder();
    });
    expect(view.result.current.olderPage?.loading).toBe(true);
    act(() => second.close());
    act(() => vi.advanceTimersByTime(500));
    const third = Socket.instances[2];
    act(() => third.open());
    expect(view.result.current.olderPage).toBeNull(); // pending also resets
    expect(third.pages()).toHaveLength(0);
  } finally {
    view.unmount();
    client.close();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  }
});
