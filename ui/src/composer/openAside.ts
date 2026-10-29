// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Explicit aside prefixes (🎯T95 / 🎯T247 / 🎯T250 / 🎯T368).
 * `target:` / `aside:` / `capture:` / `idea:` open an aside immediately —
 * they must not paint as a main #messages .msg.user.
 */

import { parsePrefixAfterImages } from './images';

export const ASIDE_PREFIX_COMMANDS = new Set(['target', 'aside', 'capture', 'idea']);

export type AsideKind = 'target' | 'side' | 'capture';

export type AsideOpenPlan = {
  command: string;
  kind: AsideKind;
  id: string;
  title: string;
  body: string;
  images: string;
  /** Opening text delivered on create (🎯T263). Empty = register-only. */
  text?: string;
};

export function mintAsideId(now = Date.now, random = Math.random): string {
  const t = now().toString(36);
  const r = random().toString(36).slice(2, 8) || 'x';
  return 'att-' + t + '-' + r;
}

export function asideKindOf(command: string): AsideKind {
  if (command === 'target') return 'target';
  if (command === 'capture' || command === 'idea') return 'capture';
  return 'side';
}

export function planExplicitAside(draft: string, mint: () => string = mintAsideId): AsideOpenPlan | null {
  const p = parsePrefixAfterImages(draft);
  if (!p.command || !ASIDE_PREFIX_COMMANDS.has(p.command)) return null;
  const body = String(p.body || '').replace(/^\s+/, '');
  const title = body.split(/\r?\n/, 1)[0].trim().slice(0, 120) || p.command;
  const kind = asideKindOf(p.command);
  const id = mint();
  // 🎯T263: freeform aside: delivers the opening in the same request.
  // capture/target (and idea) stay register-only.
  const text = p.command === 'aside' ? [p.images, body].filter(Boolean).join('\n') : undefined;
  return { command: p.command, kind, id, title, body, images: p.images, text };
}

export type CreateAsideBody = {
  id: string;
  title: string;
  kind: AsideKind;
  text?: string;
};

export function createAsideRequest(plan: AsideOpenPlan): CreateAsideBody {
  const body: CreateAsideBody = { id: plan.id, title: plan.title, kind: plan.kind };
  if (plan.text) body.text = plan.text;
  return body;
}

export async function postCreateAside(
  plan: AsideOpenPlan,
  fetchImpl: typeof fetch = fetch,
): Promise<{ name: string }> {
  const res = await fetchImpl('/api/asides', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(createAsideRequest(plan)),
  });
  if (!res.ok) {
    const msg = await res.text().catch(() => '');
    throw new Error(msg || 'aside create HTTP ' + res.status);
  }
  const out = (await res.json().catch(() => ({}))) as { name?: string };
  return { name: String(out.name || plan.id) };
}
