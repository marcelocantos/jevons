// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { displayRows, queuedOwnerRows } from './display';

/** 🎯T616: a reply is visibly bound to the request that elicited it. */

describe('queuedOwnerRows (T616)', () => {
  it('paints each pending queue item as an owner bubble marked queued', () => {
    const rows = queuedOwnerRows([
      { id: 'q1', text: 'follow up about MCP' },
      { id: 'q2', text: 'and another' },
    ]);
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({
      kind: 'user',
      text: 'follow up about MCP',
      id: 'queued:q1',
      origin: 'owner',
      delivery: { state: 'queued' },
    });
    expect(rows[1].id).toBe('queued:q2');
  });

  it('skips empty items and keeps image-only markers readable', () => {
    expect(queuedOwnerRows([{ id: 'e', text: '   ' }])).toEqual([]);
    const img = queuedOwnerRows([{ id: 'i', text: '[image: abc]\n' }]);
    expect(img).toHaveLength(1);
    expect(img[0].delivery?.state).toBe('queued');
    expect(img[0].text).toContain('[image: abc]');
  });
});

describe('stream termination (T616)', () => {
  it('an unsealed assistant stays unsealed; a terminal stop seals it', () => {
    const streaming = {
      type: 'assistant',
      message: { role: 'assistant', content: [{ type: 'text', text: 'Still going' }] },
    };
    const sealed = {
      type: 'assistant',
      message: {
        role: 'assistant',
        content: [{ type: 'text', text: 'Done.' }],
        stop_reason: 'end_turn',
      },
    };
    const rows = displayRows([
      { type: 'user', turn_origin: 'owner', message: { role: 'user', content: [{ type: 'text', text: 'hi' }] } },
      streaming,
    ]);
    expect(rows.find((r) => r.kind === 'assistant')?.sealed).toBe(false);

    const done = displayRows([
      { type: 'user', turn_origin: 'owner', message: { role: 'user', content: [{ type: 'text', text: 'hi' }] } },
      sealed,
    ]);
    expect(done.find((r) => r.kind === 'assistant')?.sealed).toBe(true);
  });

  it('owner + streaming assistant + queued follow-up reads as request, answer-in-progress, then waiting', () => {
    const frames = [
      {
        type: 'user',
        turn_origin: 'owner',
        message: { role: 'user', content: [{ type: 'text', text: 'switch both seats to Grok' }] },
      },
      {
        type: 'assistant',
        stream_id: 's1',
        message: { role: 'assistant', content: [{ type: 'text', text: 'Switching now…' }] },
      },
    ];
    const rows = displayRows(frames).concat(
      queuedOwnerRows([{ id: 'q', text: 'why is MCP search empty?' }]),
    );
    expect(rows.map((r) => r.kind)).toEqual(['user', 'assistant', 'user']);
    expect(rows[0].text).toContain('switch both seats');
    expect(rows[1].sealed).toBe(false);
    expect(rows[2].delivery?.state).toBe('queued');
    expect(rows[2].text).toContain('MCP search');
  });
});
