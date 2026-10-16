// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import '../composer/ensureLocalStorage';
import { act, cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MuxClient } from '../mux/client';
import { AgentInteraction } from './AgentInteraction';

/**
 * 🎯T616: a send held while a reply is streaming paints in the transcript as
 * queued, so the owner can see which request the stream is still answering
 * and that the new one has not been picked up yet.
 */
class Socket {
  static OPEN = 1;
  static CONNECTING = 0;
  static latest: Socket;
  readyState = Socket.OPEN;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  sent: string[] = [];
  constructor() {
    Socket.latest = this;
  }
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.readyState = 3;
  }
}

let client: MuxClient;
beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal('WebSocket', Socket);
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(800);
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(900);
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: 0, y: 0, top: 0, left: 0, right: 900, bottom: 800, width: 900, height: 800, toJSON() {},
  });
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  });
  HTMLElement.prototype.scrollTo = vi.fn();
  client = new MuxClient('ws://fixture/ws/mux');
  client.connect();
  Socket.latest.onopen?.();
});
afterEach(() => {
  cleanup();
  client.close();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('T616 transcript binding', () => {
  it('queued follow-up appears in the transcript as a dashed owner bubble marked queued', async () => {
    // escalate=false holds plain Enter until idle (the T616 misread case).
    const view = render(
      <AgentInteraction mux={client} name="jevons" escalate={false} density="comfortable" connected />,
    );
    const emit = (t: string, body?: unknown) =>
      Socket.latest.onmessage?.({ data: JSON.stringify({ v: 1, ch: 'transcript:jevons', t, body }) });

    const owner = {
      id: 'e:1',
      index: 1,
      op: 'put',
      type: 'user',
      event: {
        type: 'user',
        turn_origin: 'owner',
        message: { role: 'user', content: [{ type: 'text', text: 'switch both seats to Grok' }] },
      },
    };
    const stream = {
      id: 'e:2',
      index: 2,
      op: 'put',
      type: 'assistant',
      event: {
        type: 'assistant',
        stream_id: 's1',
        message: { role: 'assistant', content: [{ type: 'text', text: 'Switching the seats now…' }] },
      },
    };
    act(() => {
      emit('frame', owner);
      emit('frame', stream);
      emit('meta', { start: 1, older: 0, total: 2, n: 2, following: true, phase: 'streaming' });
    });

    // Streaming assistant is marked unsealed with a "still writing…" cue.
    await waitFor(() => {
      const streaming = view.container.querySelector('.msg.jevons.streaming');
      expect(streaming).not.toBeNull();
      expect(streaming?.getAttribute('data-sealed')).toBe('false');
      expect(streaming?.textContent).toMatch(/still writing/i);
    });

    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'why is MCP search empty?' } });
    fireEvent.keyDown(box, { key: 'Enter' });

    // Queue strip still paints (T657), AND the transcript shows the same text as queued.
    await waitFor(() => {
      const queued = view.container.querySelectorAll('.msg.user.queued');
      expect(queued.length).toBeGreaterThanOrEqual(1);
      const last = queued[queued.length - 1];
      expect(last.querySelector('.msg-delivery')?.getAttribute('data-state')).toBe('queued');
      expect(last.textContent).toMatch(/why is MCP search empty/);
      expect(last.textContent).toMatch(/queued — waiting for this turn to end/i);
    });

    // The live stream is still answering the earlier request, not the queued one.
    const kinds = [...view.container.querySelectorAll('[data-kind]')].map((el) => el.getAttribute('data-kind'));
    expect(kinds).toEqual(expect.arrayContaining(['user', 'assistant', 'user']));
    const msgs = [...view.container.querySelectorAll('.msg')];
    // Order on canvas: original owner, streaming assistant, queued follow-up.
    const texts = msgs.map((m) => (m.querySelector('.msg-body')?.textContent || m.textContent || '').trim());
    expect(texts.some((t) => t.includes('switch both seats'))).toBe(true);
    expect(texts.some((t) => t.includes('Switching the seats'))).toBe(true);
    expect(texts.some((t) => t.includes('why is MCP search empty'))).toBe(true);
  });

  it('a sealed assistant loses the streaming cue', async () => {
    const view = render(
      <AgentInteraction mux={client} name="jevons" escalate={false} density="comfortable" connected />,
    );
    const emit = (t: string, body?: unknown) =>
      Socket.latest.onmessage?.({ data: JSON.stringify({ v: 1, ch: 'transcript:jevons', t, body }) });
    act(() => {
      emit('frame', {
        id: 'e:1',
        index: 1,
        op: 'put',
        type: 'assistant',
        event: {
          type: 'assistant',
          message: {
            role: 'assistant',
            content: [{ type: 'text', text: 'All done.' }],
            stop_reason: 'end_turn',
          },
        },
      });
      emit('meta', { start: 1, older: 0, total: 1, n: 1, following: true, phase: 'idle' });
    });
    await waitFor(() => {
      const sealed = view.container.querySelector('.msg.jevons.sealed');
      expect(sealed).not.toBeNull();
      expect(sealed?.getAttribute('data-sealed')).toBe('true');
      expect(sealed?.textContent).not.toMatch(/still writing/i);
    });
  });
});
