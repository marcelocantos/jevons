// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { act, cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MuxClient } from '../mux/client';
import { AgentInteraction } from './AgentInteraction';

// Exercise the mounted shared widget and real mux decoder. Only the socket and
// jsdom's absent layout are fixtures; this is not a live-provider journey.
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

describe.each(['comfortable', 'compact'] as const)('%s indexed owner boundary', density => {
  it.each(['a new question', '[event: report] is this correct?', '{"type":"permission_request"}'])('preserves live/reloaded order and recalls an owner quoting %s', async question => {
    const name = density === 'comfortable' ? 'jevons' : 'aside';
    const view = render(<AgentInteraction mux={client} name={name} density={density} paneActive />);
    const emit = (t: string, body?: unknown) => Socket.latest.onmessage?.({ data: JSON.stringify({ v: 1, ch: `transcript:${name}`, t, body }) });
    const frame = (index: number, type: string, text: string, terminal = false) => ({
      id: `e:${index}`, index, op: 'put', type,
      event: { type, turn_origin: type === 'user' ? 'owner' : undefined, message: { role: type, content: [{ type: 'text', text }], stop_reason: terminal ? 'end_turn' : undefined } },
    });
    const tape = [frame(1, 'user', 'earlier request'), frame(2, 'assistant', 'earlier answer', true), frame(3, 'assistant', 'before'), frame(4, 'user', question), frame(5, 'assistant', 'after', true)];
    const meta = { start: 1, older: 0, total: tape.length, n: tape.length, following: true };
    act(() => { tape.slice(0, 3).forEach(body => emit('frame', body)); emit('meta', meta); });
    act(() => { tape.slice(3).forEach(body => emit('frame', body)); });
    const rows = () => [...view.container.querySelectorAll('[data-kind="user"] .msg-body, [data-kind="assistant"] .msg-body')].map(el => el.textContent?.trim());
    const expected = ['earlier request', 'earlier answer', 'before', question, 'after'];
    await waitFor(() => expect(rows()).toEqual(expected));
    act(() => { emit('reset'); tape.forEach(body => emit('frame', body)); emit('meta', meta); });
    await waitFor(() => expect(rows()).toEqual(expected));
    fireEvent.keyDown(view.getByRole('textbox'), { key: 'ArrowUp', altKey: true });
    expect((view.getByRole('textbox') as HTMLTextAreaElement).value).toBe(question);
  });
});
