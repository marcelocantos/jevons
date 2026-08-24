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

  it('mux send nack is a transcript diagnostic, not chrome or an optimistic user bubble (🎯T545.3)', () => {
    const reduce = readFileSync(join(root, 'src/conversation/reduce.ts'), 'utf8');
    expect(reduce).toMatch(/type:\s*['"]send_error['"]/);
    expect(reduce).toMatch(/error:\s*null/);
    const send = readFileSync(join(root, 'src/conversation/useConversation.ts'), 'utf8');
    expect(send).toMatch(/pendingSendRef/);
    expect(send).not.toMatch(/turn_origin:\s*['"]owner['"]/);
    const chrome = readFileSync(join(root, 'src/components/AgentInteraction.tsx'), 'utf8');
    expect(chrome).not.toMatch(/className=["']ai-err["']/);
    const paint = readFileSync(join(root, 'src/components/AgentTranscript.tsx'), 'utf8');
    expect(paint).toMatch(/kind === ['"]diagnostic['"]/);
    expect(paint).toMatch(/send-diag/);
  });

  it('standing overseer-down banner is sourced from transcript meta, not a racing mux subscribe (🎯T545.6)', () => {
    const app = readFileSync(join(root, 'src/App.tsx'), 'utf8');
    expect(app).toMatch(/onMeta=\{onJevonsMeta\}/);
    expect(app).toMatch(/degradedBannerText\(meta\)/);
    expect(app).not.toMatch(/env\.t !== ['"]meta['"]/);
    const interaction = readFileSync(join(root, 'src/components/AgentInteraction.tsx'), 'utf8');
    expect(interaction).toMatch(/props\.onMeta\?\.\(conv\.meta\)/);
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

  it('unsealed assistant uses StreamingMarkdownBody, not marked-every-token (🎯T64.4)', () => {
    const src = readFileSync(join(root, 'src/components/AgentTranscript.tsx'), 'utf8');
    expect(src).toMatch(/StreamingMarkdownBody/);
    expect(src).toMatch(/props\.sealed/);
    expect(src).not.toMatch(/textContent\s*=/);
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
