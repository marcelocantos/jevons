// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { applyConversationEvent, emptyConversation } from './reduce';

describe('applyConversationEvent', () => {
  it('replays then marks ready on meta — one hydrate', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: { type: 'user', message: { content: 'hi' } },
    });
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'meta',
      body: { older: 10, total: 11, start: 10 },
    });
    expect(s.frames).toHaveLength(1);
    expect(s.ready).toBe(true);
    expect(s.meta?.older).toBe(10);
  });

  it('reset then replay does not keep old frames (reconnect)', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons-po', t: 'frame', body: { type: 'user' },
    });
    s = applyConversationEvent(s, { v: 1, ch: 'transcript:jevons-po', t: 'reset' });
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons-po', t: 'frame', body: { type: 'assistant' },
    });
    expect(s.frames).toHaveLength(1);
    expect((s.frames[0] as { type: string }).type).toBe('assistant');
    expect(s.ready).toBe(false);
  });

  it('page prepends older lines', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'frame', body: { id: 'new' },
    });
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'page', body: { lines: [{ id: 'old' }] },
    });
    expect(s.frames.map((f) => (f as { id: string }).id)).toEqual(['old', 'new']);
  });
});
