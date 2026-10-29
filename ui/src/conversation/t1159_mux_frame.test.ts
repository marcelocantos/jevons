// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { applyConversationEvent, emptyConversation } from './reduce';
import { displayRows } from './display';
import { latestOwnerUserIndex } from './ownerPaint';

const ownerMuxFrame = {
  id: 'e:137063',
  index: 137063,
  op: 'put',
  type: 'user',
  event: {
    type: 'user',
    turn_origin: 'owner',
    msg_id: 'om-1792577687079-63',
    message: {
      role: 'user',
      content: [{ type: 'text', text: 'T1120 live-status-line worker check: reply with just "ack" please.' }],
    },
  },
};

describe('owner mux frame paints (🎯T1159)', () => {
  it('reduce + displayRows keep owner user rows from mux window frames', () => {
    let state = emptyConversation();
    state = applyConversationEvent(state, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: ownerMuxFrame,
    });
    expect(state.frames.length).toBe(1);
    const rows = displayRows(state.frames);
    expect(rows[0]?.kind).toBe('user');
    expect(rows[0]?.origin).toBe('agent');
    expect(latestOwnerUserIndex(rows, state.frames)).toBe(-1);
  });
});
