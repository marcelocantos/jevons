// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { CATALOG } from './catalog';

describe('oracle catalog', () => {
  const here = dirname(fileURLToPath(import.meta.url));
  const files = new Set(readdirSync(join(here, 'families')));

  it('every family has a unique id and a families/ file', () => {
    const ids = CATALOG.map((f) => f.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const f of CATALOG) {
      expect(files.has(f.file), f.id + ' missing ' + f.file).toBe(true);
      expect(f.covers.length, f.id + ' has no covers').toBeGreaterThan(0);
    }
  });

  it('hermetic families remain; journeys cover the chrome pack', () => {
    const hermetic = CATALOG.filter((f) => f.layer === 'hermetic');
    const journey = CATALOG.filter((f) => f.layer === 'journey');
    expect(hermetic.length).toBeGreaterThanOrEqual(8);
    expect(journey.length).toBeGreaterThanOrEqual(8);
    expect(journey.length).toBeLessThanOrEqual(12);
  });
});
