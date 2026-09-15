// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import './ensureLocalStorage';
import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { queueStorageKey, useSendQueue } from './useSendQueue';
import { STORAGE_KEY } from './sendQueue';

// 🎯T657 slice 2a: the React send queue is real — busy enqueues, idle drains,
// steer/interrupt bypass, offline holds, reload restores (🎯T113 / T154 / T228).

beforeEach(() => {
  localStorage.clear();
});

function mount(name = 'jevons', init = { busy: true, wireOpen: true }) {
  const sendNow = vi.fn();
  const hook = renderHook(
    (p: { busy: boolean; wireOpen: boolean }) => useSendQueue(name, { busy: p.busy, wireOpen: p.wireOpen, sendNow }),
    { initialProps: init },
  );
  return { ...hook, sendNow };
}

describe('useSendQueue (T657 / T113)', () => {
  it('plain submit while busy enqueues, persists, and does not send', () => {
    const { result, sendNow } = mount();
    let outcome: { queued: boolean } | undefined;
    act(() => {
      outcome = result.current.submit('follow up', 'submit');
    });
    expect(outcome).toEqual({ queued: true });
    expect(sendNow).not.toHaveBeenCalled();
    expect(result.current.items.map((i) => i.text)).toEqual(['follow up']);
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').items).toEqual([{ id: 'q1', text: 'follow up' }]);
  });

  it('drains the head on the next idle observation, FIFO', () => {
    const { result, rerender, sendNow } = mount();
    act(() => {
      result.current.submit('first', 'submit');
      result.current.submit('second', 'submit');
    });
    expect(result.current.items).toHaveLength(2);
    rerender({ busy: false, wireOpen: true });
    // The hook drains heads in order for as long as the seat stays idle; the
    // one-per-turn pacing comes from the optimistic received phase the
    // overseer paints after each send (pinned in AgentInteraction.queue.test).
    expect(sendNow.mock.calls).toEqual([['first', 'submit'], ['second', 'submit']]);
    expect(result.current.items).toEqual([]);
    // Busy again: a new follow-up is held, not sent.
    rerender({ busy: true, wireOpen: true });
    act(() => {
      result.current.submit('third', 'submit');
    });
    expect(sendNow).toHaveBeenCalledTimes(2);
    expect(result.current.items.map((i) => i.text)).toEqual(['third']);
  });

  it('steer and interrupt bypass the queue while busy; idle steer sends too', () => {
    const { result, rerender, sendNow } = mount();
    act(() => {
      expect(result.current.submit('go left', 'steer')).toEqual({ queued: false });
      expect(result.current.submit('stop that', 'interrupt')).toEqual({ queued: false });
    });
    expect(sendNow).toHaveBeenNthCalledWith(1, 'go left', 'steer');
    expect(sendNow).toHaveBeenNthCalledWith(2, 'stop that', 'interrupt');
    expect(result.current.items).toEqual([]);
    rerender({ busy: false, wireOpen: true });
    act(() => {
      result.current.submit('idle steer', 'steer');
    });
    expect(sendNow).toHaveBeenLastCalledWith('idle steer', 'steer');
  });

  it('offline enqueues visibly and drains when the wire reopens (T228)', () => {
    const { result, rerender, sendNow } = mount('jevons', { busy: false, wireOpen: false });
    act(() => {
      expect(result.current.submit('while offline', 'submit')).toEqual({ queued: true });
    });
    expect(sendNow).not.toHaveBeenCalled();
    expect(result.current.items.map((i) => i.text)).toEqual(['while offline']);
    rerender({ busy: false, wireOpen: true });
    expect(sendNow).toHaveBeenCalledWith('while offline', 'submit');
    expect(result.current.items).toEqual([]);
  });

  it('a reload restores FIFO order from storage; other seats keep their own key (T154)', () => {
    const first = mount();
    act(() => {
      first.result.current.submit('a', 'submit');
      first.result.current.submit('b', 'submit');
    });
    first.unmount();
    const again = mount();
    expect(again.result.current.items.map((i) => i.text)).toEqual(['a', 'b']);
    expect(queueStorageKey('jevons')).toBe(STORAGE_KEY);
    expect(queueStorageKey('jv-t1')).toBe(STORAGE_KEY + ':jv-t1');
    const other = mount('jv-t1');
    expect(other.result.current.items).toEqual([]);
  });

  it('sendItem sends a chosen item with the given mode and removes it; remove drops it', () => {
    const { result, sendNow } = mount();
    act(() => {
      result.current.submit('one', 'submit');
      result.current.submit('two', 'submit');
      result.current.submit('three', 'submit');
    });
    act(() => {
      result.current.sendItem('q2', 'steer');
    });
    expect(sendNow).toHaveBeenCalledWith('two', 'steer');
    expect(result.current.items.map((i) => i.id)).toEqual(['q1', 'q3']);
    act(() => {
      result.current.remove('q1');
    });
    expect(result.current.items.map((i) => i.id)).toEqual(['q3']);
    act(() => {
      expect(result.current.submit('   ', 'submit')).toEqual({ queued: false });
      expect(result.current.submit('explicit', 'queue')).toEqual({ queued: true });
    });
    expect(result.current.items.map((i) => i.text)).toEqual(['three', 'explicit']);
  });
});
