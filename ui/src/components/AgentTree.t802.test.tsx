// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AgentTree, mcpMissingLine } from './AgentTree';

// 🎯T802: the RHS fleet tree marks a seat whose required MCP servers never attached.
describe('AgentTree mcp-missing (🎯T802)', () => {
  it('paints the missing-MCP set on a live seat and stays quiet on healthy rows', () => {
    const { container } = render(
      <AgentTree
        selected=""
        onSelect={() => {}}
        agents={[
          { name: 'jevons-po', running: true },
          {
            name: 'jv-t802-missing',
            parent: 'jevons-po',
            running: true,
            mcp_missing: ['bullseye'],
          },
          {
            name: 'jv-t802-both',
            parent: 'jevons-po',
            running: true,
            mcp_missing: ['jevonsmcp', 'bullseye'],
          },
        ]}
      />,
    );
    const marks = [...container.querySelectorAll('.agent-mcp-missing')].map((n) => n.textContent);
    expect(marks).toEqual([
      '⚠ mcp-missing: bullseye',
      '⚠ mcp-missing: jevonsmcp, bullseye',
    ]);
    const po = [...container.querySelectorAll('.agent-node')].find(
      (n) => n.querySelector('.agent-name')?.textContent === 'jevons-po',
    );
    expect(po?.querySelector('.agent-mcp-missing')).toBeNull();
  });

  it('formats an empty set as nothing', () => {
    expect(mcpMissingLine(undefined)).toBe('');
    expect(mcpMissingLine([])).toBe('');
    expect(mcpMissingLine(['  ', ''])).toBe('');
    expect(mcpMissingLine(['bullseye'])).toBe('mcp-missing: bullseye');
  });
});
