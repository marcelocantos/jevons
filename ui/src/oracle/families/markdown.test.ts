// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { ensureFenceNewlines, parseAssistantMarkdown } from '../../conversation/markdown';
import { bubblePaintsMarkdown, paintUserHTML } from '../../conversation/paint';
import { coalesceAssistantText, joinAssistantTexts } from '../../conversation/stream';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('markdown'), () => {
  itOracle('T74', 'fenced code is highlighted HTML, not a raw dump', () => {
    const html = parseAssistantMarkdown('```js\nconst x = 1;\n```');
    expect(html).toMatch(/<pre|<code/i);
    expect(html).toContain('const x = 1');
  });

  itOracle('T145', 'display-time fence normalize still lifts a smushed opener', () => {
    expect(ensureFenceNewlines('see:```js\nconst x = 1\n```')).toBe('see:\n\n```js\nconst x = 1\n```');
  });

  itOracle('T147', 'join-time coalesce inserts a newline before a fence opener at a segment boundary', () => {
    const out = coalesceAssistantText('Intro.', '```cpp\ncode\n```');
    expect(out).toBe('Intro.\n\n```cpp\ncode\n```');
    expect(out).not.toContain('.```');
    expect(joinAssistantTexts(['See:', '```js\n1\n```'])).toBe('See:\n\n```js\n1\n```');
  });

  itOracle('T645', 'React join helper separates fence and sentence ACP edges, not T145 display repair', () => {
    const fence = coalesceAssistantText('finish-report.', '```jevons\nk: v\n```');
    expect(fence).toBe('finish-report.\n\n```jevons\nk: v\n```');
    expect(fence).not.toContain('.```');
    const sentence = coalesceAssistantText('idle.', 'The reminted');
    expect(sentence).toBe('idle. The reminted');
    expect(sentence).not.toContain('idle.The');
    expect(coalesceAssistantText('Hello', '.')).toBe('Hello.');
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
