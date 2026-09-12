// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Enter-chord policy (T113 / T132 / T235 / T241 / T644).
 * Extracted so oracles can assert without mounting UserRequest.
 *
 *   Cmd+Enter   → interrupt (cancel in-flight; then send if the draft is non-empty)
 *   Ctrl+Enter  → not hooked (Firefox steals it for the context menu)
 *   Alt+Enter   → force_send | send_queue_now | noop  (never pop_last)
 *   Shift+Enter → newline
 *   plain Enter → send (enqueue while busy is decideSend; never interrupt)
 */

export type EnterAction = 'newline' | 'send' | 'interrupt' | 'force_send' | 'send_queue_now' | 'noop';

export function isEnterKey(key: string | null | undefined, opts?: { code?: string }): boolean {
  if (key === 'Enter') return true;
  const code = opts && opts.code != null ? String(opts.code) : '';
  return code === 'Enter' || code === 'NumpadEnter';
}

export function classifyEnterAction(
  key: string,
  mods: { metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean; shiftKey?: boolean; code?: string },
  opts?: { composerEmpty?: boolean; code?: string; queueLen?: number },
): EnterAction | null {
  const m = mods || {};
  const o = opts || {};
  const code = o.code != null ? o.code : m.code;
  if (!isEnterKey(key, { code })) return null;
  if (m.shiftKey) return 'newline';
  // Firefox slurps Ctrl+Enter for the context menu — do not preventDefault.
  if (m.ctrlKey && !m.metaKey) return null;
  if (m.metaKey) return 'interrupt';
  if (m.altKey) {
    if (!o.composerEmpty) return 'force_send';
    const qLen = o.queueLen || 0;
    if (qLen > 0) return 'send_queue_now';
    return 'noop';
  }
  return 'send';
}
