// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AgentTree } from './AgentTree';

describe('AgentTree model badges', () => {
  it('paints Claude and Cursor siblings, and does not invent a version', () => {
    const { container } = render(
      <AgentTree
        selected=""
        onSelect={() => {}}
        agents={[
          { name: 'jevons-po' },
          {
            name: 'jv-compact-a7a1dc5e',
            parent: 'jevons-po',
            provider: 'claude',
            model: 'claude-opus-4-5',
          },
          { name: 'jv-t541-acp-materialize', parent: 'jevons-po', provider: 'cursor' },
          { name: 'jv-bare-claude', parent: 'jevons-po', provider: 'claude' },
        ]}
      />,
    );
    const badges = [...container.querySelectorAll('.model-badge')];
    const byName = new Map(
      badges.map((b) => {
        const row = b.closest('.agent-node');
        const name = row?.querySelector('.agent-name')?.textContent || '';
        return [name, b];
      }),
    );
    expect(byName.get('jv-compact-a7a1dc5e')?.getAttribute('data-company')).toBe('anthropic');
    expect(byName.get('jv-compact-a7a1dc5e')?.querySelector('sub')?.textContent).toBe('O4.5');
    expect(byName.get('jv-t541-acp-materialize')?.getAttribute('data-company')).toBe('cursor');
    expect(byName.get('jv-bare-claude')?.getAttribute('data-company')).toBe('anthropic');
    expect(byName.get('jv-bare-claude')?.querySelector('sub')).toBeNull();
    expect(byName.has('jevons-po')).toBe(false);
  });
});
