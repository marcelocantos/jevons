// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { isAgentForgedOwnerEvent } from './agentForgedOwnerTurns';
import type { DisplayRow } from './display';

function frameRec(frame: unknown): Record<string, unknown> {
  return frame && typeof frame === 'object' ? (frame as Record<string, unknown>) : {};
}

function wireEvent(frame: unknown): Record<string, unknown> {
  const r = frameRec(frame);
  return r.event != null ? frameRec(r.event) : r;
}

/** Index of the newest owner-authored user row, or -1. */
export function latestOwnerUserIndex(rows: DisplayRow[], frames?: unknown[]): number {
  if (frames && frames.length) {
    for (let fi = frames.length - 1; fi >= 0; fi--) {
      const ev = wireEvent(frames[fi]);
      if (ev.type !== 'user' || ev.turn_origin !== 'owner') continue;
      const id = String(frameRec(frames[fi]).id || '');
      if (isAgentForgedOwnerEvent(id)) continue;
      for (let i = rows.length - 1; i >= 0; i--) {
        const r = rows[i];
        if (r.kind !== 'user') continue;
        if (r.origin === 'owner' || (id && r.id === id)) return i;
      }
      return -1;
    }
  }
  for (let i = rows.length - 1; i >= 0; i--) {
    const r = rows[i];
    if (r.kind === 'user' && r.origin === 'owner') return i;
  }
  return -1;
}

/** Row index of the first painted reply after the newest owner turn, or -1. */
export function ownerReplyRowIndex(rows: DisplayRow[], frames?: unknown[]): number {
  const owner = latestOwnerUserIndex(rows, frames);
  if (owner < 0) return -1;
  for (let i = owner + 1; i < rows.length; i++) {
    const r = rows[i];
    if (r.kind === 'user') return -1;
    if (r.kind === 'assistant' || r.kind === 'steps') return i;
  }
  return -1;
}

/** True when the newest owner turn has a painted reply row after it (assistant or steps). */
export function ownerTurnAndReplyPainted(rows: DisplayRow[], frames?: unknown[]): boolean {
  return ownerReplyRowIndex(rows, frames) >= 0;
}

/** Owner+reply exist but only in a prepended desert — follow pinned the live tail. */
export function ownerConversationOffscreen(rows: DisplayRow[], tailRows = 120, frames?: unknown[]): boolean {
  const owner = latestOwnerUserIndex(rows, frames);
  const reply = ownerReplyRowIndex(rows, frames);
  if (owner < 0 || reply < 0) return false;
  // Live follow scrolls to the last row (often folded steps). lastMessageRowIndex
  // ignores steps, so treat any row after the owner reply as machine tail.
  if (reply < rows.length - 1) return true;
  return owner < Math.max(0, rows.length - tailRows);
}

/** Main chat: keep paging while older history exists but owner+reply are off-screen. */
export function shouldBackfillOwnerConversation(
  agent: string,
  rows: DisplayRow[],
  older?: number,
  frames?: unknown[],
): boolean {
  if (agent !== 'jevons') return false;
  if (!older || older <= 0) return false;
  return !ownerTurnAndReplyPainted(rows, frames);
}
