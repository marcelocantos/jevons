// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import '../composer/ensureLocalStorage';
import { act, cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MuxClient } from '../mux/client';
import { AgentInteraction } from './AgentInteraction';

// 🎯T657 slice 2a, owner path end to end inside the widget: a busy overseer
// phase turns plain Enter into a queued follow-up that paints above the
// composer and drains to the mux socket on idle. Only the socket is a fixture.
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
beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal('WebSocket', Socket);
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(800);
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(900);
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ x: 0, y: 0, top: 0, left: 0, right: 900, bottom: 800, width: 900, height: 800, toJSON() {} });
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  HTMLElement.prototype.scrollTo = vi.fn();
  client = new MuxClient('ws://fixture/ws/mux');
  client.connect();
  Socket.latest.onopen?.();
});
afterEach(() => { cleanup(); client.close(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const sends = () => Socket.latest.sent.map((s) => JSON.parse(s)).filter((m) => m.t === 'send').map((m) => m.body);

describe('send queue wiring (T657 / T113)', () => {
  it('busy overseer: Enter queues above the composer; idle drains it to the wire; Cmd+Enter steers past it', async () => {
    const view = render(<AgentInteraction mux={client} name="jevons" density="comfortable" connected />);
    const emit = (t: string, body?: unknown) => Socket.latest.onmessage?.({ data: JSON.stringify({ v: 1, ch: 'transcript:jevons', t, body }) });
    act(() => { emit('meta', { start: 1, older: 0, total: 0, n: 0, following: true, phase: 'thinking' }); });
    const box = view.getByRole('textbox') as HTMLTextAreaElement;

    fireEvent.change(box, { target: { value: 'follow up' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(sends()).toEqual([]);
    const strip = view.container.querySelector('#send-queue') as HTMLElement;
    await waitFor(() => expect(strip.classList.contains('visible')).toBe(true));
    expect([...strip.querySelectorAll('.send-queue-item .sq-text')].map((el) => el.textContent)).toEqual(['follow up']);
    expect(box.value).toBe('');

    fireEvent.change(box, { target: { value: 'second' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(strip.querySelectorAll('.send-queue-item')).toHaveLength(2));
    // Newest paints at the top; the item next to send sits at the bottom, nearest the composer.
    expect([...strip.querySelectorAll('.send-queue-item .sq-text')].map((el) => el.textContent)).toEqual(['second', 'follow up']);
    expect(strip.querySelector('[data-queue-next="true"] .sq-text')?.textContent).toBe('follow up');

    fireEvent.change(box, { target: { value: 'go left' } });
    fireEvent.keyDown(box, { key: 'Enter', metaKey: true });
    expect(sends()).toEqual([{ text: 'go left', mode: 'steer' }]);
    expect(strip.querySelectorAll('.send-queue-item')).toHaveLength(2);

    act(() => { emit('meta', { start: 1, older: 0, total: 0, n: 0, following: true, phase: 'idle' }); });
    await waitFor(() => expect(sends()).toEqual([{ text: 'go left', mode: 'steer' }, { text: 'follow up' }]));
    // The optimistic received phase after that send holds the second item until the next idle.
    await waitFor(() => expect([...strip.querySelectorAll('.send-queue-item .sq-text')].map((el) => el.textContent)).toEqual(['second']));
    act(() => { emit('meta', { start: 1, older: 0, total: 0, n: 0, following: true, phase: 'idle' }); });
    await waitFor(() => expect(sends().map((b) => b.text)).toEqual(['go left', 'follow up', 'second']));
    await waitFor(() => expect(strip.classList.contains('visible')).toBe(false));
  });

  it('strip buttons: Steer sends with mode=steer, Cut in with mode=interrupt, Remove drops it', async () => {
    const view = render(<AgentInteraction mux={client} name="jevons" density="comfortable" connected />);
    const emit = (t: string, body?: unknown) => Socket.latest.onmessage?.({ data: JSON.stringify({ v: 1, ch: 'transcript:jevons', t, body }) });
    act(() => { emit('meta', { start: 1, older: 0, total: 0, n: 0, following: true, phase: 'tool' }); });
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    for (const t of ['a', 'b', 'c']) {
      fireEvent.change(box, { target: { value: t } });
      fireEvent.keyDown(box, { key: 'Enter' });
    }
    const strip = view.container.querySelector('#send-queue') as HTMLElement;
    await waitFor(() => expect(strip.querySelectorAll('.send-queue-item')).toHaveLength(3));
    const row = (text: string) => [...strip.querySelectorAll('.send-queue-item')].find((el) => el.querySelector('.sq-text')?.textContent === text) as HTMLElement;
    fireEvent.click(row('b').querySelector('.sq-send-now')!);
    expect(sends()).toEqual([{ text: 'b', mode: 'steer' }]);
    fireEvent.click(row('c').querySelector('.sq-interrupt')!);
    expect(sends()).toEqual([{ text: 'b', mode: 'steer' }, { text: 'c', mode: 'interrupt', interrupt: true }]);
    fireEvent.click(row('a').querySelector('.sq-remove')!);
    await waitFor(() => expect(strip.querySelectorAll('.send-queue-item')).toHaveLength(0));
    expect(sends()).toHaveLength(2);
    expect(JSON.parse(localStorage.getItem('jevons-send-queue-v1') || '{}').items).toEqual([]);
  });
});
