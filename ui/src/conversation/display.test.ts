// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { displayRows, stepsLabel } from './display';

describe('displayRows', () => {
  it('nested MCP tool_input shows the real tool name, not a key dump (🎯T116)', () => {
    const rows = displayRows([
      {
        type: 'assistant',
        message: {
          content: [
            {
              type: 'tool_use',
              name: 'use_tool',
              input: { tool_name: 'search_tool', tool_input: { limit: 3, query: 'jevonsmcp agent list' } },
            },
          ],
        },
      },
    ]);
    expect(rows[0].items?.[0]?.text).toBe('use_tool: search_tool: jevonsmcp agent list');
  });

  it('generic MCP: tool label yields to nested tool_name (🎯T63)', () => {
    const rows = displayRows([
      {
        type: 'assistant',
        message: {
          content: [
            {
              type: 'tool_use',
              name: 'MCP: tool',
              input: { tool_name: 'jevons_agent_list', tool_input: { query: 'running' } },
            },
          ],
        },
      },
    ]);
    expect(rows[0].items?.[0]?.text).toBe('jevons_agent_list: running');
  });

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

  it('mixed text+tool_use on one frame still counts every tool (🎯T119.10)', () => {
    const rows = displayRows([
      {
        type: 'assistant',
        message: {
          content: [
            { type: 'text', text: 'Checking.' },
            { type: 'tool_use', name: 'Read', input: { path: 'a' } },
            { type: 'tool_use', name: 'Grep', input: { path: 'x' } },
          ],
        },
      },
    ]);
    expect(rows.map((r) => r.kind)).toEqual(['assistant', 'steps']);
    expect(rows[0].text).toBe('Checking.');
    expect(rows[1].text).toBe('⋯ 2 steps');
    expect(rows[1].items).toEqual([
      { cls: 'tool-use', text: 'Read: a' },
      { cls: 'tool-use', text: 'Grep: x' },
    ]);
  });

  it('tool_use + tool_result is one step live and on hydrate (🎯T119.10)', () => {
    const tape = [
      { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read' }] } },
      { type: 'tool_result', tool_use_id: '1', content: 'ok' },
      { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Grep' }] } },
      { type: 'tool_result', tool_use_id: '2', content: 'ok' },
      { type: 'progress', recorded: 'lossless', progress_type: 'tool_use' },
    ];
    const rows = displayRows(tape);
    expect(rows).toHaveLength(1);
    expect(rows[0].kind).toBe('steps');
    expect(rows[0].text).toBe('⋯ 2 steps');
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
