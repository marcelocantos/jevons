// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { CATALOG, allCoveredIds } from './catalog';
import {
  CENSUS,
  CENSUS_CUTOFF,
  censusExceptions,
  censusIds,
  censusVisible,
} from './census';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, '../../..');
const TID = /T\d+(?:\.\d+)*/g;

function mentionedIn(dir: string, exts: string[]): Set<string> {
  const found = new Set<string>();
  const walk = (d: string) => {
    for (const name of readdirSync(d, { withFileTypes: true })) {
      const p = join(d, name.name);
      if (name.isDirectory()) {
        walk(p);
        continue;
      }
      if (!exts.some((e) => name.name.endsWith(e))) continue;
      const text = readFileSync(p, 'utf8');
      for (const m of text.matchAll(TID)) found.add(m[0]);
    }
  };
  walk(dir);
  return found;
}

describe('pre-React UI census', () => {
  it('has unique ids and a cutoff', () => {
    expect(CENSUS_CUTOFF).toBe('2026-08-22');
    const ids = censusIds();
    expect(new Set(ids).size).toBe(ids.length);
    expect(CENSUS.length).toBeGreaterThanOrEqual(260);
    expect(censusVisible().length).toBeGreaterThanOrEqual(200);
  });

  it('every exception has a why', () => {
    for (const r of censusExceptions()) {
      expect(r.reason && r.reason.trim().length > 8, r.id + ' empty reason').toBe(true);
      expect(r.journeys || r.families, r.id + ' exception must not claim coverage').toBeFalsy();
    }
  });

  it('every visible row names a journey or a family', () => {
    for (const r of censusVisible()) {
      const n = (r.journeys?.length || 0) + (r.families?.length || 0);
      expect(n, r.id + ' visible with no journey/family').toBeGreaterThan(0);
    }
  });

  it('every visible id is in some catalog covers list', () => {
    const covered = new Set(allCoveredIds());
    const missing = censusVisible().filter((r) => !covered.has(r.id)).map((r) => r.id);
    expect(missing).toEqual([]);
  });

  it('tagged pre-cutoff achieved UI targets are all in the census', () => {
    // Same contract as scripts/docratchet/t540_1_census_test.go: only the
    // target's tags: list, not the word "chat" in context prose.
    const yaml = readFileSync(join(repoRoot, 'bullseye.yaml'), 'utf8');
    const tagged = new Set<string>();
    const uiTag = new Set([
      'web', 'cockpit', 'chat', 'visual', 'composer', 'virtual-list',
      'virtualization', 'collapse', 'plan-usage', 'activity-strip',
      'keyboard', 'markdown', 'scroll', 'frontier', 'ux',
    ]);
    const blocks = yaml.split(/\n  (?=T\d)/);
    for (const block of blocks) {
      const idm = /^(T\d+(?:\.\d+)*)/.exec(block.trim());
      if (!idm) continue;
      const tid = idm[1];
      if (!/^  status:\s*achieved/m.test(block) && !/^status:\s*achieved/m.test(block)) continue;
      const am = /^  achieved:\s*(\d{4}-\d{2}-\d{2})/m.exec(block) || /^achieved:\s*(\d{4}-\d{2}-\d{2})/m.exec(block);
      if (!am || am[1] >= CENSUS_CUTOFF) continue;
      const tm = /\n  tags:\n((?:    - [^\n]+\n)+)/.exec('\n' + block);
      if (!tm) continue;
      const tags = [...tm[1].matchAll(/- ([^\n]+)/g)].map((x) => x[1].trim());
      if (tags.some((t) => uiTag.has(t))) tagged.add(tid);
    }
    const have = new Set(censusIds());
    const missing = [...tagged].filter((id) => !have.has(id)).sort();
    expect(missing, 'tagged pre-cutoff UI ids missing from census').toEqual([]);
  });

  it('every visible id is mentioned by a family test or a journey-suite file', () => {
    const ids = new Set<string>();
    for (const id of mentionedIn(join(here, 'families'), ['.ts'])) ids.add(id);
    for (const id of mentionedIn(join(repoRoot, 'scripts/journey-suite'), ['.go', '.js', '.mjs', '.ts'])) {
      ids.add(id);
    }
    const missing = censusVisible().filter((r) => !ids.has(r.id)).map((r) => r.id);
    expect(missing).toEqual([]);
  });

  it('hover-card content oracles bind toFrontierRows, not a richer fixture than the mapper', () => {
    const frontier = readFileSync(join(here, 'families/frontier.test.ts'), 'utf8');
    for (const id of ['T181', 'T184', 'T326', 'T168']) {
      expect(frontier, id + ' must call toFrontierRows').toMatch(/toFrontierRows/);
      expect(frontier, id + ' must appear as an itOracle').toMatch(new RegExp("itOracle\\(('" + id + "'|\\[[^\\]]*" + id + ')'));
    }
    const app = readFileSync(join(here, '../App.tsx'), 'utf8');
    expect(app).toMatch(/toFrontierRows/);
    expect(app).not.toMatch(/id: t\.id \|\| ''/);
  });

  it('catalog still has a families/ file per row', () => {
    const files = new Set(readdirSync(join(here, 'families')));
    for (const f of CATALOG) {
      expect(files.has(f.file), f.id + ' missing ' + f.file).toBe(true);
      expect(f.covers.length, f.id + ' has no covers').toBeGreaterThan(0);
    }
  });
});
