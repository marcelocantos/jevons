// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** PageUp / PageDown scroll the transcript by ~0.8 viewport (T336). */

export function pageScrollDelta(key: string, clientHeight: number): number {
  const step = Math.round((clientHeight || 0) * 0.8);
  if (key === 'PageUp') return -step;
  if (key === 'PageDown') return step;
  return 0;
}
