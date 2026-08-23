// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** When to request the next older page (T537.2). One decision, used by AgentTranscript. */
export const PAGE_TOP_PX = 48;

export function shouldRequestPage(o: {
  scrollTop: number;
  older?: number;
  inFlight: boolean;
  following?: boolean;
  scrollHeight?: number;
  clientHeight?: number;
}): boolean {
  if (o.inFlight) return false;
  if (!o.older) return false;
  // Live tail: scrollTop is 0 when the window is shorter than the pane.
  // That is the bottom, not history-top — paging here freezes mux follow.
  if (o.following) return false;
  const sh = Number(o.scrollHeight) || 0;
  const ch = Number(o.clientHeight) || 0;
  if (sh > 0 && ch > 0 && sh <= ch) return false;
  return o.scrollTop < PAGE_TOP_PX;
}
