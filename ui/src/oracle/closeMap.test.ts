// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { censusVisible } from './census';
import { CLOSE, CONTENT_MAPPER_IDS, closeById } from './closeMap';

const here = dirname(fileURLToPath(import.meta.url));

describe('T540.3 close map', () => {
  it('assigns every visible census id exactly once', () => {
    const want = censusVisible().map((r) => r.id).sort();
    const have = CLOSE.map((r) => r.id).sort();
    expect(have).toEqual(want);
    expect(new Set(have).size).toBe(have.length);
  });

  it('every skip/journey has a named residual; mapper set is the card composition class', () => {
    expect([...CONTENT_MAPPER_IDS].sort()).toEqual(['T168', 'T181', 'T184', 'T326']);
    for (const row of CLOSE) {
      expect(row.reason.trim().length, row.id + ' empty reason').toBeGreaterThan(8);
      if (row.kind === 'skip' || row.kind === 'journey') {
        expect(row.reason, row.id).toMatch(/journey is the arbiter|not ported|named residual|mention-only|daily path|live fail/);
      }
    }
  });

  it('passing hover-card content oracles still import toFrontierRows', () => {
    const frontier = readFileSync(join(here, 'families/frontier.test.ts'), 'utf8');
    for (const id of CONTENT_MAPPER_IDS) {
      expect(frontier).toMatch(/toFrontierRows/);
      expect(frontier).toContain(id);
    }
  });

  it('journey family files do not pass by grepping react_paint.js', () => {
    const dir = join(here, 'families');
    for (const name of readdirSync(dir)) {
      if (!name.startsWith('journey-') || !name.endsWith('.test.ts')) continue;
      const src = readFileSync(join(dir, name), 'utf8');
      expect(src, name + ' must not source-grep the journey suite as a passing oracle').not.toMatch(
        /itOracle\(\s*\[/,
      );
      expect(src, name).toMatch(/itOracle\.skip/);
    }
  });
});

describe('closeById', () => {
  it('looks up T184 as mapper/frontier', () => {
    const row = closeById().get('T184');
    expect(row?.kind).toBe('mapper');
    expect(row?.pocket).toBe('frontier');
  });
});
