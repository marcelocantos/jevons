// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { normalizeGraphPayload, resolveFrontierGraphOpenPlan } from './graphPack';

function multiDiagramPayload() {
  const diagrams = [];
  for (let i = 0; i < 7; i++) {
    diagrams.push({
      id: 'c' + i,
      kind: i === 6 ? 'orphans' : 'component',
      title: i === 6 ? 'Orphans (4)' : 'Component',
      mermaid: 'flowchart TB\n  A' + i + '-->B' + i + '\n',
      node_count: i === 0 ? 24 : 4,
      edge_count: i === 0 ? 18 : 2,
    });
  }
  return {
    available: true,
    pack: 'wrap-grid',
    node_count: 42,
    edge_count: 29,
    diagrams,
    mermaid: '%% jevons-frontier-pack pack=wrap-grid diagrams=7 %%\n' + diagrams.map((d) => d.mermaid).join('\n'),
  };
}

describe('T294 graph pack model', () => {
  it('opens every component, not a joined mermaid source', () => {
    const model = normalizeGraphPayload(multiDiagramPayload());
    const plan = resolveFrontierGraphOpenPlan(model);
    expect(plan.mode).toBe('pack');
    expect(plan.diagramCount).toBe(7);
    expect(model.diagrams.map((d) => d.id)).toEqual(['c0', 'c1', 'c2', 'c3', 'c4', 'c5', 'c6']);
    expect(model.mermaid).toMatch(/jevons-frontier-pack/);
  });

  it('refuses a joined pack source without diagrams[]', () => {
    const model = normalizeGraphPayload({
      available: true,
      mermaid: '%% jevons-frontier-pack pack=wrap-grid diagrams=2 %%\nflowchart TB\nA-->B\nflowchart TB\nC-->D\n',
    });
    expect(model.diagrams.length).toBe(0);
    const plan = resolveFrontierGraphOpenPlan(model);
    expect(plan.mode).toBe('empty');
  });

  it('HTTP 200 with error is a failure model, not an empty ledger', () => {
    const model = normalizeGraphPayload({
      available: false,
      error: 'bullseye open: exit status 101 — panic graph.rs:704',
      updated_at: '2026-08-08T00:00:00Z',
    });
    const plan = resolveFrontierGraphOpenPlan(model);
    expect(plan.mode).toBe('empty');
    expect(model.error).toMatch(/panic graph\.rs:704/);
  });
});
