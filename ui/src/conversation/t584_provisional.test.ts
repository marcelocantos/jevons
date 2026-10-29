// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { displayRows } from './display';
import { applyConversationEvent, emptyConversation } from './reduce';
import { PROVISIONAL_ASSISTANT } from './provisional';

describe('T584 provisional TUI preview', () => {
  it('replaces provisional tail on each wire frame (not append)', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: { type: PROVISIONAL_ASSISTANT, stream_id: 's1', text: '```text\nfirst\n```' },
    });
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: { type: PROVISIONAL_ASSISTANT, stream_id: 's1', text: '```text\nsecond\n```' },
    });
    expect(s.provisionalByStream.s1).toContain('second');
    expect(s.provisionalByStream.s1).not.toContain('first');
    const rows = displayRows([], { provisionalByStream: s.provisionalByStream });
    expect(rows).toHaveLength(1);
    expect(rows[0].text).toContain('second');
  });

  it('clears provisional on seal and paints sealed markdown only', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: { type: PROVISIONAL_ASSISTANT, stream_id: 's1', text: '```text\ndraft\n```' },
    });
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: {
        type: 'assistant',
        stream_id: 's1',
        message: { content: [{ type: 'text', text: '**sealed**' }] },
      },
    });
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: { type: PROVISIONAL_ASSISTANT, stream_id: 's1', text: '' },
    });
    const rows = displayRows(s.frames, { provisionalByStream: s.provisionalByStream });
    expect(rows.some((r) => r.kind === 'assistant' && r.text.includes('**sealed**'))).toBe(true);
    expect(rows.some((r) => r.kind === 'assistant' && r.text.includes('draft'))).toBe(false);
  });
});
