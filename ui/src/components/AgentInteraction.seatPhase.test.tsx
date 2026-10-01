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
  // 🎯T899: a busy agent seat sends at once; the daemon steers the message into
  // the turn and says when it will interrupt, and the pane counts down to it.
  it('busy seat: Enter reaches the wire at once and the escalation counts down; idle clears it', async () => {
    const view = render(<AgentInteraction mux={client} name={SEAT} density="compact" connected />);
    act(() => { emit('meta', { ...win, phase: { phase: 'tool', step: 'Read' } }); });
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'while busy' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(sends()).toEqual([{ text: 'while busy' }]);
    expect(view.container.querySelectorAll('#agent-inspect-send-queue .send-queue-item')).toHaveLength(0);

    const id = Socket.latest.sent.map((m) => JSON.parse(m)).filter((m) => m.t === 'send').pop().body.id;
    act(() => { emit('status', { id, status: 'steered', mode: 'submit', mechanism: 'stub_steer', interrupt_after_ms: 60000, message: 'steered' }); });
    await waitFor(() => expect(view.container.querySelector('.escalation-strip')?.textContent).toMatch(/interrupts in \d+s unless it takes the message/));
    expect(box.value).toBe('');

    act(() => { emit('meta', { phase: { phase: 'idle' } }); });
    await waitFor(() => expect(view.container.querySelector('.escalation-strip')).toBeNull());
  });

  // 🎯T903: the overseer pane escalates too. A busy overseer takes a plain
  // Enter at once (the daemon steers it into the owner turn) and the pane
  // counts down to the interrupt, instead of holding it until idle.
  it('busy overseer: Enter reaches the wire at once and the escalation counts down', async () => {
    const OVERSEER = 'jevons';
    const emitO = (t: string, body?: unknown) => Socket.latest.onmessage?.({ data: JSON.stringify({ v: 1, ch: `transcript:${OVERSEER}`, t, body }) });
    const view = render(<AgentInteraction mux={client} name={OVERSEER} density="compact" connected />);
    act(() => { emitO('meta', { ...win, phase: { phase: 'tool', step: 'Read' } }); });
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'while busy' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(sends()).toEqual([{ text: 'while busy' }]);
    expect(view.container.querySelectorAll('#agent-inspect-send-queue .send-queue-item')).toHaveLength(0);

    const id = Socket.latest.sent.map((m) => JSON.parse(m)).filter((m) => m.t === 'send').pop().body.id;
    act(() => { emitO('status', { id, status: 'steered', mode: 'submit', mechanism: 'steer', interrupt_after_ms: 60000, message: 'steered' }); });
    await waitFor(() => expect(view.container.querySelector('.escalation-strip')?.textContent).toMatch(/interrupts in \d+s unless it takes the message/));
  });

  // 🎯T903 (found by J35 live): a steered message usually paints in the
  // transcript before the daemon's status arrives. The echo must not cost
  // the pane its countdown.
  it('the countdown survives the echo landing before the status', async () => {
    const view = render(<AgentInteraction mux={client} name={SEAT} density="compact" connected />);
    act(() => { emit('meta', { ...win, phase: { phase: 'tool', step: 'Bash' } }); });
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'echo first' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    const id = Socket.latest.sent.map((m) => JSON.parse(m)).filter((m) => m.t === 'send').pop().body.id;
    const echo = { id: 'e:9', index: 9, op: 'put', type: 'user', event: { type: 'user', turn_origin: 'owner', message: { role: 'user', content: [{ type: 'text', text: 'echo first' }] } } };
    act(() => { emit('frame', echo); });
    act(() => { emit('status', { id, status: 'steered', mode: 'submit', mechanism: 'steer', interrupt_after_ms: 60000, message: 'steered' }); });
    await waitFor(() => expect(view.container.querySelector('.escalation-strip')?.textContent).toMatch(/interrupts in \d+s unless it takes the message/));
  });

  it('a plan wall holds the composer closed', () => {
    const view = render(<AgentInteraction mux={client} name={SEAT} density="compact" connected planWall="Upgrade your plan to continue" />);
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    expect(box.disabled).toBe(true);
    // The hint is drawn over the box, not a placeholder (Wispr Flow reads a
    // placeholder as typed text).
    expect(box.placeholder).toBe('');
    expect(view.container.querySelector('.composer-hint')?.textContent).toBe('Upgrade your plan to continue');
    expect((view.getByRole('button', { name: 'Send' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(sends()).toEqual([]);
  });

  // macOS accessibility reports an empty field's placeholder as its value,
  // so Wispr Flow read the hint as typed text and dictated mid-sentence. The
  // empty box exposes no text; the hint is aria-hidden and leaves on typing.
  it('the empty composer exposes no text to dictation and draws no hint', () => {
    const view = render(<AgentInteraction mux={client} name="jv-composer-hint" density="compact" connected />);
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    expect(box.hasAttribute('placeholder')).toBe(false);
    expect(view.container.querySelector('.composer-hint')).toBeNull();
  });

  it('an open seat still keeps Send off while the box is empty', () => {
    const view = render(<AgentInteraction mux={client} name={SEAT} density="compact" connected />);
    const box = view.getByRole('textbox') as HTMLTextAreaElement;
    expect(box.disabled).toBe(false);
    expect(box.placeholder).toBe('');
    expect(view.container.querySelector('.composer-hint')).toBeNull();
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
