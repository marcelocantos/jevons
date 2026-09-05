// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import type { PlanSnapshot } from './tickerGroups';

/** Holding a reading preserves the same schema consumed by the ticker. */
export type PlanSnap = PlanSnapshot;

function hasNumericRemaining(snap: PlanSnap | undefined): boolean {
  if (!snap) return false;
  const wins = Array.isArray(snap.windows) ? snap.windows : [];
  for (const w of wins) {
    if (w && typeof w === 'object' && typeof (w as { remaining_percent?: unknown }).remaining_percent === 'number') {
      return true;
    }
  }
  for (const b of snap.backends || []) {
    for (const w of b.windows || []) {
      if (w && typeof w === 'object' && typeof (w as { remaining_percent?: unknown }).remaining_percent === 'number') {
        return true;
      }
    }
  }
  return false;
}

/**
 * Keep the last paint-able reading. A pending long-poll timeout or a
 * refetch that has not arrived yet must not blank the ticker.
 */
export function holdLastPlanSnapshot(
  prev: PlanSnap | undefined,
  next: PlanSnap | undefined,
): PlanSnap | undefined {
  if (hasNumericRemaining(next)) return next;
  if (hasNumericRemaining(prev)) return prev;
  return next ?? prev;
}
