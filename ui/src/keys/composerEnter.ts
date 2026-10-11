// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Enter-chord policy (T113 / T132 / T235 / T241 / T644 / T657 / T1018).
 * Extracted so oracles can assert without mounting UserRequest.
 *
 *   plain Enter     → send on desktop (enqueue while busy is decideSend; never interrupt);
 *                     noop on an empty composer so a busy turn keeps focus.
 *                     🎯T1018/T1057: on any touchscreen, plain Enter is
 *                     newline, including with a trackpad or hardware keyboard.
 *                     Send button remains the way to send; desktop without
 *                     touch retains Enter-to-send.
 *   Cmd+Enter       → steer (fold the draft into the running turn; plain send when idle)
 *   Cmd+Shift+Enter → interrupt (cancel in-flight; then send if the draft is non-empty)
 *   Ctrl+Enter      → not hooked (Firefox steals it for the context menu)
 *   Alt+Enter       → force_send | send_queue_now | noop  (never pop_last)
 *   Shift+Enter     → newline
 *
 * Until 🎯T657 Cmd+Enter was interrupt (🎯T644); the claudia steer/interrupt
 * design inverts that so the destructive chord needs the extra modifier.
 *
 * 🎯T1018/T1057: the touchscreen carve-out applies ONLY to naked plain Enter.
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
 * Touch capability, not primary-pointer type: an iPad/Pixel Fold with a
 * keyboard and trackpad may report `(pointer: fine)` yet remains a touchscreen.
 * maxTouchPoints reports actual touch contacts independently of pointer type;
 * a missing/zero value is treated as no touchscreen, never inferred from an
 * app-specific JS bridge or user agent.
 */
export function isTouchscreenDevice(source?: Pick<Navigator, 'maxTouchPoints'>): boolean {
  const nav = source ?? (typeof navigator !== 'undefined' ? navigator : undefined);
  return typeof nav?.maxTouchPoints === 'number' && nav.maxTouchPoints > 0;
}

export function isEnterKey(key: string | null | undefined, opts?: { code?: string }): boolean {
  if (key === 'Enter') return true;
  const code = opts && opts.code != null ? String(opts.code) : '';
  return code === 'Enter' || code === 'NumpadEnter';
}

export function classifyEnterAction(
  key: string,
  mods: { metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean; shiftKey?: boolean; code?: string },
  opts?: { composerEmpty?: boolean; code?: string; queueLen?: number; touchscreen?: boolean },
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
  // 🎯T1018/T1057: naked plain Enter only. Every chord above is unconditional on device.
  if (o.touchscreen) return 'newline';
  if (o.composerEmpty) return 'noop';
  return 'send';
}
