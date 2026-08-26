// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { displayRows, stepsLabel } from '../../conversation/display';
import { family } from '../catalog';
import { assistantProse, assistantTool, userTurn } from '../fixtures';
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

  itOracle.todo('T23', 'user / assistant / worker roles stay visually distinct');
  itOracle.todo('T159', 'one assistant bubble per terminal stop_reason');
  itOracle.todo('T238', '[silent] replies never become owner bubbles');
  itOracle.todo('T240', 'silent-only streams stay suppressed as a whole');
  itOracle.todo('T245', 'silent turn does not coalesce into the next owner bubble');
  itOracle.todo('T249', 'streaming does not split one reply into multiple bubbles');
  itOracle.todo('T362', 'ux_state frames never appear as owner bubbles');
  itOracle.todo('T479', 'one inline-code token-stream is one bubble');
  itOracle.todo('T496', 'overseer final replies paint as main-chat assistant bubbles');
});
