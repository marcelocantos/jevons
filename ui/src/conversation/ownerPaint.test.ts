// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { displayRows, type DisplayRow } from './display';
import { isAgentForgedOwnerEvent } from './agentForgedOwnerTurns';
import {
  latestOwnerUserIndex,
  ownerConversationOffscreen,
  ownerTurnAndReplyPainted,
  shouldBackfillOwnerConversation,
} from './ownerPaint';

function toolFrame(i: number) {
  return {
    type: 'assistant',
    message: {
      content: [{ type: 'tool_use', id: `t${i}`, name: 'jevons_agent_list', input: {} }],
    },
  };
}

function ownerFrame(text: string) {
  return {
    type: 'user',
    turn_origin: 'owner',
    message: { role: 'user', content: [{ type: 'text', text }] },
  };
}

function assistantReply(text: string) {
  return {
    type: 'assistant',
    message: { content: [{ type: 'text', text }] },
    stop_reason: 'end_turn',
  };
}

function agentNote(i: number) {
  return { type: 'agent_note', text: `note ${i}` };
}

describe('ownerTurnAndReplyPainted (🎯T1159)', () => {
  it('is false for a connect tail of steps/notes only, true after owner+reply are in the window', () => {
    const tail: unknown[] = [];
    for (let i = 0; i < 700; i++) tail.push(toolFrame(i));
    for (let i = 0; i < 200; i++) tail.push(agentNote(i));
    const tailRows = displayRows(tail);
    expect(ownerTurnAndReplyPainted(tailRows)).toBe(false);
    expect(shouldBackfillOwnerConversation('jevons', tailRows, 9000)).toBe(true);
    expect(shouldBackfillOwnerConversation('worker', tailRows, 9000)).toBe(false);

    const withOwner = displayRows([ownerFrame('owner question'), assistantReply('answer'), ...tail]);
    expect(ownerTurnAndReplyPainted(withOwner)).toBe(true);
    expect(ownerConversationOffscreen(withOwner)).toBe(true);
    expect(shouldBackfillOwnerConversation('jevons', withOwner, 9000)).toBe(false);
  });

  it('ignores agent-forged owner wire turns for owner paint (🎯T1129 / 🎯T1164)', () => {
    const frame = {
      id: 'e:137063',
      type: 'user',
      turn_origin: 'owner',
      message: { role: 'user', content: [{ type: 'text', text: 'T1120 live-status-line worker check' }] },
    };
    expect(isAgentForgedOwnerEvent('e:137063')).toBe(true);
    const tail = Array.from({ length: 40 }, (_, i) => toolFrame(i));
    const frames = [frame, { type: 'assistant', message: { content: [{ type: 'text', text: 'ack' }] }, stop_reason: 'end_turn' }, ...tail];
    const rows = displayRows(frames);
    expect(rows[0]?.origin).toBe('agent');
    expect(latestOwnerUserIndex(rows, frames)).toBe(-1);
    expect(ownerTurnAndReplyPainted(rows, frames)).toBe(false);
    expect(ownerConversationOffscreen(rows, 120, frames)).toBe(false);
  });

  it('detects owner+reply stranded above a machine tail', () => {
    const rows: DisplayRow[] = [
      { kind: 'user', text: 'owner question', origin: 'owner' },
      { kind: 'assistant', text: 'answer' },
      ...Array.from({ length: 300 }, () => ({ kind: 'assistant' as const, text: 'machine' })),
    ];
    expect(ownerConversationOffscreen(rows, 120)).toBe(true);
    const near: DisplayRow[] = [
      ...Array.from({ length: 50 }, () => ({ kind: 'assistant' as const, text: 'machine' })),
      { kind: 'user', text: 'q', origin: 'owner' },
      { kind: 'assistant', text: 'a' },
    ];
    expect(ownerConversationOffscreen(near, 120)).toBe(false);
  });
});
