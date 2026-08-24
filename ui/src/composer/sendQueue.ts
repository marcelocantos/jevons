// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Owner-chat send-queue persistence (🎯T154).
 * Port of web/scripts/send_queue.js load/save — text-only bodies so a
 * full reload restores FIFO order; soft reconnect must not reset it.
 */

export const STORAGE_KEY = 'jevons-send-queue-v1';

export type QueueItem = { id: string; text: string };
export type QueueState = { items: QueueItem[]; nextId: number };

export function emptyState(): QueueState {
  return { items: [], nextId: 1 };
}

export type SendDecision =
  | { action: 'noop' }
  | { action: 'enqueue'; text: string; reason?: 'busy' | 'offline' }
  | { action: 'send'; text: string; interrupt: boolean };

/** Pure send decision (T113 / T228). Plain Enter while busy enqueues. */
export function decideSend(opts: {
  busy?: boolean;
  interrupt?: boolean;
  text?: string;
  hasImages?: boolean;
  wireOpen?: boolean;
}): SendDecision {
  const busy = !!opts.busy;
  const interrupt = !!opts.interrupt;
  const raw = opts.text == null ? '' : String(opts.text);
  const hasPayload = raw.trim().length > 0 || !!opts.hasImages;
  if (!hasPayload) return { action: 'noop' };
  const wireOpen = opts.wireOpen === undefined ? true : !!opts.wireOpen;
  if (!wireOpen) {
    return { action: 'enqueue', text: raw, reason: 'offline' };
  }
  if (busy && !interrupt) {
    return { action: 'enqueue', text: raw, reason: 'busy' };
  }
  return { action: 'send', text: raw, interrupt: busy && interrupt };
}

export function shouldInterrupt(busy: boolean, interruptChord: boolean): boolean {
  return !!(busy && interruptChord);
}

export function shouldEnqueue(busy: boolean, interruptChord: boolean): boolean {
  return !!(busy && !interruptChord);
}

export function enqueue(state: QueueState | null | undefined, text: string): QueueState {
  const s = state || emptyState();
  const id = 'q' + s.nextId;
  return {
    items: s.items.concat([{ id, text: String(text ?? '') }]),
    nextId: (s.nextId || 1) + 1,
  };
}

export function shiftNext(state: QueueState | null | undefined): { item: QueueItem | null; state: QueueState } {
  const s = state || emptyState();
  if (!s.items.length) return { item: null, state: s };
  return { item: s.items[0], state: { items: s.items.slice(1), nextId: s.nextId } };
}

export function serialize(state: QueueState | null | undefined): string {
  const s = state || emptyState();
  const items = (s.items || [])
    .map((it) => ({
      id: String(it?.id != null ? it.id : ''),
      text: String(it?.text != null ? it.text : ''),
    }))
    .filter((it) => it.id.length > 0);
  let nextId = Math.max(1, Number(s.nextId) || 1);
  for (const it of items) {
    const m = /^q(\d+)$/.exec(it.id);
    if (m) {
      const n = parseInt(m[1], 10);
      if (n >= nextId) nextId = n + 1;
    }
  }
  return JSON.stringify({ items, nextId });
}

export function deserialize(raw: unknown): QueueState {
  if (!raw) return emptyState();
  try {
    const parsed = typeof raw === 'string' ? JSON.parse(raw) : raw;
    if (!parsed || typeof parsed !== 'object') return emptyState();
    const rec = parsed as { items?: unknown; nextId?: unknown };
    const items: QueueItem[] = [];
    let maxQ = 0;
    if (Array.isArray(rec.items)) {
      for (const it of rec.items) {
        if (!it || typeof it !== 'object') continue;
        const row = it as { id?: unknown; text?: unknown };
        if (row.id == null || row.id === '') continue;
        const id = String(row.id);
        items.push({ id, text: String(row.text == null ? '' : row.text) });
        const m = /^q(\d+)$/.exec(id);
        if (m) {
          const n = parseInt(m[1], 10);
          if (n > maxQ) maxQ = n;
        }
      }
    }
    let nextId = Math.max(1, Number(rec.nextId) || 1);
    if (nextId <= maxQ) nextId = maxQ + 1;
    return { items, nextId };
  } catch {
    return emptyState();
  }
}

export type StorageLike = {
  getItem: (k: string) => string | null;
  setItem: (k: string, v: string) => void;
};

export function load(storage: StorageLike | null | undefined): QueueState {
  if (!storage || typeof storage.getItem !== 'function') return emptyState();
  try {
    return deserialize(storage.getItem(STORAGE_KEY));
  } catch {
    return emptyState();
  }
}

export function save(storage: StorageLike | null | undefined, state: QueueState): void {
  if (!storage || typeof storage.setItem !== 'function') return;
  try {
    storage.setItem(STORAGE_KEY, serialize(state));
  } catch {
    /* quota / private mode — in-memory state still works */
  }
}
