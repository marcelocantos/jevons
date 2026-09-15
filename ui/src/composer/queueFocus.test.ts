// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { cycleQueueFocus, reconcileQueueFocus } from './queueFocus';

// 🎯T657 slice 2b: Alt+↑ enters the queue at the next-to-send item (bottom of
// the strip) and climbs; Alt+↓ descends and leaves to the composer.
const items = [
  { id: 'q1', text: 'next to send' },
  { id: 'q2', text: 'after that' },
  { id: 'q3', text: 'newest' },
];

describe('cycleQueueFocus (T657)', () => {
  it('empty queue is never handled — the caller falls through to history recall', () => {
    expect(cycleQueueFocus(null, [], -1)).toEqual({ handled: false, focusedId: null });
    expect(cycleQueueFocus('q1', [], 1)).toEqual({ handled: false, focusedId: null });
  });

  it('Alt+↑ from the composer lands on the next-to-send item, then climbs, then holds at the top', () => {
    expect(cycleQueueFocus(null, items, -1)).toEqual({ handled: true, focusedId: 'q1' });
    expect(cycleQueueFocus('q1', items, -1)).toEqual({ handled: true, focusedId: 'q2' });
    expect(cycleQueueFocus('q2', items, -1)).toEqual({ handled: true, focusedId: 'q3' });
    expect(cycleQueueFocus('q3', items, -1)).toEqual({ handled: true, focusedId: 'q3' });
  });

  it('Alt+↓ descends and returns to the composer from the bottom item; from the composer it is not handled', () => {
    expect(cycleQueueFocus('q3', items, 1)).toEqual({ handled: true, focusedId: 'q2' });
    expect(cycleQueueFocus('q2', items, 1)).toEqual({ handled: true, focusedId: 'q1' });
    expect(cycleQueueFocus('q1', items, 1)).toEqual({ handled: true, focusedId: null });
    expect(cycleQueueFocus(null, items, 1)).toEqual({ handled: false, focusedId: null });
  });

  it('a stale focused id behaves like the composer; reconcile drops it', () => {
    expect(cycleQueueFocus('gone', items, -1)).toEqual({ handled: true, focusedId: 'q1' });
    expect(reconcileQueueFocus('gone', items)).toBeNull();
    expect(reconcileQueueFocus('q2', items)).toBe('q2');
    expect(reconcileQueueFocus(null, items)).toBeNull();
  });
});
