// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { displayRows } from './display';
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

  it('first assistant put is visible before later tokens append (🎯T64.3)', () => {
    const asst = (id: string, index: number, text: string, op: 'put' | 'append') => ({
      id,
      index,
      op,
      type: 'assistant',
      ...(op === 'put'
        ? { event: { type: 'assistant', message: { content: [{ type: 'text', text }] } } }
        : { text }),
    });
    const tool = (id: string, index: number, name: string) => ({
      id,
      index,
      op: 'put' as const,
      type: 'tool_use',
      event: { type: 'tool_use', name },
    });
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'frame', body: asst('e:10', 10, 'Checking.', 'put'),
    });
    expect(displayRows(s.frames).map((r) => r.kind + ':' + r.text)).toEqual(['assistant:Checking.']);
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'frame', body: tool('e:11', 11, 'Read'),
    });
    expect(displayRows(s.frames).map((r) => r.kind)).toEqual(['assistant', 'steps']);
    expect(displayRows(s.frames)[0].text).toBe('Checking.');
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'frame', body: asst('e:10', 10, ' Yes.', 'append'),
    });
    const rows = displayRows(s.frames);
    expect(rows.map((r) => r.kind)).toEqual(['assistant', 'steps']);
    expect(rows[0].text).toBe('Checking. Yes.');
    expect(rows[0].sealed).toBe(false);
  });

  it('mux append copies terminal stop_reason onto the growing frame (🎯T64.4)', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: {
        id: 'e:1',
        index: 1,
        op: 'put',
        type: 'assistant',
        event: { type: 'assistant', message: { content: [{ type: 'text', text: 'Hi' }] } },
      },
    });
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: {
        id: 'e:1',
        index: 1,
        op: 'append',
        type: 'assistant',
        text: ' there',
        event: {
          type: 'assistant',
          message: { content: [{ type: 'text', text: 'Hi there' }], stop_reason: 'end_turn' },
        },
      },
    });
    const rows = displayRows(s.frames);
    expect(rows).toEqual([
      { kind: 'assistant', text: 'Hi there', when: undefined, sealed: true },
    ]);
  });

  it('window put+append+before stays in journal order (🎯T537.1.3)', () => {
    const put = (id: string, index: number, type: string, text: string) => ({
      id,
      index,
      op: 'put' as const,
      type,
      event: {
        type,
        message: { content: [{ type: 'text', text }] },
      },
    });
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'batch',
      body: {
        frames: [
          put('e:3', 3, 'assistant', 'Hello'),
          put('e:4', 4, 'user', 'u2'),
          put('e:5', 5, 'assistant', 'Hi'),
        ],
      },
    });
    expect(s.frames.map((f) => (f as { id: string }).id)).toEqual(['e:3', 'e:4', 'e:5']);
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: { id: 'e:5', index: 5, op: 'append', text: ' there' },
    });
    const last = s.frames[2] as { message: { content: Array<{ text: string }> } };
    expect(last.message.content.map((b) => b.text).join('')).toBe('Hi there');
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'page',
      body: {
        lines: [put('e:1', 1, 'user', 'u0'), put('e:2', 2, 'user', 'u1')],
        start: 1,
        older: 1,
        total: 5,
      },
    });
    expect(s.frames.map((f) => (f as { id: string }).id)).toEqual(['e:1', 'e:2', 'e:3', 'e:4', 'e:5']);
    const texts = s.frames.map((f) => {
      const msg = (f as { message?: { content?: Array<{ text?: string }> } }).message;
      return (msg?.content || []).map((b) => b.text || '').join('');
    });
    expect(texts).toEqual(['u0', 'u1', 'Hello', 'u2', 'Hi there']);
  });

  it('sealed window put is not re-joined with raw tokens (🎯T537.1.3)', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: {
        id: 'e:1',
        index: 1,
        op: 'put',
        event: {
          type: 'assistant',
          stream_id: 's1',
          message: { content: [{ type: 'text', text: 'Hello' }] },
        },
      },
    });
    // A second put of the same id replaces; it must not become HelloHello.
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: {
        id: 'e:1',
        index: 1,
        op: 'put',
        event: {
          type: 'assistant',
          stream_id: 's1',
          message: { content: [{ type: 'text', text: 'Hello' }] },
        },
      },
    });
    expect(s.frames).toHaveLength(1);
    const content = (s.frames[0] as { message: { content: Array<{ text: string }> } }).message.content;
    expect(content.map((b) => b.text).join('')).toBe('Hello');
  });

  it('live user after hydrate is a new frame, not dropped', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'batch',
      body: {
        frames: [
          {
            id: 'e:1',
            index: 1,
            op: 'put',
            type: 'assistant',
            event: { type: 'assistant', message: { content: [{ type: 'text', text: 'prev' }] } },
          },
        ],
      },
    });
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'meta', body: { following: true, older: 10, total: 11 },
    });
    s = applyConversationEvent(s, {
      v: 1,
      ch: 'transcript:jevons',
      t: 'frame',
      body: {
        type: 'user',
        turn_origin: 'owner',
        message: { role: 'user', content: 'It is still in the tree.' },
      },
    });
    expect(s.frames).toHaveLength(2);
    const last = s.frames[1] as { type?: string; message?: { content?: string } };
    expect(last.type).toBe('user');
    expect(last.message?.content).toBe('It is still in the tree.');
  });

  it('mux error is a send_error frame, not latched chrome state (🎯T545.3)', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'error', body: { error: 'overseer is not running' },
    });
    expect(s.error).toBeNull();
    expect(s.frames).toEqual([{ type: 'send_error', text: 'overseer is not running' }]);
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'error', body: { error: 'overseer is not running' },
    });
    expect(s.error).toBeNull();
    expect(s.frames).toHaveLength(2);
    expect(displayRows(s.frames).map((r) => r.text)).toEqual(['overseer is not running · ×2']);
  });

  it('empty page lines are not EOF unless start is the journal head', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'meta', body: { start: 80, older: 80, total: 200 },
    });
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'page',
      body: { lines: [], start: 50, older: 50, total: 200 },
    });
    expect(s.meta?.older).toBe(50);
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'page',
      body: { lines: [], start: 1, older: 0, total: 200 },
    });
    expect(s.meta?.older).toBe(0);
  });

  it('keeps paging when start is cache-head but the journal is still truncated (🎯T494.1.4)', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'meta', body: { start: 1, older: 2, total: 40, truncated: true },
    });
    expect(s.meta?.older).toBe(2);
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'page',
      body: { lines: [{ id: 'e:o:1' }], start: 1, older: 2, total: 80, truncated: true },
    });
    expect(s.meta?.older).toBe(2);
    expect(s.meta?.truncated).toBe(true);
  });
});
