// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import '../composer/ensureLocalStorage';
import { act, cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MuxClient } from '../mux/client';
import { AgentInteraction } from './AgentInteraction';

// 🎯T562.2: a worker/aside composer is busy from ITS OWN phase sample, in the
// object shape the daemon puts on that seat's transcript meta. Busy queues a
// plain Enter visibly; idle sends straight through. Only the socket is a fixture.
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

// 🎯T562.5: send bodies carry a per-send correlation id; strip it for text-shape assertions.
const sends = () => Socket.latest.sent.map((s) => JSON.parse(s)).filter((m) => m.t === 'send').map((m) => ({ text: m.body.text }));
const SEAT = 'jv-t562.2-seat-phase';
const emit = (t: string, body?: unknown) => Socket.latest.onmessage?.({ data: JSON.stringify({ v: 1, ch: `transcript:${SEAT}`, t, body }) });
const win = { start: 1, older: 0, total: 0, n: 0, following: true };

describe('seat composer busy from its own phase (T562.2)', () => {
  it('busy seat: Enter queues visibly, nothing reaches the wire; idle drains it', async () => {
    const view = render(<AgentInteraction mux={client} name={SEAT} density="compact" connected />);
    act(() => { emit('meta', { ...win, phase: { phase: 'tool', step: 'Read' } }); });
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'while busy' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(sends()).toEqual([]);
    const strip = view.container.querySelector('#agent-inspect-send-queue') as HTMLElement;
    await waitFor(() => expect(strip.classList.contains('visible')).toBe(true));
    expect([...strip.querySelectorAll('.send-queue-item .sq-text')].map((el) => el.textContent)).toEqual(['while busy']);

    act(() => { emit('meta', { phase: { phase: 'idle' } }); });
    await waitFor(() => expect(sends()).toEqual([{ text: 'while busy' }]));
  });

  it('a plan wall holds the composer closed', () => {
    const view = render(<AgentInteraction mux={client} name={SEAT} density="compact" connected planWall="Upgrade your plan to continue" />);
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    expect(box.disabled).toBe(true);
    expect(box.placeholder).toBe('Upgrade your plan to continue');
    expect((view.getByRole('button', { name: 'Send' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(sends()).toEqual([]);
  });

  it('an open seat still keeps Send off while the box is empty', () => {
    const view = render(<AgentInteraction mux={client} name={SEAT} density="compact" connected />);
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    expect(box.disabled).toBe(false);
    expect(box.placeholder).toBe('Message this agent…');
    expect((view.getByRole('button', { name: 'Send' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('idle seat: Enter sends straight through and nothing is queued', async () => {
    const view = render(<AgentInteraction mux={client} name={SEAT} density="compact" connected />);
    act(() => { emit('meta', { ...win, phase: { phase: 'idle' } }); });
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'hello' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(sends()).toEqual([{ text: 'hello' }]);
    const strip = view.container.querySelector('#agent-inspect-send-queue') as HTMLElement;
    expect(strip.querySelectorAll('.send-queue-item')).toHaveLength(0);
  });
});
