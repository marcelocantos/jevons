// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { renderUserTextWithImages } from '../composer/images';
import { linkifyTargetText } from '../frontier/targetHotspot';
import { parseAssistantMarkdown } from './markdown';

/** Wire provenance (🎯T381). Unmarked is the owner — verbatim is the safe default. */
export type TurnOrigin = 'owner' | 'agent';

export { linkifyTargetText } from '../frontier/targetHotspot';

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

/**
 * User-role body paint. Keys on provenance, never on what the body looks like
 * (🎯T381). Same markdown-shaped source must stay literal when the owner typed it.
 */
export function paintUserHTML(text: string, origin: TurnOrigin): string {
  if (origin === 'agent') return parseAssistantMarkdown(text);
  return linkifyTargetText(renderUserTextWithImages(text));
}
