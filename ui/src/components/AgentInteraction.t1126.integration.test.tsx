// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import '../composer/ensureLocalStorage';
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MuxClient } from '../mux/client';
import { AgentInteraction } from './AgentInteraction';
import { usePendingImages } from '../store/pendingImages';

// 🎯T1126: full-widget version of the AgentTranscript-level reattach test —
// an owner composer send while scrolled up clears the ↓ Latest button.
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
  usePendingImages.setState({ images: {} });
  vi.stubGlobal('WebSocket', Socket);
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(800);
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(900);
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ x: 0, y: 0, top: 0, left: 0, right: 900, bottom: 800, width: 900, height: 800, toJSON() {} });
  HTMLElement.prototype.scrollTo = vi.fn();
  client = new MuxClient('ws://fixture/ws/mux');
  client.connect();
  Socket.latest.onopen?.();
});
afterEach(() => { cleanup(); client.close(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const sends = () => Socket.latest.sent.map((s) => JSON.parse(s)).filter((m) => m.t === 'send');

function geometry(el: HTMLElement, scrollHeight: number, clientHeight: number) {
  Object.defineProperty(el, 'scrollHeight', { value: scrollHeight, configurable: true });
  Object.defineProperty(el, 'clientHeight', { value: clientHeight, configurable: true });
}

describe('owner send re-attaches follow (T1126, full widget)', () => {
  it('scrolled up with ↓ Latest showing: sending restores follow and the button disappears', async () => {
    const view = render(<AgentInteraction mux={client} name="jevons" density="comfortable" connected />);
    const emit = (t: string, body?: unknown) => Socket.latest.onmessage?.({ data: JSON.stringify({ v: 1, ch: 'transcript:jevons', t, body }) });
    const earlierFrame = {
      id: 'e:1', index: 1, op: 'put', type: 'user',
      event: { type: 'user', turn_origin: 'owner', message: { role: 'user', content: [{ type: 'text', text: 'earlier request' }] } },
    };
    act(() => {
      emit('frame', earlierFrame);
      emit('meta', { start: 1, older: 0, total: 1, n: 1, following: true, phase: 'idle' });
    });

    const scroller = document.getElementById('messages') as HTMLDivElement;
    geometry(scroller, 20000, 900);
    // Let the hydrate pin's own rAF-scheduled write settle before driving a
    // real scroll (see t603_repin_after_settle: a scroll fired mid-pin is
    // attributed to the pin's own write, not a user leave).
    await act(async () => { await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))); });

    expect((view.queryByText('↓ Latest') as HTMLElement | null)?.hasAttribute('hidden') ?? true).toBe(true);

    scroller.scrollTop = 5000;
    act(() => { fireEvent.scroll(scroller); });

    expect((view.queryByText('↓ Latest') as HTMLElement).hasAttribute('hidden')).toBe(false);

    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'hello from up above' } });
    fireEvent.keyDown(box, { key: 'Enter' });

    expect(sends().length).toBe(1);
    expect((view.queryByText('↓ Latest') as HTMLElement).hasAttribute('hidden')).toBe(true);
  });
});
