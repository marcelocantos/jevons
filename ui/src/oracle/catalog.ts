// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Family contract for React cockpit oracles (🎯T540.1). See methodology.md. */

import { CENSUS } from './census';

export type OracleLayer = 'hermetic' | 'journey';

export type OracleFamily = {
  id: string;
  title: string;
  file: string;
  layer: OracleLayer;
  /** Retired vanilla targets this family must re-prove against ui/. */
  covers: readonly string[];
};

function censusCovers(familyId: string, journeys: readonly string[]): string[] {
  return CENSUS.filter(
    (r) =>
      r.kind === 'visible' &&
      ((r.families != null && r.families.includes(familyId)) ||
        (r.journeys != null && r.journeys.some((j) => journeys.includes(j)))),
  ).map((r) => r.id);
}

function covers(familyId: string, base: readonly string[], journeys: readonly string[] = []): string[] {
  return [...new Set([...base, ...censusCovers(familyId, journeys)])].sort();
}

export const CATALOG: readonly OracleFamily[] = [
  {
    id: 'fold',
    title: 'Transcript fold — bubbles, steps, silence, stream barrier',
    file: 'fold.test.ts',
    layer: 'hermetic',
    covers: covers('fold', ['T23', 'T63', 'T116', 'T159', 'T238', 'T240', 'T245', 'T249', 'T362', 'T479', 'T496', 'T504']),
  },
  {
    id: 'clip',
    title: 'Collapse pocket — size clip, latest expanded, same rule both panes',
    file: 'clip.test.ts',
    layer: 'hermetic',
    covers: covers('clip', ['T55', 'T66', 'T77', 'T106', 'T166', 'T246', 'T261', 'T480']),
  },
  {
    id: 'markdown',
    title: 'Markdown, mermaid, streaming emphasis, agent-report paint',
    file: 'markdown.test.ts',
    layer: 'hermetic',
    covers: covers('markdown', ['T59', 'T74', 'T147', 'T150', 'T381']),
  },
  {
    id: 'composer-keys',
    title: 'Composer chords — Tab, Home/End, Alt+Enter, Ctrl+Enter, queue',
    file: 'composer-keys.test.ts',
    layer: 'hermetic',
    covers: covers('composer-keys', ['T113', 'T126', 'T127', 'T132', 'T153', 'T235', 'T241', 'T307', 'T366', 'T547']),
  },
  {
    id: 'composer-chrome',
    title: 'Composer chrome — height, persist, images, Wispr, empty after send',
    file: 'composer-chrome.test.ts',
    layer: 'hermetic',
    covers: covers('composer-chrome', ['T70', 'T70.1', 'T76', 'T80', 'T123', 'T154', 'T183', 'T368', 'T478']),
  },
  {
    id: 'transcript-geom',
    title: 'Transcript geometry — pin, PageUp, overscan, no jiggle, connect tail',
    file: 'transcript-geom.test.ts',
    layer: 'hermetic',
    covers: covers('transcript-geom', ['T30.2', 'T56', 'T119', 'T119.1', 'T119.3', 'T336', 'T341', 'T347', 'T351', 'T363', 'T491', 'T494', 'T494.1.2']),
  },
  {
    id: 'plan-ticker',
    title: 'Plan ticker — damped burn, waste bands, exhausted box, marks',
    file: 'plan-ticker.test.ts',
    layer: 'hermetic',
    covers: covers('plan-ticker', ['T117', 'T175', 'T390', 'T390.1.3', 'T390.1.6', 'T390.1.6.1', 'T390.1.6.2']),
  },
  {
    id: 'fleet-tree',
    title: 'Fleet tree — badges, migrate, sticky select, lineage chrome',
    file: 'fleet-tree.test.ts',
    layer: 'hermetic',
    covers: covers('fleet-tree', ['T68', 'T72', 'T72.1', 'T115', 'T285.2', 'T287', 'T293', 'T295', 'T298', 'T299', 'T348', 'T383', 'T506', 'T507', 'T508']),
  },
  {
    id: 'aside-sidebar',
    title: 'Aside / sidebar — one widget, asides off main, persist, send path',
    file: 'aside-sidebar.test.ts',
    layer: 'hermetic',
    covers: covers('aside-sidebar', ['T65', 'T95', 'T124', 'T250', 'T251', 'T265', 'T309.1', 'T367', 'T371', 'T372']),
  },
  {
    id: 'frontier',
    title: 'Frontier tab — table, InstantTip, graph, play chrome',
    file: 'frontier.test.ts',
    layer: 'hermetic',
    covers: covers('frontier', ['T131', 'T168', 'T173', 'T175', 'T179', 'T181', 'T184', 'T186', 'T187', 'T189', 'T203', 'T230', 'T231', 'T248', 'T271', 'T280', 'T294', 'T326', 'T340', 'T485']),
  },
  {
    id: 'journey-connect',
    title: 'Journey — isolate connect tail (J19) retargeted at React',
    file: 'journey-connect.test.ts',
    layer: 'journey',
    covers: covers('journey-connect', ['T491', 'T493', 'T494'], ['connect']),
  },
  {
    id: 'journey-send',
    title: 'Journey — send-once (J22) vs vanilla .msg.user / stream barrier',
    file: 'journey-send.test.ts',
    layer: 'journey',
    covers: covers('journey-send', ['T279', 'T281', 'T504'], ['send']),
  },
  {
    id: 'journey-fold-md',
    title: 'Journey — fold / mermaid / silent (J23)',
    file: 'journey-fold-md.test.ts',
    layer: 'journey',
    covers: covers('journey-fold-md', ['T55', 'T59', 'T238'], ['fold-md']),
  },
  {
    id: 'journey-composer',
    title: 'Journey — composer Home/End and empty height (J24)',
    file: 'journey-composer.test.ts',
    layer: 'journey',
    covers: covers('journey-composer', ['T76', 'T123', 'T126', 'T478'], ['composer']),
  },
  {
    id: 'journey-fleet',
    title: 'Journey — fleet tree (J25)',
    file: 'journey-fleet.test.ts',
    layer: 'journey',
    covers: covers('journey-fleet', ['T68', 'T72'], ['fleet']),
  },
  {
    id: 'journey-aside',
    title: 'Journey — aside target: (J26)',
    file: 'journey-aside.test.ts',
    layer: 'journey',
    covers: covers('journey-aside', ['T95', 'T250', 'T251'], ['aside']),
  },
  {
    id: 'journey-frontier',
    title: 'Journey — frontier table + graph (J27)',
    file: 'journey-frontier.test.ts',
    layer: 'journey',
    covers: covers('journey-frontier', ['T168', 'T173', 'T175', 'T181', 'T184', 'T186', 'T230'], ['frontier']),
  },
  {
    id: 'journey-ticker',
    title: 'Journey — plan ticker + RHS resize (J28)',
    file: 'journey-ticker.test.ts',
    layer: 'journey',
    covers: covers('journey-ticker', ['T175', 'T390', 'T248'], ['ticker']),
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
