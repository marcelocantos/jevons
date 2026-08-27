// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * 🎯T233: harness injects (system-reminder / standing brief / event push /
 * daemon restart) become compact ⋯ nuggets with hover detail — not owner
 * bubbles. Port of vanilla classifyInspectUserLine (inject half). Leaf.
 */
export type InjectKind = 'system-reminder' | 'standing-brief' | 'event' | 'daemon';

export type InjectNugget = { injectKind: InjectKind; label: string; detail: string };

function extractSystemReminderBody(s: string): string {
  const m = s.match(/<system-reminder(?:\s[^>]*)?>\s*([\s\S]*?)\s*<\/system-reminder>/i);
  if (m && m[1].trim()) return m[1].trim();
  return s.replace(/<\/?system-reminder(?:\s[^>]*)?>/gi, '').trim() || s;
}

/** Returns null for owner prose (not an inject). */
export function classifyInjectUserText(text: string): InjectNugget | null {
  const raw = String(text ?? '');
  const trimmed = raw.replace(/^\s+/, '');
  if (/<system-reminder[\s>]/i.test(raw) || /<\/system-reminder>/i.test(raw)) {
    return { injectKind: 'system-reminder', label: '⋯ system', detail: extractSystemReminderBody(raw) };
  }
  if (trimmed.indexOf('[Jevons fleet standing brief') === 0 || /Jevons fleet standing brief/.test(raw)) {
    return { injectKind: 'standing-brief', label: '⋯ brief', detail: raw.trim() };
  }
  const em = trimmed.match(/^\[event:\s*([^\]]+)\]/i);
  if (em) {
    const src = String(em[1]).trim() || 'event';
    return { injectKind: 'event', label: '⋯ ' + src, detail: raw.trim() };
  }
  if (trimmed.indexOf('[Daemon restart') === 0) {
    return { injectKind: 'daemon', label: '⋯ system', detail: raw.trim() };
  }
  return null;
}
