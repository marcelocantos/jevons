// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Family contract for React cockpit oracles (🎯T540.1). See methodology.md. */

export type OracleLayer = 'hermetic' | 'journey';

export type OracleFamily = {
  id: string;
  title: string;
  file: string;
  layer: OracleLayer;
  /** Retired vanilla targets this family must re-prove against ui/. */
  covers: readonly string[];
};

export const CATALOG: readonly OracleFamily[] = [
  {
    id: 'fold',
    title: 'Transcript fold — bubbles, steps, silence, stream barrier',
    file: 'fold.test.ts',
    layer: 'hermetic',
    covers: ['T23', 'T63', 'T116', 'T159', 'T238', 'T240', 'T245', 'T249', 'T362', 'T479', 'T496', 'T504'],
  },
  {
    id: 'clip',
    title: 'Collapse pocket — size clip, latest expanded, same rule both panes',
    file: 'clip.test.ts',
    layer: 'hermetic',
    covers: ['T55', 'T66', 'T77', 'T106', 'T166', 'T246', 'T261', 'T480'],
  },
  {
    id: 'markdown',
    title: 'Markdown, mermaid, streaming emphasis, agent-report paint',
    file: 'markdown.test.ts',
    layer: 'hermetic',
    covers: ['T59', 'T74', 'T147', 'T150', 'T381'],
  },
  {
    id: 'composer-keys',
    title: 'Composer chords — Tab, Home/End, Alt+Enter, Ctrl+Enter, queue',
    file: 'composer-keys.test.ts',
    layer: 'hermetic',
    covers: ['T113', 'T126', 'T127', 'T132', 'T153', 'T235', 'T241', 'T307', 'T366', 'T547'],
  },
  {
    id: 'composer-chrome',
    title: 'Composer chrome — height, persist, images, Wispr, empty after send',
    file: 'composer-chrome.test.ts',
    layer: 'hermetic',
    covers: ['T70', 'T70.1', 'T76', 'T80', 'T123', 'T154', 'T183', 'T368', 'T478'],
  },
  {
    id: 'transcript-geom',
    title: 'Transcript geometry — pin, PageUp, overscan, no jiggle, connect tail',
    file: 'transcript-geom.test.ts',
    layer: 'hermetic',
    covers: ['T30.2', 'T56', 'T119', 'T119.1', 'T119.3', 'T336', 'T341', 'T347', 'T351', 'T363', 'T491', 'T494', 'T494.1.2'],
  },
  {
    id: 'plan-ticker',
    title: 'Plan ticker — damped burn, waste bands, exhausted box, marks',
    file: 'plan-ticker.test.ts',
    layer: 'hermetic',
    covers: ['T117', 'T390', 'T390.1.3', 'T390.1.6', 'T390.1.6.1', 'T390.1.6.2'],
  },
  {
    id: 'fleet-tree',
    title: 'Fleet tree — badges, migrate, sticky select, lineage chrome',
    file: 'fleet-tree.test.ts',
    layer: 'hermetic',
    covers: ['T68', 'T72', 'T72.1', 'T115', 'T285.2', 'T287', 'T293', 'T295', 'T298', 'T299', 'T348', 'T383', 'T506', 'T507', 'T508'],
  },
  {
    id: 'aside-sidebar',
    title: 'Aside / sidebar — one widget, asides off main, persist, send path',
    file: 'aside-sidebar.test.ts',
    layer: 'hermetic',
    covers: ['T65', 'T95', 'T124', 'T250', 'T251', 'T265', 'T309.1', 'T367', 'T371', 'T372'],
  },
  {
    id: 'frontier',
    title: 'Frontier tab — table, InstantTip, graph, play chrome',
    file: 'frontier.test.ts',
    layer: 'hermetic',
    covers: ['T131', 'T168', 'T173', 'T179', 'T181', 'T248', 'T280', 'T294', 'T340', 'T485'],
  },
  {
    id: 'journey-connect',
    title: 'Journey — isolate connect tail (J19) retargeted at React',
    file: 'journey-connect.test.ts',
    layer: 'journey',
    covers: ['T491', 'T493', 'T494'],
  },
];

const byId = new Map(CATALOG.map((f) => [f.id, f]));

export function family(id: string): OracleFamily {
  const f = byId.get(id);
  if (!f) throw new Error('unknown oracle family: ' + id);
  return f;
}

export function allCoveredIds(): string[] {
  return [...new Set(CATALOG.flatMap((f) => [...f.covers]))].sort();
}
