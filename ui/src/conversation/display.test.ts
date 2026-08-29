// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { diagnosticLabel, displayRows, shouldAckPendingSend, stepsLabel } from './display';

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
      { kind: 'user', text: "What's running right now?", when: undefined, origin: 'owner' },
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
      { kind: 'user', text: 'hello', when: undefined, origin: 'owner' },
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
      { kind: 'assistant', text: 'Yes. I have this message', when: undefined, sealed: false },
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

  it('terminal stop_reason seals only that assistant (🎯T64.4)', () => {
    const rows = displayRows([
      {
        type: 'assistant',
        stream_id: 'a',
        message: { content: [{ type: 'text', text: 'done' }], stop_reason: 'end_turn' },
      },
      {
        type: 'assistant',
        stream_id: 'b',
        message: { content: [{ type: 'text', text: 'live' }] },
      },
    ]);
    expect(rows.map((r) => [r.kind, r.sealed, r.text])).toEqual([
      ['assistant', true, 'done'],
      ['assistant', false, 'live'],
    ]);
  });

  it('send_error is a left diagnostic, not a bubble; identical consecutive nacks coalesce (🎯T545.3)', () => {
    const rows = displayRows([
      { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'hi' }] } },
      { type: 'send_error', text: 'overseer is not running' },
      { type: 'send_error', text: 'overseer is not running' },
      { type: 'send_error', text: 'overseer is not running' },
      { type: 'send_error', text: 'channel closed' },
    ]);
    expect(rows.map((r) => r.kind + ':' + r.text)).toEqual([
      'user:hi',
      'diagnostic:' + diagnosticLabel('overseer is not running', 3),
      'diagnostic:channel closed',
    ]);
    expect(rows[1].text).toBe('overseer is not running · ×3');
  });

  it('pending send acks only a user echo that arrived after send, not a nack or older bubble (🎯T545.3)', () => {
    const pending = 'hello';
    const prior = { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'hello' }] } };
    const nack = { type: 'send_error', text: 'overseer is not running' };
    const echo = { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'hello' }] } };
    expect(shouldAckPendingSend(pending, [prior, nack], 1)).toBe(false);
    expect(shouldAckPendingSend(pending, [prior], 1)).toBe(false);
    expect(shouldAckPendingSend(pending, [prior, echo], 1)).toBe(true);
    expect(shouldAckPendingSend(pending, [prior, { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'other' }] } }], 1)).toBe(false);
  });

  it('unsealed assistant stays unsealed after a later steps row (🎯T64.4)', () => {
    const rows = displayRows([
      { type: 'assistant', message: { content: [{ type: 'text', text: 'Checking.' }] } },
      { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read' }] } },
    ]);
    expect(rows[0]).toMatchObject({ kind: 'assistant', text: 'Checking.', sealed: false });
    expect(rows[1].kind).toBe('steps');
  });

  it('type=status is chrome, not an unsealed assistant (🎯T555.5)', () => {
    const rows = displayRows([
      { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'hi' }] } },
      { type: 'status', text: 'overseer is back' },
      { type: 'status', state: 'idle', text: 'overseer is back' },
      {
        type: 'assistant',
        message: { content: [{ type: 'text', text: 'ok' }], stop_reason: 'end_turn' },
      },
    ]);
    expect(rows.map((r) => r.kind + ':' + r.text)).toEqual(['user:hi', 'assistant:ok']);
    expect(rows.some((r) => String(r.text).includes('overseer'))).toBe(false);
  });
});
