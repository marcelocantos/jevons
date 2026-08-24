// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { displayRows, isProtocolControlFrameText, isSilentAssistantText, stepsLabel } from '../../conversation/display';
import { isSealedAssistant, reduceTranscriptBodies } from '../../conversation/stream';
import { family } from '../catalog';
import { agentNote, assistantProse, assistantTool, silentAssistant, userTurn } from '../fixtures';
import { describeOracle, itOracle } from '../harness';

function chunk(text: string, streamId: string, stop?: string): unknown {
  return {
    type: 'assistant',
    stream_id: streamId,
    message: {
      content: text ? [{ type: 'text', text }] : [],
      ...(stop ? { stop_reason: stop } : {}),
    },
  };
}

function assistantKinds(frames: unknown[]): string[] {
  return displayRows(frames)
    .filter((r) => r.kind === 'assistant')
    .map((r) => r.text);
}

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

  itOracle('T504', 'owner user is a stream barrier — same stream_id mints below', () => {
    const leftover = 'Expected leftover mail…';
    const owner = 'why did jevons PO start the workers as Fable.';
    const reply = 'Checking how those workers were minted…';
    const sid = 'sid-t504-fable';
    const { frames } = reduceTranscriptBodies([
      chunk(leftover, sid),
      userTurn(owner),
      chunk(reply, sid),
    ]);
    const rows = displayRows(frames);
    expect(rows.map((r) => r.kind)).toEqual(['assistant', 'user', 'assistant']);
    expect(rows[0].text).toBe(leftover);
    expect(rows[0].text.includes(reply)).toBe(false);
    expect(rows[1].text).toBe(owner);
    expect(rows[2].text).toBe(reply);

    const injectSid = 'sid-t504-inject';
    const inject = displayRows(
      reduceTranscriptBodies([
        chunk('I will read the file.', injectSid),
        userTurn('<system-reminder>\nBackground task "call-1" completed (exit code: 0).\n</system-reminder>'),
        chunk(' Then edit it.', injectSid),
      ]).frames,
    );
    const injectAsst = inject.filter((r) => r.kind === 'assistant');
    expect(injectAsst).toHaveLength(1);
    expect(injectAsst[0].text).toContain('I will read the file.');
    expect(injectAsst[0].text).toContain('Then edit it.');
    expect(inject.filter((r) => r.kind === 'user')).toHaveLength(0);

    const controlSid = 'sid-t504-control';
    const control = displayRows(
      reduceTranscriptBodies([
        chunk('Checking the fleet', controlSid),
        {
          type: 'assistant',
          stream_id: controlSid,
          message: { content: [{ type: 'tool_use', name: 'jevons_agent_list' }], stop_reason: 'tool_use' },
        },
        { type: 'tool_result', message: { content: [{ type: 'tool_result', content: 'ok' }] } },
        chunk('FINAL-T504 the fleet is healthy.', controlSid),
      ]).frames,
    );
    const controlAsst = control.filter((r) => r.kind === 'assistant');
    expect(controlAsst).toHaveLength(1);
    expect(controlAsst[0].text).toContain('Checking the fleet');
    expect(controlAsst[0].text).toContain('FINAL-T504');
  });

  itOracle('T23', 'user / assistant / worker roles stay visually distinct', () => {
    const rows = displayRows([
      userTurn('hi'),
      assistantProse('ok'),
      agentNote('[worker] stalled'),
    ]);
    expect(rows.map((r) => r.kind)).toEqual(['user', 'assistant', 'steps']);
    expect(rows[0].kind).not.toBe(rows[1].kind);
    expect(rows[1].kind).not.toBe(rows[2].kind);
    expect(rows[0].kind).not.toBe(rows[2].kind);
    expect(rows[2].items?.[0]).toEqual({ cls: 'agent-note', text: '[worker] stalled' });
  });

  itOracle('T159', 'one assistant bubble per terminal stop_reason', () => {
    expect(
      isSealedAssistant({
        type: 'assistant',
        message: { content: [{ type: 'tool_use', name: 'Bash' }], stop_reason: 'tool_use' },
      }),
    ).toBe(false);

    const joined = reduceTranscriptBodies([
      userTurn('x'),
      chunk('Table then ', 's-t159'),
      {
        type: 'assistant',
        stream_id: 's-t159',
        message: { content: [{ type: 'tool_use', name: 'search_tool' }], stop_reason: 'tool_use' },
      },
      chunk('Note after tools', 's-t159'),
      chunk('', 's-t159', 'end_turn'),
    ]);
    expect(assistantKinds(joined.frames)).toEqual(['Table then \n\nNote after tools']);

    const two = reduceTranscriptBodies([
      userTurn('a'),
      chunk('first', 's-a'),
      chunk('', 's-a', 'end_turn'),
      chunk('second', 's-b'),
      chunk('', 's-b', 'end_turn'),
    ]);
    expect(assistantKinds(two.frames)).toEqual(['first', 'second']);
  });

  itOracle('T238', '[silent] replies never become owner bubbles', () => {
    expect(isSilentAssistantText('[silent] PO re-pressured')).toBe(true);
    expect(isSilentAssistantText('  [SILENT] ops ok')).toBe(true);
    expect(isSilentAssistantText('Owner needs this')).toBe(false);
    expect(isSilentAssistantText('')).toBe(false);

    const rows = displayRows([userTurn('status?'), silentAssistant(), assistantProse('visible')]);
    expect(rows.map((r) => r.kind)).toEqual(['user', 'assistant']);
    expect(rows[1].text).toBe('visible');
    expect(rows.every((r) => !r.text.includes('[silent]'))).toBe(true);
  });

  itOracle('T240', 'silent-only streams stay suppressed as a whole', () => {
    const { frames } = reduceTranscriptBodies([
      chunk('[silent]', 's9'),
      chunk(' continued jv-t240', 's9'),
      chunk('', 's9', 'end_turn'),
    ]);
    const rows = displayRows(frames);
    expect(rows.filter((r) => r.kind === 'assistant')).toEqual([]);
    expect(rows.every((r) => !/\[silent\]|continued/.test(r.text))).toBe(true);
  });

  itOracle('T245', 'silent turn does not coalesce into the next owner bubble', () => {
    const rows = displayRows([
      silentAssistant('[silent] PO already re-pressured jv-t244; no further action.'),
      agentNote('[Agent jevons-po responded] Independent gate…'),
      assistantProse('**🎯T244 landed.**\n\nIndependent check.'),
    ]);
    expect(rows.map((r) => r.kind)).toEqual(['steps', 'assistant']);
    expect(rows[1].text).toBe('**🎯T244 landed.**\n\nIndependent check.');
    expect(rows.every((r) => r.text.indexOf('[silent]') < 0)).toBe(true);
    expect(rows.every((r) => r.text.indexOf('re-pressured') < 0)).toBe(true);

    const twoVisible = displayRows([
      assistantProse('First turn.'),
      agentNote('worker note'),
      assistantProse('Second turn.'),
    ]);
    expect(assistantKinds([
      assistantProse('First turn.'),
      agentNote('worker note'),
      assistantProse('Second turn.'),
    ])).toEqual(['First turn.', 'Second turn.']);
    expect(twoVisible.filter((r) => r.kind === 'assistant')).toHaveLength(2);
  });

  itOracle('T249', 'streaming does not split one reply into multiple bubbles', () => {
    const sid = '0c38c30e-53f3-4783-8e08-dc633e707850';
    const tokens = [
      '**', '🎯', 'T', '247', ' landed', '**', ' —', ' independent', ' check', ' agrees',
      ' (`', '0', 'fb', 'ce', '59', '`,', ' herm', 'etics', ' green', ').',
      '\n\n',
      '**', 'Hard', '-', 'reload', '**',
      '\n\n',
      'Still', ' in', ' progress',
    ];
    const bodies: unknown[] = [];
    let maxAssistants = 0;
    for (const t of tokens) {
      bodies.push(chunk(t, sid));
      const n = assistantKinds(reduceTranscriptBodies(bodies).frames).length;
      if (n > maxAssistants) maxAssistants = n;
      expect(n).toBe(1);
    }
    bodies.push(chunk('', sid, 'end_turn'));
    const { frames } = reduceTranscriptBodies(bodies);
    const asst = assistantKinds(frames);
    expect(maxAssistants).toBe(1);
    expect(asst).toHaveLength(1);
    expect(asst[0]).toContain('independent check agrees');
    expect(asst[0]).toContain('Hard-reload');
    expect(asst[0]).toContain('Still in progress');
    expect(asst[0]).toContain('\n\n');
  });

  itOracle('T362', 'ux_state frames never appear as owner bubbles', () => {
    const ux = '{"type":"ux_state","composer_blocked":false}';
    const blocked = '{"type":"ux_state","composer_blocked":true,"reason":"overseer_down"}';
    for (const f of [ux, blocked, '  ' + ux + '  ', '{"type":"ping"}', '{"turns":2,"type":"rewind"}']) {
      expect(isProtocolControlFrameText(f)).toBe(true);
    }
    for (const p of ['', 'Fix the ux_state leak please.', 'Look at {"type":"ux_state"} in my chat and kill it.', '{"composer_blocked":false}', '{"type":""}', '{"type":42}', '["type","ux_state"]', '{"type":"ux_state"']) {
      expect(isProtocolControlFrameText(p)).toBe(false);
    }

    const rows = displayRows([
      userTurn('kill the ux_state leak'),
      userTurn(ux),
      userTurn(blocked),
      { type: 'ux_state', composer_blocked: false },
      assistantProse('On it.'),
    ]);
    expect(rows.filter((r) => r.kind === 'user').map((r) => r.text)).toEqual(['kill the ux_state leak']);
    expect(rows.filter((r) => r.kind === 'assistant').map((r) => r.text)).toEqual(['On it.']);
    expect(rows.some((r) => /ux_state"\s*,/.test(r.text))).toBe(false);

    const sid = 't362-stream';
    const live = displayRows(
      reduceTranscriptBodies([
        chunk('Working on ', sid),
        userTurn(ux),
        chunk('the leak.', sid),
      ]).frames,
    );
    expect(live.filter((r) => r.kind === 'user')).toHaveLength(0);
    const asst = live.filter((r) => r.kind === 'assistant');
    expect(asst).toHaveLength(1);
    expect(asst[0].text).toBe('Working on the leak.');
  });

  itOracle('T479', 'one inline-code token-stream is one bubble', () => {
    const tokens = ['`', 'index', '.html', '`'];
    const withId = reduceTranscriptBodies(tokens.map((t) => chunk(t, 's-inline')));
    expect(assistantKinds(withId.frames)).toEqual(['`index.html`']);

    const noId = reduceTranscriptBodies(
      tokens.map((t) => ({
        type: 'assistant',
        message: { content: [{ type: 'text', text: t }] },
      })),
    );
    expect(assistantKinds(noId.frames)).toEqual(['`index.html`']);
  });

  itOracle('T496', 'overseer final replies paint as main-chat assistant bubbles', () => {
    const sid = 'sid-t496';
    const { frames } = reduceTranscriptBodies([
      userTurn('how is the fleet?'),
      chunk('Checking the fleet', sid),
      chunk(' now.', sid),
      {
        type: 'assistant',
        stream_id: sid,
        message: {
          content: [{ type: 'tool_use', name: 'jevons_agent_list', input: {} }],
          stop_reason: 'tool_use',
        },
      },
      { type: 'tool_result', message: { content: [{ type: 'tool_result', content: 'ok' }] } },
      chunk('FINAL-T496 the fleet is healthy.', sid),
      chunk('', sid, 'end_turn'),
    ]);
    const rows = displayRows(frames);
    const asst = rows.filter((r) => r.kind === 'assistant');
    expect(asst).toHaveLength(1);
    expect(asst[0].text).toContain('FINAL-T496');
    expect(asst[0].text).toContain('Checking the fleet now.');
  });
});
