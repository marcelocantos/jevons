// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { displayRows, stepsLabel } from './display';

describe('displayRows', () => {
  it('coalesces tool_use into ⋯ n steps, not assistant bubbles', () => {
    const rows = displayRows([
      { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'hi' }] } },
      { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read' }] } },
      { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Bash' }] } },
      { type: 'assistant', message: { content: [{ type: 'text', text: 'done' }] } },
    ]);
    expect(rows.map((r) => r.kind)).toEqual(['user', 'steps', 'assistant']);
    expect(rows[1].text).toBe(stepsLabel(2));
    expect(rows[1].text).toBe('⋯ 2 steps');
  });

  it('steps row carries tooltip items for notes and tool_use, not an empty tip (🎯T537.2.9)', () => {
    const rows = displayRows([
      { type: 'agent_note', text: '[Fleet health] stalled' },
      { type: 'agent_note', text: '[Who you are]' },
      { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read', input: { path: '/tmp/x' } }] } },
      { type: 'assistant', message: { content: [{ type: 'text', text: 'ok' }] } },
    ]);
    expect(rows[0].kind).toBe('steps');
    expect(rows[0].items).toEqual([
      { cls: 'agent-note', text: '[Fleet health] stalled' },
      { cls: 'agent-note', text: '[Who you are]' },
      { cls: 'tool-use', text: 'Read: /tmp/x' },
    ]);
  });

  it('folds agent_note into ⋯ n steps, not a note row', () => {
    const rows = displayRows([
      { type: 'agent_note', text: '[Fleet health] stalled' },
      { type: 'agent_note', text: '[Who you are]' },
      { type: 'assistant', message: { content: [{ type: 'text', text: 'ok' }] } },
    ]);
    expect(rows.map((r) => r.kind)).toEqual(['steps', 'assistant']);
    expect(rows[0].text).toBe('⋯ 2 steps');
  });

  it('owner echo with [user] marker does not paint a second bubble (🎯T537.1.2)', () => {
    const rows = displayRows([
      { type: 'user', message: { role: 'user', content: [{ type: 'text', text: "What's running right now?" }] } },
      { type: 'user', message: { role: 'user', content: [{ type: 'text', text: "[user]\nWhat's running right now?" }] } },
      { type: 'user', message: { role: 'user', content: [{ type: 'text', text: "[user]\nWhat's running right now?" }] } },
    ]);
    expect(rows.filter((r) => r.kind === 'user')).toEqual([
      { kind: 'user', text: "What's running right now?", when: undefined },
    ]);
  });

  it('owner echo wrapped in user_query paints once (🎯T537.1.2)', () => {
    const rows = displayRows([
      { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'hello' }] } },
      {
        type: 'user',
        message: { role: 'user', content: [{ type: 'text', text: '[user]\n<user_query>\nhello\n</user_query>' }] },
      },
    ]);
    expect(rows.filter((r) => r.kind === 'user')).toEqual([
      { kind: 'user', text: 'hello', when: undefined },
    ]);
  });

  it('one coalesced assistant frame is one row, not one pill per token (🎯T537.1.1)', () => {
    const rows = displayRows([
      {
        type: 'assistant',
        stream_id: 's1',
        message: { content: [{ type: 'text', text: 'Yes. I have this message' }] },
      },
    ]);
    expect(rows).toEqual([
      { kind: 'assistant', text: 'Yes. I have this message', when: undefined },
    ]);
  });

  it('coalesces agent_note with tool_use into one capsule', () => {
    const rows = displayRows([
      { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'hi' }] } },
      { type: 'agent_note', text: '[Fleet health] stalled' },
      { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read' }] } },
      { type: 'assistant', message: { content: [{ type: 'text', text: 'done' }] } },
    ]);
    expect(rows.map((r) => r.kind)).toEqual(['user', 'steps', 'assistant']);
    expect(rows[1].text).toBe('⋯ 2 steps');
  });
});
