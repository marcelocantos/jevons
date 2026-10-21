// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Historical owner-chat rows that were agent-driven, not owner-typed (🎯T1129). */
export const AGENT_FORGED_OWNER_EVENT_IDS = new Set<string>([
  'e:137063',
]);

export function isAgentForgedOwnerEvent(eventId: string | undefined): boolean {
  const id = String(eventId || '').trim();
  return id !== '' && AGENT_FORGED_OWNER_EVENT_IDS.has(id);
}
