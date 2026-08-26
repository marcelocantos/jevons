// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { parseAssistantMarkdown } from '../../conversation/markdown';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('markdown'), () => {
  itOracle('T74', 'fenced code is highlighted HTML, not a raw dump', () => {
    const html = parseAssistantMarkdown('```js\nconst x = 1;\n```');
    expect(html).toMatch(/<pre|<code/i);
    expect(html).toContain('const x = 1');
  });

  itOracle.todo('T59', '```mermaid fences render as diagrams, not raw source');
  itOracle.todo('T147', 'coalesce inserts a newline before a fence opener at a segment boundary');
  itOracle.todo('T150', 'streaming emphasis is complete structure, not raw asterisks');
  itOracle.todo('T381', 'agent reports render as markdown; only owner text is verbatim');
});
