// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import '../composer/ensureLocalStorage';
import { act, cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MuxClient } from '../mux/client';
import { AgentInteraction } from './AgentInteraction';
import { usePendingImages } from '../store/pendingImages';

// 🎯T247 / 🎯T250 / 🎯T251: target: on the main composer opens an aside
// immediately and never sends the raw prefix to the overseer wire.
class Socket {
  static OPEN = 1;
  static CONNECTING = 0;
  static latest: Socket;
  readyState = Socket.OPEN;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  sent: string[] = [];
  constructor() { Socket.latest = this; }
  send(data: string) { this.sent.push(data); }
  close() { this.readyState = 3; }
}
let client: MuxClient;
let fetchMock: ReturnType<typeof vi.fn>;
beforeEach(() => {
  localStorage.clear();
  usePendingImages.setState({ images: {} });
  vi.stubGlobal('WebSocket', Socket);
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(800);
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(900);
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: 0, y: 0, top: 0, left: 0, right: 900, bottom: 800, width: 900, height: 800, toJSON() {},
  });
  HTMLElement.prototype.scrollTo = vi.fn();
  fetchMock = vi.fn(async (url: string) => {
    if (String(url).includes('/api/asides')) {
      return { ok: true, json: async () => ({ name: 'att-fixture', purpose: 'aside' }), text: async () => '' };
    }
    return { ok: true, json: async () => ({}), text: async () => '' };
  });
  vi.stubGlobal('fetch', fetchMock);
  client = new MuxClient('ws://fixture/ws/mux');
  client.connect();
  Socket.latest.onopen?.();
});
afterEach(() => { cleanup(); client.close(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const sends = () => Socket.latest.sent.map((s) => JSON.parse(s)).filter((m) => m.t === 'send');

describe('explicit aside prefix (T247 / T250 / T251)', () => {
  it('target: POSTs /api/asides, selects the aside, and does not send to main', async () => {
    const opened: string[] = [];
    const view = render(
      <AgentInteraction
        mux={client}
        name="jevons"
        density="comfortable"
        ownerChat
        connected
        onOpenAside={(id) => opened.push(id)}
      />,
    );
    act(() => {
      Socket.latest.onmessage?.({
        data: JSON.stringify({ v: 1, ch: 'transcript:jevons', t: 'meta', body: { start: 0, older: 0, total: 0, n: 0, following: true, phase: 'idle' } }),
      });
    });
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'target: T540.1 census aside from journey' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(opened.length).toBe(1));
    expect(opened[0]).toMatch(/^att-/);
    expect(sends()).toEqual([]);
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const asideCalls = fetchMock.mock.calls.filter((c) => String(c[0]).includes('/api/asides'));
    expect(asideCalls.length).toBe(1);
    const init = asideCalls[0][1] as { method?: string; body?: string };
    expect(init.method).toBe('POST');
    const body = JSON.parse(String(init.body));
    expect(body.kind).toBe('target');
    expect(body.title).toMatch(/T540\.1 census aside/);
    expect(body.text).toBeUndefined();
    await waitFor(() => expect(box.value).toBe(''));
  });

  it('ordinary chat still goes to the overseer wire', async () => {
    const opened: string[] = [];
    const view = render(
      <AgentInteraction
        mux={client}
        name="jevons"
        density="comfortable"
        ownerChat
        connected
        onOpenAside={(id) => opened.push(id)}
      />,
    );
    act(() => {
      Socket.latest.onmessage?.({
        data: JSON.stringify({ v: 1, ch: 'transcript:jevons', t: 'meta', body: { start: 0, older: 0, total: 0, n: 0, following: true, phase: 'idle' } }),
      });
    });
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'hello overseer' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(sends().length).toBe(1));
    expect(sends()[0].body.text).toBe('hello overseer');
    expect(opened).toEqual([]);
    expect(fetchMock.mock.calls.some((c) => String(c[0]).includes('/api/asides'))).toBe(false);
  });
});
