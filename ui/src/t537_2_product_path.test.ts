// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

function walk(dir: string, acc: string[] = []): string[] {
  for (const ent of readdirSync(dir, { withFileTypes: true })) {
    if (ent.name === 'node_modules' || ent.name === 'dist') continue;
    const p = join(dir, ent.name);
    if (ent.isDirectory()) walk(p, acc);
    else if (/\.(ts|tsx|js|jsx)$/.test(ent.name)) acc.push(p);
  }
  return acc;
}

describe('T537.2 product path', () => {
  const root = join(dirname(fileURLToPath(import.meta.url)), '..');

  it('ui/ does not call inspect_subscribe', () => {
    const hits: string[] = [];
    for (const file of walk(join(root, 'src'))) {
      if (file.includes('.test.')) continue;
      const text = readFileSync(file, 'utf8');
      if (text.includes('inspect_subscribe')) hits.push(file);
    }
    expect(hits, 'inspect_subscribe is the old sidebar hydrate').toEqual([]);
  });

  it('default sidebar tab is Frontier then Transcript', () => {
    const panel = readFileSync(join(root, 'src/components/SidebarPanel.tsx'), 'utf8');
    const fi = panel.indexOf("id: 'frontier'");
    const ti = panel.indexOf("id: 'transcript'");
    expect(fi).toBeGreaterThanOrEqual(0);
    expect(ti).toBeGreaterThan(fi);
  });

  it('reload pin keeps follow across measure growth and hydrates a PageUp band (🎯T494.1.3)', () => {
    const paint = readFileSync(join(root, 'src/components/AgentTranscript.tsx'), 'utf8');
    expect(paint).toMatch(/followAfterScroll/);
    expect(paint).toMatch(/nextHydrateOverscan/);
    expect(paint).toMatch(/measuredSuffixFromEnd/);
    expect(paint).toMatch(/HYDRATE_OVERSCAN_MAX/);
    const hydrate = readFileSync(join(root, 'src/transcript/hydrateOverscan.ts'), 'utf8');
    expect(hydrate).toMatch(/function standingOverscan/);
    expect(paint).toMatch(/hydrateSettled/);
    expect(paint).toMatch(/second HaloProse/);
    const pin = readFileSync(join(root, 'src/transcript/followPin.ts'), 'utf8');
    expect(pin).toMatch(/heightGrew/);
    expect(pin).toMatch(/function followAfterScroll/);
  });

  it('ui/ does not import web/', () => {
    const hits: string[] = [];
    for (const file of walk(join(root, 'src'))) {
      if (file.includes('.test.')) continue;
      const text = readFileSync(file, 'utf8');
      if (/from\s+['"][^'"]*web\//.test(text) || /require\(\s*['"][^'"]*web\//.test(text)) {
        hits.push(file);
      }
    }
    expect(hits, 'new UI must not link the old web/ tree').toEqual([]);
  });
});
