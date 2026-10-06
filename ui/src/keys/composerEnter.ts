// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Enter-chord policy (T113 / T132 / T235 / T241 / T644 / T657 / T1018).
 * Extracted so oracles can assert without mounting UserRequest.
 *
 *   plain Enter     → send (enqueue while busy is decideSend; never interrupt);
 *                     noop on an empty composer so a busy turn keeps focus.
 *                     🎯T1018: on a touch-primary device (no easy Shift key),
 *                     plain Enter is 'newline' instead — the Send button
 *                     remains the way to send on those devices.
 *   Cmd+Enter       → steer (fold the draft into the running turn; plain send when idle)
 *   Cmd+Shift+Enter → interrupt (cancel in-flight; then send if the draft is non-empty)
 *   Ctrl+Enter      → not hooked (Firefox steals it for the context menu)
 *   Alt+Enter       → force_send | send_queue_now | noop  (never pop_last)
 *   Shift+Enter     → newline
 *
 * Until 🎯T657 Cmd+Enter was interrupt (🎯T644); the claudia steer/interrupt
 * design inverts that so the destructive chord needs the extra modifier.
 *
 * 🎯T1018: the touch-primary carve-out applies ONLY to naked plain Enter.
 * Every modifier chord (Cmd+Enter, Cmd+Shift+Enter, Alt+Enter, Shift+Enter)
 * behaves identically on every device — this is deliberately a single
 * narrow branch, not a parallel touch keymap.
 */

import type { DeliveryMode } from '../composer/deliveryMode';

/**
 * 🎯T562.7: delivery mode for Alt+Enter force-send. 'interrupt' cancels the
 * running turn (owner question open: interrupt vs steer). One line to change.
 */
export const FORCE_SEND_MODE: DeliveryMode = 'interrupt';

export type EnterAction = 'newline' | 'send' | 'steer' | 'interrupt' | 'force_send' | 'send_queue_now' | 'noop';

/**
 * 🎯T1018: touch-primary device detection, following the systemAppearance()
 * pattern in theme.ts — an injectable `media` param so tests can supply a
 * fake matchMedia instead of depending on jsdom/browser truth. Feature
 * detection via `(pointer: coarse)`, never user-agent sniffing: UA strings
 * are unreliable and explicitly to be avoided (per 🎯T1018 acceptance).
 *
 * `(pointer: coarse)` reflects the PRIMARY pointer, which is what we want:
 * a touch tablet with a connected mouse still reports coarse here unless
 * the OS considers the mouse primary, matching "no easy Shift key" intent.
 */
export function isTouchPrimaryDevice(media?: Pick<Window, 'matchMedia'>): boolean {
  const src = media ?? (typeof window !== 'undefined' ? window : undefined);
  if (!src || typeof src.matchMedia !== 'function') return false;
  return src.matchMedia('(pointer: coarse)').matches;
}

export function isEnterKey(key: string | null | undefined, opts?: { code?: string }): boolean {
  if (key === 'Enter') return true;
  const code = opts && opts.code != null ? String(opts.code) : '';
  return code === 'Enter' || code === 'NumpadEnter';
}

export function classifyEnterAction(
  key: string,
  mods: { metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean; shiftKey?: boolean; code?: string },
  opts?: { composerEmpty?: boolean; code?: string; queueLen?: number; touchPrimary?: boolean },
): EnterAction | null {
  const m = mods || {};
  const o = opts || {};
  const code = o.code != null ? o.code : m.code;
  if (!isEnterKey(key, { code })) return null;
  // Firefox slurps Ctrl+Enter for the context menu — do not preventDefault.
  if (m.ctrlKey && !m.metaKey) return null;
  if (m.metaKey) return m.shiftKey ? 'interrupt' : 'steer';
  if (m.shiftKey) return 'newline';
  if (m.altKey) {
    if (!o.composerEmpty) return 'force_send';
    const qLen = o.queueLen || 0;
    if (qLen > 0) return 'send_queue_now';
    return 'noop';
  }
  // 🎯T1018: naked plain Enter only. Every chord above is unconditional on device.
  if (o.touchPrimary) return 'newline';
  if (o.composerEmpty) return 'noop';
  return 'send';
}
