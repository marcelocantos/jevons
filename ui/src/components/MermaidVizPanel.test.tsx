// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
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
});
