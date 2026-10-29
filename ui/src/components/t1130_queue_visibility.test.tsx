// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement } from 'react';
import { cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AgentTranscript, ClippedBubble } from './AgentTranscript';
import { SendQueueStrip } from './SendQueueStrip';

// 🎯T1130: one visible queue for a message waiting behind a running turn,
// wherever it is held — a client-side queued item (case a, busy correctly
// known) and a daemon-held entry (case b, the browser's phase was stale)
// both offer Steer / Cut-in, never a plain owner row with at most Resend.

// jsdom has no layout: give the virtualizer a viewport so rows mount.
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(800);
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(900);
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ x: 0, y: 0, top: 0, left: 0, right: 900, bottom: 800, width: 900, height: 800, toJSON() {} });
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  HTMLElement.prototype.scrollTo = vi.fn();
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

// (a) busy known, correctly, client-side: the pending send sits in the
// client queue strip (useSendQueue's own state) and offers Steer / Cut-in —
// this path is unaffected by this target and already green (🎯T657 / T899).
it('(a) a message the client correctly knows is busy offers Steer / Cut-in as a queued item', () => {
  const onSteer = vi.fn();
  const onInterrupt = vi.fn();
  const { container } = render(
    createElement(SendQueueStrip, {
      items: [{ id: 'q1', text: 'also check the frontier' }],
      onSteer,
      onInterrupt,
      onRemove: vi.fn(),
      onEdit: vi.fn(),
    }),
  );
  const row = container.querySelector('.send-queue-item');
  expect(row).toBeTruthy();
  fireEvent.click(container.querySelector('.sq-send-now') as HTMLElement);
  expect(onSteer).toHaveBeenCalledWith('q1');
  fireEvent.click(container.querySelector('.sq-interrupt') as HTMLElement);
  expect(onInterrupt).toHaveBeenCalledWith('q1');
});

// (b) RED-today case: the browser's phase was stale-idle, the send went
// straight to the daemon, and the daemon replies queued_behind_turn
// (painted as delivery.state === 'held', 🎯T1130). The row must offer the
// same Steer / Cut-in, acting on that daemon-held entry, and leaves the
// list once delivered (covered by display.test.ts's state transition).
it('(b) a message the daemon holds behind a turn, despite a stale-idle client phase, offers Steer / Cut-in on that row', () => {
  const onSteerHeld = vi.fn();
  const onInterruptHeld = vi.fn();
  const owner = { type: 'user', msg_id: 'om-held', turn_origin: 'owner', message: { role: 'user', content: [{ type: 'text', text: "what's the status" }] } };
  const held = { type: 'send_error', state: 'held', msg_id: 'om-held', text: "message queued behind the overseer's running turn" };
  const mount = (frames: unknown[]) =>
    createElement(AgentTranscript, {
      name: 'jevons', density: 'comfortable', frames, meta: { start: 0, older: 0, total: frames.length }, ready: true,
      onSteerHeld, onInterruptHeld,
    });
  const view = render(mount([owner, held]));
  // Held messages no longer paint a duplicate transcript row.
  expect(view.container.querySelector('.msg.user')).toBeNull();

  const delivered = { type: 'send_error', state: 'delivered', msg_id: 'om-held', text: 'message delivered to the overseer after retry' };
  const after = render(mount([owner, held, delivered]));
  const afterRows = after.container.querySelectorAll('.msg-delivery');
  expect(afterRows).toHaveLength(1);
  expect(afterRows[0].getAttribute('data-state')).toBe('delivered');
  expect(after.container.querySelector('.msg-steer')).toBeNull();
});

it('a plain owner bubble with no delivery state has no held chrome', () => {
  const { container } = render(createElement(ClippedBubble, { index: 0, kind: 'user', text: 'hi', start: 0 }));
  expect(container.querySelector('.msg-delivery')).toBeNull();
});

// Stale-idle regression: the daemon's held row must be projected into the
// very same strip as client items, not left in the transcript.
it('(b2) projects a stale-idle daemon hold and a client hold into one ordered strip with all four controls', async () => {
  const { queueItemsFromFrames } = await import('./AgentInteraction');
  const frames = [
    { type: 'user', msg_id: 'om-held', turn_origin: 'owner', message: { role: 'user', content: [{ type: 'text', text: 'first' }] } },
    { type: 'send_error', msg_id: 'om-held', state: 'held', text: 'queued behind turn' },
  ];
  const items = queueItemsFromFrames(frames, [{ id: 'q1', text: 'second' }]);
  const handlers = { onSteer: vi.fn(), onInterrupt: vi.fn(), onEdit: vi.fn(), onRemove: vi.fn() };
  const { container } = render(createElement(SendQueueStrip, { items, ...handlers }));
  const rows = container.querySelectorAll('.send-queue-item');
  expect(rows).toHaveLength(2);
  expect(rows[1].textContent).toContain('first');
  for (const [selector, action] of [['.sq-send-now', 'onSteer'], ['.sq-interrupt', 'onInterrupt'], ['.sq-edit', 'onEdit'], ['.sq-remove', 'onRemove']] as const) {
    fireEvent.click(rows[1].querySelector(selector) as HTMLElement);
    expect(handlers[action]).toHaveBeenCalledWith('om-held');
  }
  expect(queueItemsFromFrames([...frames, { type: 'send_error', msg_id: 'om-held', state: 'delivered', text: 'delivered' }], [])).toEqual([]);
});
