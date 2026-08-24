// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { renderUserTextWithImages } from '../composer/images';
import { parseAssistantMarkdown } from './markdown';

/** Wire provenance (🎯T381). Unmarked is the owner — verbatim is the safe default. */
export type TurnOrigin = 'owner' | 'agent';

const TARGET_TOKEN_RE = /(?:🎯\s*)?(T\d+(?:\.\d+)*)\b/g;

/**
 * Classify a wire event by who spoke it. Anything unmarked is the owner so
 * an old journal line can never reinterpret owner asterisks as formatting.
 */
export function turnOriginOf(event: unknown): TurnOrigin {
  if (!event || typeof event !== 'object') return 'owner';
  const rec = event as Record<string, unknown>;
  const raw = rec.turn_origin !== undefined ? rec.turn_origin
    : rec.turnOrigin !== undefined ? rec.turnOrigin
    : rec.origin;
  if (typeof raw !== 'string') return 'owner';
  return raw.trim().toLowerCase() === 'agent' ? 'agent' : 'owner';
}

/** Assistant always paints markdown; a user-role bubble does only when agent-origin. */
export function bubblePaintsMarkdown(role: string, origin: TurnOrigin): boolean {
  if (role === 'jevons' || role === 'assistant') return true;
  if (role !== 'user') return false;
  return origin === 'agent';
}

export function userBubbleClass(origin: TurnOrigin): string {
  return origin === 'agent' ? 'agent-report' : '';
}

function escapeText(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function escapeAttr(s: string): string {
  return escapeText(s).replace(/"/g, '&quot;');
}

/** Owner / product HTML: 🎯T22 → .target-hotspot (vanilla T326 text path). */
export function linkifyTargetText(text: string): string {
  const s = String(text ?? '');
  if (!s) return s;
  TARGET_TOKEN_RE.lastIndex = 0;
  return s.replace(TARGET_TOKEN_RE, (full, id: string) => {
    const tid = String(id || '').replace(/^t/, 'T');
    if (!tid) return full;
    const label = '🎯' + tid;
    return (
      '<span class="target-hotspot target-hotspot-finger" data-target-id="' +
      escapeAttr(tid) +
      '" role="button" tabindex="0">' +
      escapeText(label) +
      '</span>'
    );
  });
}

/**
 * User-role body paint. Keys on provenance, never on what the body looks like
 * (🎯T381). Same markdown-shaped source must stay literal when the owner typed it.
 */
export function paintUserHTML(text: string, origin: TurnOrigin): string {
  if (origin === 'agent') return parseAssistantMarkdown(text);
  return linkifyTargetText(renderUserTextWithImages(text));
}
