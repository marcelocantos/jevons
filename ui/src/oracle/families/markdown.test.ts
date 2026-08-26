// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { ensureFenceNewlines, parseAssistantMarkdown } from '../../conversation/markdown';
import { bubblePaintsMarkdown, paintUserHTML } from '../../conversation/paint';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('markdown'), () => {
  itOracle('T74', 'fenced code is highlighted HTML, not a raw dump', () => {
    const html = parseAssistantMarkdown('```js\nconst x = 1;\n```');
    expect(html).toMatch(/<pre|<code/i);
    expect(html).toContain('const x = 1');
  });

  itOracle('T147', 'coalesce inserts a newline before a fence opener at a segment boundary', () => {
    expect(ensureFenceNewlines('see:```js\nconst x = 1\n```')).toBe('see:\n\n```js\nconst x = 1\n```');
  });

  itOracle('T150', 'streaming emphasis is complete structure, not raw asterisks', () => {
    const html = parseAssistantMarkdown('**claudia-po**');
    expect(html).toMatch(/<strong>claudia-po<\/strong>/);
    expect(html).not.toContain('**claudia-po**');
  });

  itOracle('T381', 'agent reports render as markdown; only owner text is verbatim', () => {
    expect(bubblePaintsMarkdown('assistant', 'owner')).toBe(true);
    expect(bubblePaintsMarkdown('user', 'owner')).toBe(false);
    expect(bubblePaintsMarkdown('user', 'agent')).toBe(true);
    expect(paintUserHTML('**stars**', 'owner')).toContain('**stars**');
    expect(paintUserHTML('**stars**', 'agent')).toMatch(/<strong>stars<\/strong>/);
  });

  itOracle.skip('T59', '```mermaid fences render as diagrams, not raw source', 'journey is the arbiter (J23)');
});
