// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { displayRows } from '../../conversation/display';
import { ensureFenceNewlines, parseAssistantMarkdown } from '../../conversation/markdown';
import {
  bubblePaintsMarkdown,
  paintUserHTML,
  turnOriginOf,
} from '../../conversation/paint';
import { joinAssistantSegments } from '../../conversation/stream';
import { createSession } from '../../conversation/streamingMarkdown';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('markdown'), () => {
  itOracle('T74', 'fenced code is highlighted HTML, not a raw dump', () => {
    const html = parseAssistantMarkdown('```js\nconst x = 1;\n```');
    expect(html).toMatch(/<pre|<code/i);
    expect(html).toContain('const x = 1');
  });

  itOracle('T59', '```mermaid fences render as diagrams, not raw source', () => {
    const html = parseAssistantMarkdown('```mermaid\ngraph TD;\n  A[Start] --> B{OK?};\n```');
    // Seal path tags the fence as a mermaid language node (vanilla then
    // mounts SVG). Not leftover raw ```mermaid prose, and not a js fence.
    expect(html).toMatch(/language-mermaid/i);
    expect(html).toMatch(/<pre|<code/i);
    expect(html).toMatch(/graph TD/);
    expect(html).not.toContain('```mermaid');
    const js = parseAssistantMarkdown('```js\nconst x = 1;\n```');
    expect(js).not.toMatch(/language-mermaid/i);
  });

  itOracle('T147', 'coalesce inserts a newline before a fence opener at a segment boundary', () => {
    expect(joinAssistantSegments('Intro.', '```cpp\nint x;\n```')).toBe(
      'Intro.\n\n```cpp\nint x;\n```',
    );
    expect(ensureFenceNewlines("Here's a snippet:```cpp\nint x;\n```")).toBe(
      "Here's a snippet:\n\n```cpp\nint x;\n```",
    );
    const html = parseAssistantMarkdown(joinAssistantSegments('See:', '```js\nconst x = 1;\n```'));
    expect(html).toMatch(/<pre|<code/i);
    expect(html).toContain('const x = 1');
    expect(html).not.toMatch(/See:```/);
  });

  itOracle('T150', 'streaming emphasis is complete structure, not raw asterisks', () => {
    const root = document.createElement('div');
    const session = createSession(root);
    expect(session).not.toBeNull();
    session!.writeFull('**tex', ensureFenceNewlines);
    session!.writeFull('**text', ensureFenceNewlines);
    session!.writeFull('**text**', ensureFenceNewlines);
    expect(root.innerHTML).toMatch(/<strong>text<\/strong>/);
    expect(root.innerHTML).not.toContain('**text**');
    expect(root.textContent).not.toContain('**');
    expect(root.textContent).toMatch(/text/);
    session!.writeFull('**text** done.', ensureFenceNewlines);
    expect(root.innerHTML).toMatch(/<strong>text<\/strong>/);
    expect(root.textContent).toMatch(/done/);
  });

  itOracle('T381', 'agent reports render as markdown; only owner text is verbatim', () => {
    const report = '**Commit:** `bec51ca`';
    expect(turnOriginOf({ turn_origin: 'agent' })).toBe('agent');
    expect(turnOriginOf({ type: 'user' })).toBe('owner');
    expect(bubblePaintsMarkdown('user', 'agent')).toBe(true);
    expect(bubblePaintsMarkdown('user', 'owner')).toBe(false);
    expect(bubblePaintsMarkdown('jevons', 'owner')).toBe(true);

    const agentHtml = paintUserHTML(report, 'agent');
    expect(agentHtml).toMatch(/<strong>Commit:<\/strong>/);
    expect(agentHtml).toMatch(/<code>bec51ca<\/code>/);

    const ownerHtml = paintUserHTML('why is **this** literal?', 'owner');
    expect(ownerHtml).toContain('**this**');
    expect(ownerHtml).not.toMatch(/<strong>/);

    const rows = displayRows([
      { type: 'user', message: { role: 'user', content: 'why is **this** literal?' } },
      { type: 'user', turn_origin: 'agent', message: { role: 'user', content: report } },
    ]);
    expect(rows.map((r) => r.origin)).toEqual(['owner', 'agent']);
    expect(rows[1].text).toBe(report);
  });
});
