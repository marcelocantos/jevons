// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { graphBodyHtml } from './MermaidVizPanel';

describe('frontier graph pack', () => {
  it('draws each component as its own diagram', () => {
    const html = graphBodyHtml([
      { id: 'c0', mermaid: 'flowchart TB\n  A-->B' },
      { id: 'c1', title: 'orphans', mermaid: 'flowchart TB\n  C-->D' },
    ]);
    const fences = html.match(/language-mermaid/g) || [];
    expect(fences).toHaveLength(2);
    expect(html).toContain('mvp-pack-block');
    expect(html).toContain('orphans');
    expect(html).not.toContain('flowchart TB\n  A-->B\nflowchart');
  });

  it('lets a tall component keep its height instead of clipping', () => {
    const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../cockpit.css'), 'utf8');
    const pack = css.slice(css.indexOf('#mermaid-viz-panel .mvp-body.mvp-pack {'), css.indexOf('#mermaid-viz-panel .mvp-body.mvp-pack.mvp-pack-scaled'));
    const block = css.slice(css.indexOf('#mermaid-viz-panel .mvp-pack-block {'), css.indexOf('#mermaid-viz-panel .mvp-pack-block .mvp-pack-title'));
    expect(pack).toMatch(/grid-auto-rows:\s*max-content/);
    expect(pack).toMatch(/align-items:\s*start/);
    expect(block).toMatch(/overflow:\s*visible/);
    expect(block).toMatch(/min-height:\s*min-content/);
  });
});
