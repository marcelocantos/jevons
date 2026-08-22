// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { buildAgentForest, type AgentRow } from './AgentTree';

describe('buildAgentForest', () => {
  it('nests children under parent', () => {
    const agents: AgentRow[] = [
      { name: 'jevons' },
      { name: 'jevons-po', parent: 'jevons' },
      { name: 'jv-t1', parent: 'jevons-po' },
    ];
    const roots = buildAgentForest(agents);
    expect(roots.map((n) => n.name)).toEqual(['jevons']);
    expect(roots[0].children.map((n) => n.name)).toEqual(['jevons-po']);
    expect(roots[0].children[0].children.map((n) => n.name)).toEqual(['jv-t1']);
  });
});
