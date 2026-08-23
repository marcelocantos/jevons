// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** PageUp / PageDown scroll the transcript by ~0.8 viewport (T336). */

/** Vanilla `msgs` is #messages; compact sidebar is #agent-inspect-body. */
export const TRANSCRIPT_SCROLL_SEL = '#messages, #agent-inspect-body';

export function transcriptPane(from: Element | null): HTMLElement | null {
  if (from) {
    const wrap = from.closest('#chat-pane, #agent-inspect, .conversation-widget');
    const local = wrap?.querySelector(TRANSCRIPT_SCROLL_SEL);
    if (local instanceof HTMLElement) return local;
  }
  const main = document.querySelector(TRANSCRIPT_SCROLL_SEL);
  return main instanceof HTMLElement ? main : null;
}

export function pageScrollDelta(key: string, clientHeight: number): number {
  const step = Math.round((clientHeight || 0) * 0.8);
  if (key === 'PageUp') return -step;
  if (key === 'PageDown') return step;
  return 0;
}

/** Near the oldest loaded row — request another page (T537.2 / T336). */
export const PAGE_OLDER_PX = 48;

/** Scroll the live transcript pane. PageUp leaves follow-the-end (T336 / T537.2.8). */
export function applyTranscriptPageKey(key: string, from: Element | null): boolean {
  const pane = transcriptPane(from);
  if (!pane) return false;
  const dy = pageScrollDelta(key, pane.clientHeight);
  if (!dy) return false;
  if (key === 'PageUp') pane.dispatchEvent(new Event('jevons-leave-track'));
  pane.scrollBy(0, dy);
  if (key === 'PageUp' && pane.scrollTop < PAGE_OLDER_PX) {
    pane.dispatchEvent(new Event('jevons-page-older'));
  }
  return true;
}
