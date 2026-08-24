// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Owner-echo + inject / protocol-frame classifiers (🎯T362 / T504). Leaf: no stream/display import. */

function rec(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' ? (v as Record<string, unknown>) : {};
}

function messageOf(frame: unknown): Record<string, unknown> {
  return rec(rec(frame).message);
}

/** Strip journal echo markers so owner text matches the live bubble (🎯T537.1.2). */
export function normalizeOwnerEchoText(text: string): string {
  let t = String(text ?? '').trim();
  if (!t) return '';
  if (t.startsWith('[user]\n')) t = t.slice('[user]\n'.length).trim();
  else if (/^\[user\]\s+/.test(t)) t = t.replace(/^\[user\]\s+/, '').trim();
  for (let i = 0; i < 3; i++) {
    const m = t.match(/^\s*<user_query(?:\s[^>]*)?>\s*([\s\S]*?)\s*<\/user_query>\s*$/i);
    if (!m) break;
    t = String(m[1] || '').trim();
  }
  return t;
}

export function isProtocolControlFrameText(text: string): boolean {
  const t = text.trim();
  if (t.length < 2 || t[0] !== '{' || t[t.length - 1] !== '}') return false;
  try {
    const obj = JSON.parse(t) as { type?: unknown };
    return !!obj && typeof obj === 'object' && !Array.isArray(obj) && typeof obj.type === 'string' && obj.type.trim() !== '';
  } catch {
    return false;
  }
}

export function isNonBoundaryUserText(text: string): boolean {
  const raw = String(text ?? '');
  if (!raw.trim()) return false;
  if (isProtocolControlFrameText(raw)) return true;
  const display = normalizeOwnerEchoText(raw);
  const trimmed = display.replace(/^\s+/, '');
  if (/<system-reminder[\s>]/i.test(raw) || /<\/system-reminder>/i.test(raw)) return true;
  if (trimmed.indexOf('[Jevons fleet standing brief') === 0 || /Jevons fleet standing brief/.test(display)) return true;
  if (/^\[event:\s*[^\]]+\]/i.test(trimmed)) return true;
  if (trimmed.indexOf('[Daemon restart') === 0) return true;
  if (/^Background task\b/i.test(trimmed)) return true;
  return false;
}

export function userContentText(frame: unknown): string {
  const content = messageOf(frame).content ?? rec(frame).content ?? rec(frame).text;
  if (typeof content === 'string') return content;
  if (!Array.isArray(content)) return '';
  let out = '';
  for (const c of content) {
    const b = rec(c);
    if (b.type === 'text' && typeof b.text === 'string') out += b.text;
  }
  return out;
}

export function isUserLikeFrame(frame: unknown): boolean {
  return rec(frame).type === 'user' || messageOf(frame).role === 'user';
}

/** Real owner user seals open streams (🎯T504). T329 inject / protocol frames do not. */
export function isOwnerUserBarrierFrame(frame: unknown): boolean {
  if (!isUserLikeFrame(frame)) return false;
  const text = userContentText(frame);
  if (!String(text).trim()) return false;
  return !isNonBoundaryUserText(text);
}
