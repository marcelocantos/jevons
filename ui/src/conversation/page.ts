// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** When to request the next older page (T537.2). One decision, used by AgentTranscript. */
export const PAGE_TOP_PX = 48;

export function shouldRequestPage(o: {
  scrollTop: number;
  older?: number;
  inFlight: boolean;
}): boolean {
  if (o.inFlight) return false;
  if (!o.older) return false;
  return o.scrollTop < PAGE_TOP_PX;
}
