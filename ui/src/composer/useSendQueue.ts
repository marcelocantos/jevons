// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Owner-chat send queue, wired (🎯T657 slice 2a; policy 🎯T113 / 🎯T154 / 🎯T228).
 *
 * Plain Enter while the seat is busy enqueues instead of sending; the head of
 * the queue drains on the next idle. Steer and interrupt bypass the queue —
 * they are exactly the chords for a busy seat. Offline enqueues visibly
 * (never clear-and-drop). State persists per agent so a full reload keeps
 * FIFO order.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import type { DeliveryMode } from './deliveryMode';
import {
  STORAGE_KEY,
  decideSend,
  enqueue,
  load,
  save,
  shiftNext,
  takeById,
  type QueueItem,
  type QueueState,
} from './sendQueue';

/** The overseer keeps the 🎯T154 key; other seats get their own. */
export function queueStorageKey(name: string): string {
  return name === 'jevons' ? STORAGE_KEY : `${STORAGE_KEY}:${name}`;
}

function browserStorage(): Storage | null {
  try {
    return typeof localStorage !== 'undefined' ? localStorage : null;
  } catch {
    return null;
  }
}

export type SendNow = (text: string, mode: DeliveryMode) => void;

export type SendQueueApi = {
  items: QueueItem[];
  /** Route one composer send: `{ queued: true }` when the text was held instead of sent. */
  submit: (text: string, mode: DeliveryMode) => { queued: boolean };
  remove: (id: string) => void;
  /** Send a queued item now with the given mode, removing it from the queue. */
  sendItem: (id: string, mode: DeliveryMode) => void;
};

export function useSendQueue(
  name: string,
  opts: { busy: boolean; wireOpen: boolean; sendNow: SendNow },
): SendQueueApi {
  const key = queueStorageKey(name);
  const [state, setState] = useState<QueueState>(() => load(browserStorage(), key));
  const loadedKey = useRef(key);
  const sendNowRef = useRef(opts.sendNow);
  sendNowRef.current = opts.sendNow;

  // A selected-agent change swaps the queue for that agent's own.
  useEffect(() => {
    if (loadedKey.current === key) return;
    loadedKey.current = key;
    setState(load(browserStorage(), key));
  }, [key]);

  useEffect(() => {
    if (loadedKey.current !== key) return;
    save(browserStorage(), state, key);
  }, [state, key]);

  // FIFO drain: one head per idle observation. A submit to the overseer
  // paints an optimistic "received" phase, so busy flips true again before
  // the next head could race it (🎯T555.2).
  const { busy, wireOpen } = opts;
  useEffect(() => {
    if (busy || !wireOpen || !state.items.length) return;
    const { item, state: next } = shiftNext(state);
    if (!item) return;
    setState(next);
    sendNowRef.current(item.text, 'submit');
  }, [busy, wireOpen, state]);

  const submit = useCallback(
    (text: string, mode: DeliveryMode): { queued: boolean } => {
      if (mode === 'queue') {
        const raw = String(text ?? '');
        if (!raw.trim()) return { queued: false };
        setState((s) => enqueue(s, raw));
        return { queued: true };
      }
      const d = decideSend({ busy, interrupt: mode === 'steer' || mode === 'interrupt', text, wireOpen });
      if (d.action === 'noop') return { queued: false };
      if (d.action === 'enqueue') {
        setState((s) => enqueue(s, d.text));
        return { queued: true };
      }
      sendNowRef.current(d.text, mode);
      return { queued: false };
    },
    [busy, wireOpen],
  );

  const remove = useCallback((id: string) => setState((s) => takeById(s, id).state), []);

  const sendItem = useCallback((id: string, mode: DeliveryMode) => {
    setState((s) => {
      const { item, state: next } = takeById(s, id);
      if (!item) return s;
      sendNowRef.current(item.text, mode);
      return next;
    });
  }, []);

  return { items: state.items, submit, remove, sendItem };
}
