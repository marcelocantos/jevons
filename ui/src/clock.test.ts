// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it, afterEach } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { date, isFrozen, now, reset, setNow } from './clock';

afterEach(() => {
  reset();
});

describe('clock', () => {
  it('freezes now and date', () => {
    setNow(1_700_000_000_000);
    expect(now()).toBe(1_700_000_000_000);
    expect(date().getTime()).toBe(1_700_000_000_000);
    expect(isFrozen()).toBe(true);
    reset();
    expect(isFrozen()).toBe(false);
  });

  it('ui/src reads wall time only through clock.ts', () => {
    const root = dirname(fileURLToPath(import.meta.url));
    const hits: string[] = [];
    function walk(dir: string) {
      for (const ent of readdirSync(dir, { withFileTypes: true })) {
        if (ent.name === 'clock.ts') continue;
        if (ent.name.includes('.test.')) continue;
        const p = join(dir, ent.name);
        if (ent.isDirectory()) {
          walk(p);
          continue;
        }
        if (!/\.(ts|tsx)$/.test(ent.name)) continue;
        const text = readFileSync(p, 'utf8');
        if (/\bDate\.now\s*\(/.test(text) || /\bnew Date\s*\(\s*\)/.test(text)) hits.push(p);
      }
    }
    walk(root);
    expect(hits, 'Date.now / new Date() only in clock.ts').toEqual([]);
  });
});
