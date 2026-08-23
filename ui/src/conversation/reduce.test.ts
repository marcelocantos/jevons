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
      v: 1, ch: 'transcript:jevons', t: 'meta', body: { start: 20, older: 20, total: 21 },
    });
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'page',
      body: { lines: [{ id: 'old' }], start: 10, older: 10, total: 21 },
    });
    expect(s.frames.map((f) => (f as { id: string }).id)).toEqual(['old', 'new']);
    expect(s.meta?.start).toBe(10);
    expect(s.meta?.older).toBe(10);
  });

  it('two pages do not duplicate and meta.start advances', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'frame', body: { id: 'tail' },
    });
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'meta', body: { start: 20, older: 20, total: 22 },
    });
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'page',
      body: { lines: [{ id: 'mid' }], start: 10, older: 10, total: 22 },
    });
    const afterFirst = s.frames.map((f) => (f as { id: string }).id);
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'page',
      body: { lines: [{ id: 'mid' }], start: 10, older: 10, total: 22 },
    });
    expect(s.frames.map((f) => (f as { id: string }).id)).toEqual(afterFirst);
    expect(s.meta?.start).toBe(10);
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'page',
      body: { lines: [{ id: 'head' }], start: 0, older: 0, total: 22 },
    });
    expect(s.frames.map((f) => (f as { id: string }).id)).toEqual(['head', 'mid', 'tail']);
    expect(s.meta?.start).toBe(0);
    expect(s.meta?.older).toBe(0);
  });

  it('batch hydrate applies all frames in one reduce (🎯T537.1.2)', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'batch',
      body: {
        frames: [
          { type: 'user', message: { content: [{ type: 'text', text: 'hi' }] } },
          { type: 'assistant', stream_id: 's1', message: { content: [{ type: 'text', text: 'Yes' }] } },
          { type: 'assistant', stream_id: 's1', message: { content: [{ type: 'text', text: ' indeed' }] } },
        ],
      },
    });
    expect(s.frames).toHaveLength(2);
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'meta', body: { start: 0, older: 0, total: 3 },
    });
    expect(s.ready).toBe(true);
  });

  it('Grok token stream is one assistant bubble, then a second after end_turn (🎯T537.1.1)', () => {
    const tok = (text: string, stop?: string) => ({
      type: 'assistant',
      stream_id: 's1',
      message: {
        content: text ? [{ type: 'text', text }] : [],
        ...(stop ? { stop_reason: stop } : {}),
      },
    });
    let s = emptyConversation();
    for (const t of ['Yes', '.', ' I', ' have', ' this', ' message']) {
      s = applyConversationEvent(s, { v: 1, ch: 'transcript:jevons', t: 'frame', body: tok(t) });
    }
    expect(s.frames).toHaveLength(1);
    const content = (s.frames[0] as { message: { content: Array<{ text: string }> } }).message.content;
    expect(content.map((b) => b.text).join('')).toBe('Yes. I have this message');
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'frame', body: tok('', 'end_turn'),
    });
    expect(s.frames).toHaveLength(1);
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'frame',
      body: { type: 'assistant', stream_id: 's2', message: { content: [{ type: 'text', text: 'Next' }] } },
    });
    expect(s.frames).toHaveLength(2);
  });

  it('empty page lines clear older so paging stops', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'meta', body: { start: 5, older: 5, total: 5 },
    });
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'page',
      body: { lines: [], start: 0, total: 5 },
    });
    expect(s.meta?.older).toBe(0);
    expect(s.frames).toHaveLength(0);
  });
});
