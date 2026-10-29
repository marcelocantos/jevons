// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Live Claude TUI preview wire (🎯T584). Never journaled. */

export const PROVISIONAL_ASSISTANT = 'provisional_assistant';
export const TUI_PREVIEW_FAULT = 'tui_preview_fault';

function rec(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' ? (v as Record<string, unknown>) : {};
}

export function isProvisionalWire(body: unknown): boolean {
  const t = rec(body).type;
  return t === PROVISIONAL_ASSISTANT || t === TUI_PREVIEW_FAULT;
}

export function provisionalFromWire(body: unknown): { streamId: string; text: string } | null {
  const o = rec(body);
  if (o.type !== PROVISIONAL_ASSISTANT) return null;
  const sid = String(o.stream_id ?? o.streamId ?? '').trim();
  if (!sid) return null;
  return { streamId: sid, text: typeof o.text === 'string' ? o.text : '' };
}

export type ProvisionalMap = Record<string, string>;

export function applyProvisionalWire(
  provisionalByStream: ProvisionalMap,
  body: unknown,
): { provisionalByStream: ProvisionalMap; faultFrame?: unknown } {
  const o = rec(body);
  if (o.type === TUI_PREVIEW_FAULT) {
    const text = String(o.text ?? '').trim();
    if (!text) return { provisionalByStream };
    return { provisionalByStream, faultFrame: { type: TUI_PREVIEW_FAULT, text } };
  }
  const p = provisionalFromWire(body);
  if (!p) return { provisionalByStream };
  const next = { ...provisionalByStream };
  if (!p.text.trim()) {
    delete next[p.streamId];
  } else {
    next[p.streamId] = p.text;
  }
  return { provisionalByStream: next };
}
