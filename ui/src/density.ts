// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Transcript chrome density. Same AgentInteraction; CSS params, not a fork. */

export type Density = 'comfortable' | 'compact';

export function normalizeDensity(d?: string | null): Density {
  return String(d == null ? '' : d).toLowerCase() === 'compact' ? 'compact' : 'comfortable';
}
