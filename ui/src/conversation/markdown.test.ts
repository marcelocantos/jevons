// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { ensureFenceNewlines, parseAssistantMarkdown } from './markdown';

describe('parseAssistantMarkdown', () => {
  it('keeps GFM default: single newline is not <br>', () => {
    const html = parseAssistantMarkdown('alpha\nbeta');
    expect(html).not.toMatch(/<br/i);
    expect(html).toMatch(/alpha/);
    expect(html).toMatch(/beta/);
  });

  it('turns two-space line ends into <br> (golden hard breaks)', () => {
    const html = parseAssistantMarkdown('alpha  \nbeta');
    expect(html).toMatch(/<br/i);
  });

  it('paints **strong** and splits on blank lines', () => {
    const html = parseAssistantMarkdown('hello **jevons**\n\nnext');
    expect(html).toMatch(/<strong>jevons<\/strong>/);
    expect((html.match(/<p>/g) || []).length).toBeGreaterThanOrEqual(2);
  });

  it('inserts a blank line before a smushed fence (🎯T145)', () => {
    expect(ensureFenceNewlines('see:```js\nconst x = 1\n```')).toBe(
      'see:\n\n```js\nconst x = 1\n```',
    );
  });
});
