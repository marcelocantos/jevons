// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { displayRows } from './display';

const owner = (text: string) => ({ type: 'user', turn_origin: 'owner', message: { role: 'user', content: [{ type: 'text', text }] } });
const agentUser = (text: string) => ({ type: 'user', turn_origin: 'agent', message: { role: 'user', content: [{ type: 'text', text }] } });
const reply = (text: string) => ({ type: 'assistant', message: { content: [{ type: 'text', text }] }, stop_reason: 'end_turn' });
const note = (text: string) => ({ type: 'agent_note', text });
const machineBoundary = (text: string) => ({ type: 'agent_note', text, notice: { kind: 'machine-turn' } });

// 🎯T1122: in the owner's main chat, a turn the owner did not start folds.
describe('owner chat folds machine-started turns', () => {
  const frames = [
    owner('how goes?'),
    reply('Two workers are on T1161.'),
    note('[Fleet health] Stalled backlog on "claudia-po"'),
    reply('No action needed.'),
    agentUser('finish-report from jv-t1164'),
    reply('Verified; routing to jevons-po.'),
  ];

  it('paints only the owner turn and its reply as bubbles', () => {
    const rows = displayRows(frames, { ownerChat: true });
    const bubbles = rows.filter((r) => r.kind === 'user' || r.kind === 'assistant').map((r) => r.text);
    expect(bubbles).toEqual(['how goes?', 'Two workers are on T1161.']);
    const steps = rows.filter((r) => r.kind === 'steps');
    expect(steps.length).toBeGreaterThan(0);
    const folded = steps.flatMap((r) => (r.items || []).map((it) => it.text));
    expect(folded).toContain('No action needed.');
    expect(folded).toContain('Verified; routing to jevons-po.');
  });

  it('a new owner turn paints again after machine traffic', () => {
    const rows = displayRows([...frames, owner('ship it?'), reply('Not yet: mcpserver is red.')], { ownerChat: true });
    const bubbles = rows.filter((r) => r.kind === 'user' || r.kind === 'assistant').map((r) => r.text);
    expect(bubbles.slice(-2)).toEqual(['ship it?', 'Not yet: mcpserver is red.']);
  });

  it('a window that opens mid machine turn folds its orphan replies', () => {
    const rows = displayRows([reply('No action needed.'), owner('status?'), reply('All green.')], { ownerChat: true });
    const bubbles = rows.filter((r) => r.kind === 'user' || r.kind === 'assistant').map((r) => r.text);
    expect(bubbles).toEqual(['status?', 'All green.']);
  });

  it('a machine-turn boundary after an owner reply folds the next assistant (T1169)', () => {
    const rows = displayRows(
      [owner('how goes?'), reply('On it.'), machineBoundary('2 machine report(s): jv-a, jv-b'), reply('No action needed.')],
      { ownerChat: true },
    );
    const bubbles = rows.filter((r) => r.kind === 'user' || r.kind === 'assistant').map((r) => r.text);
    expect(bubbles).toEqual(['how goes?', 'On it.']);
    const folded = rows.flatMap((r) => (r.kind === 'steps' ? (r.items || []).map((it) => it.text) : []));
    expect(folded).toContain('No action needed.');
  });

  // Pre-🎯T1169 transcripts journalled no boundary for machine turns: the
  // 2026-10-21 09:52 specimen is an owner turn, its sealed reply, then a run
  // of separately streamed "No action needed" replies with nothing between.
  it('prose after the owner turn seals folds even with no boundary frame', () => {
    const sealed = (text: string, stream: string) => ({ type: 'assistant', stream_id: stream, message: { content: [{ type: 'text', text }], stop_reason: 'end_turn' } });
    const rows = displayRows([
      owner('Yes, that was me.'),
      sealed('Confirmed by you directly.', 's1'),
      sealed('No action needed.', 's2'),
      sealed('No action needed — fully resolved.', 's3'),
    ], { ownerChat: true });
    const bubbles = rows.filter((r) => r.kind === 'user' || r.kind === 'assistant').map((r) => r.text);
    expect(bubbles).toEqual(['Yes, that was me.', 'Confirmed by you directly.']);
  });

  it('a multi-round owner reply paints every round until it seals', () => {
    const toolRound = { type: 'assistant', stream_id: 'a', message: { content: [{ type: 'text', text: 'Checking the gate.' }, { type: 'tool_use', id: 't1', name: 'Bash', input: {} }], stop_reason: 'tool_use' } };
    const final = { type: 'assistant', stream_id: 'b', message: { content: [{ type: 'text', text: 'Gate is green.' }], stop_reason: 'end_turn' } };
    const rows = displayRows([owner('is it green?'), toolRound, final], { ownerChat: true });
    const bubbles = rows.filter((r) => r.kind === 'user' || r.kind === 'assistant').map((r) => r.text);
    expect(bubbles).toEqual(['is it green?', 'Checking the gate.', 'Gate is green.']);
  });

  it('without ownerChat nothing changes', () => {
    const rows = displayRows(frames);
    expect(rows.some((r) => r.kind === 'assistant' && r.text === 'No action needed.')).toBe(true);
  });
});
