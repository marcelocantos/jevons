// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { absTimeTitle } from '../../absTime';
import { displayRows, isSilentAssistantText, stepsLabel } from '../../conversation/display';
import { isSealedAssistant } from '../../conversation/stream';
import { family } from '../catalog';
import { assistantProse, assistantTool, silentAssistant, userTurn } from '../fixtures';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('fold'), () => {
  itOracle('T63', 'tool_use folds into steps, not an assistant bubble', () => {
    const rows = displayRows([
      userTurn('hi'),
      assistantTool('Read'),
      assistantTool('Bash'),
      assistantProse('done'),
    ]);
    expect(rows.map((r) => r.kind)).toEqual(['user', 'steps', 'assistant']);
    expect(rows[1].text).toBe(stepsLabel(2));
  });

  itOracle('T116', 'nested MCP tool_input shows the real tool name', () => {
    const rows = displayRows([
      assistantTool('use_tool', {
        tool_name: 'search_tool',
        tool_input: { limit: 3, query: 'jevonsmcp agent list' },
      }),
    ]);
    expect(rows[0].items?.[0]?.text).toBe('use_tool: search_tool: jevonsmcp agent list');
  });

  itOracle('T504', 'user then assistant is two rows — user is a stream barrier', () => {
    const rows = displayRows([userTurn('go'), assistantProse('ok')]);
    expect(rows.map((r) => r.kind)).toEqual(['user', 'assistant']);
    expect(rows[0].text).toBe('go');
    expect(rows[1].text).toBe('ok');
  });

  itOracle('T23', 'user / assistant / worker roles stay visually distinct', () => {
    const rows = displayRows([userTurn('hi'), assistantTool('Read'), assistantProse('done')]);
    expect(new Set(rows.map((r) => r.kind))).toEqual(new Set(['user', 'steps', 'assistant']));
    expect(rows.filter((r) => r.kind === 'user').every((r) => r.kind !== 'assistant')).toBe(true);
  });

  itOracle('T159', 'one assistant bubble per terminal stop_reason', () => {
    const a = assistantProse('first');
    const b = assistantProse('second');
    expect(isSealedAssistant(a)).toBe(true);
    expect(isSealedAssistant(b)).toBe(true);
    const rows = displayRows([a, b]);
    expect(rows.filter((r) => r.kind === 'assistant').map((r) => r.text)).toEqual(['first', 'second']);
  });

  itOracle('T238', '[silent] replies never become owner bubbles', () => {
    expect(isSilentAssistantText('[silent] PO already re-pressured')).toBe(true);
    const rows = displayRows([silentAssistant(), userTurn('hi')]);
    expect(rows.some((r) => r.kind === 'user' && /\[silent\]/i.test(r.text))).toBe(false);
    expect(rows.some((r) => /\[silent\]/i.test(r.text))).toBe(false);
  });

  itOracle('T240', 'silent-only streams stay suppressed as a whole', () => {
    const rows = displayRows([silentAssistant('[silent] a'), silentAssistant('[silent] b')]);
    expect(rows.filter((r) => r.kind === 'assistant' || r.kind === 'user')).toEqual([]);
  });

  itOracle('T245', 'silent turn does not coalesce into the next owner bubble', () => {
    const rows = displayRows([silentAssistant(), userTurn('next'), assistantProse('ok')]);
    expect(rows.map((r) => r.kind)).toEqual(['user', 'assistant']);
    expect(rows[0].text).toBe('next');
    expect(rows[0].text).not.toMatch(/silent/i);
  });

  itOracle('T249', 'streaming does not split one reply into multiple bubbles', () => {
    const rows = displayRows([
      {
        type: 'assistant',
        message: { content: [{ type: 'text', text: 'hello world' }], stop_reason: 'end_turn' },
      },
    ]);
    expect(rows.filter((r) => r.kind === 'assistant')).toHaveLength(1);
    expect(rows[0].text).toBe('hello world');
  });

  itOracle('T362', 'ux_state frames never appear as owner bubbles', () => {
    const rows = displayRows([
      { type: 'ux_state', composer_blocked: true },
      userTurn('{"type":"ux_state","composer_blocked":true}'),
      userTurn('real owner prose'),
    ]);
    expect(rows.filter((r) => r.kind === 'user').map((r) => r.text)).toEqual(['real owner prose']);
  });

  itOracle('T479', 'one inline-code token-stream is one bubble', () => {
    const rows = displayRows([assistantProse('use `foo` now')]);
    expect(rows.filter((r) => r.kind === 'assistant')).toHaveLength(1);
  });

  itOracle('T496', 'overseer final replies paint as main-chat assistant bubbles', () => {
    const rows = displayRows([userTurn('go'), assistantProse('done')]);
    expect(rows[1]).toMatchObject({ kind: 'assistant', text: 'done', sealed: true });
  });

  itOracle('T91', 'each transcript item shows an accurate timestamp on hover', () => {
    const ms = Date.UTC(2026, 7, 26, 13, 41, 59);
    const want = new Date(ms).toLocaleString(undefined, {
      year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit',
    });
    expect(absTimeTitle(ms)).toBe(want);
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../../components/AgentTranscript.tsx'), 'utf8');
    expect(src).toMatch(/className="msg-time"[^>]*title=\{absTimeTitle\(props\.when\)\}/);
  });
  itOracle.skip('T122', 'activity-strip tooltips wrap instead of overflowing', 'not ported: InstantTip-class hover');
  itOracle.skip('T233', 'compacted activity nuggets expose hover detail', 'not ported: InstantTip-class hover');
});
