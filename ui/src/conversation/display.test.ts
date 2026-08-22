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

  it('renders agent_note as note, not assistant', () => {
    const rows = displayRows([
      { type: 'agent_note', text: 'jv-t1: landed abc' },
      { type: 'assistant', message: { content: [{ type: 'text', text: 'ok' }] } },
    ]);
    expect(rows.map((r) => r.kind)).toEqual(['note', 'assistant']);
    expect(rows[0].text).toBe('jv-t1: landed abc');
  });
});
