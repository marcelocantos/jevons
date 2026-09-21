// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement } from 'react';
import { cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AgentTranscript, ClippedBubble } from './AgentTranscript';

// jsdom has no layout: give the virtualizer a viewport so rows mount.
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(800);
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(900);
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ x: 0, y: 0, top: 0, left: 0, right: 900, bottom: 800, width: 900, height: 800, toJSON() {} });
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  HTMLElement.prototype.scrollTo = vi.fn();
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const owner = { type: 'user', msg_id: 'om-1', turn_origin: 'owner', message: { role: 'user', content: [{ type: 'text', text: 'decisions' }] } };
const undelivered = { type: 'send_error', state: 'undelivered', msg_id: 'om-1', reason: 'broker protocol: not_owner', text: 'message not delivered — will retry: broker protocol: not_owner' };
const delivered = { type: 'send_error', state: 'delivered', msg_id: 'om-1', text: 'message delivered to the overseer after retry' };

// 🎯T811: the delivery state is painted into the owner's message row, in both densities.
for (const density of ['comfortable', 'compact'] as const) {
  it(`the transcript paints not delivered with Resend, then flips once to delivered (${density})`, () => {
    const onResend = vi.fn();
    const mount = (frames: unknown[]) =>
      createElement(AgentTranscript, { name: 'jevons', density, frames, meta: { start: 0, older: 0, total: frames.length }, ready: true, onResend });
    const view = render(mount([owner, undelivered]));
    const row = view.container.querySelector('.msg.user .msg-delivery');
    expect(row?.getAttribute('data-state')).toBe('undelivered');
    expect(row?.textContent).toContain('not delivered: broker protocol: not_owner');
    fireEvent.click(view.container.querySelector('.msg-resend') as HTMLElement);
    expect(onResend).toHaveBeenCalledExactlyOnceWith('om-1');
    // no separate diagnostic pile: the state is in the row
    expect(view.container.querySelectorAll('[data-kind="diagnostic"]')).toHaveLength(0);

    view.rerender(mount([owner, undelivered, delivered]));
    const after = view.container.querySelectorAll('.msg-delivery');
    expect(after).toHaveLength(1);
    expect(after[0].getAttribute('data-state')).toBe('delivered');
    expect(view.container.querySelector('.msg-resend')).toBeNull();
  });
}

it('a plain owner bubble has no delivery line and Resend needs a message id', () => {
  const { container } = render(createElement(ClippedBubble, { index: 0, kind: 'user', text: 'hi', start: 0, delivery: { state: 'undelivered', reason: 'x' } }));
  expect(container.querySelector('.msg-resend')).toBeNull();
  const plain = render(createElement(ClippedBubble, { index: 0, kind: 'user', text: 'hi', start: 0 }));
  expect(plain.container.querySelector('.msg-delivery')).toBeNull();
});
