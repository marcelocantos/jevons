// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Owner delivery intent for one send (🎯T657). Mirrors the claudia broker
 * `send.mode` enum (../claudia/docs/design/steer-interrupt-turn-api.md):
 *
 *   submit    → start a turn when idle; the host enqueues when busy
 *   steer     → fold the text into the running turn
 *   interrupt → cancel the open turn, then submit
 *   queue     → explicit host-side enqueue; never a wire send
 */
export type DeliveryMode = 'submit' | 'steer' | 'interrupt' | 'queue';

export const DELIVERY_MODES: readonly DeliveryMode[] = ['submit', 'steer', 'interrupt', 'queue'];

export type SendOpts = { mode?: DeliveryMode };

/** Normalise a send option bag; the legacy `interrupt: true` flag is an alias for mode=interrupt. */
export function deliveryModeOf(opts?: { mode?: DeliveryMode; interrupt?: boolean } | null): DeliveryMode {
  if (opts?.mode && DELIVERY_MODES.includes(opts.mode)) return opts.mode;
  if (opts?.interrupt) return 'interrupt';
  return 'submit';
}
