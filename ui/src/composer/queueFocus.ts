// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Queue-focus cycling for Alt+↑ / Alt+↓ (🎯T657 slice 2b).
 *
 * The strip paints the next-to-send item at the bottom, nearest the
 * composer, so Alt+↑ from the composer lands on that item and each further
 * Alt+↑ climbs the strip; Alt+↓ descends and steps back to the composer from
 * the bottom item. The keys only fall through to transcript history recall
 * when the queue is empty — that is the caller's branch, not this helper's.
 */

import type { QueueItem } from './sendQueue';

export type QueueFocusResult = { handled: boolean; focusedId: string | null };

/** dir −1 = Alt+↑ (away from the composer), +1 = Alt+↓ (toward it). */
export function cycleQueueFocus(
  focusedId: string | null | undefined,
  items: readonly QueueItem[],
  dir: -1 | 1,
): QueueFocusResult {
  const n = items.length;
  if (!n) return { handled: false, focusedId: null };
  const cur = focusedId ? items.findIndex((it) => it.id === focusedId) : -1;
  if (cur < 0) {
    // Composer (or a stale id): Alt+↑ enters at the next-to-send item.
    return dir < 0 ? { handled: true, focusedId: items[0].id } : { handled: false, focusedId: null };
  }
  const next = cur - dir;
  if (next < 0) return { handled: true, focusedId: null };
  if (next >= n) return { handled: true, focusedId: items[cur].id };
  return { handled: true, focusedId: items[next].id };
}

/** A focused id that no longer names a queued item is dropped (drained or removed). */
export function reconcileQueueFocus(focusedId: string | null, items: readonly QueueItem[]): string | null {
  if (!focusedId) return null;
  return items.some((it) => it.id === focusedId) ? focusedId : null;
}
