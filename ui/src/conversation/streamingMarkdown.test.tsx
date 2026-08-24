// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { render } from '@testing-library/react';
import { useRef } from 'react';
import { describe, expect, it } from 'vitest';
import { ensureFenceNewlines } from './markdown';
import { StreamingMarkdownBody } from './StreamingMarkdownBody';
import { createSession } from './streamingMarkdown';

describe('createSession', () => {
  it('closed **text** is <strong> before seal (🎯T150 / T64.4)', () => {
    const root = document.createElement('div');
    const session = createSession(root);
    expect(session).not.toBeNull();
    session!.writeFull('**tex', ensureFenceNewlines);
    session!.writeFull('**text', ensureFenceNewlines);
    session!.writeFull('**text**', ensureFenceNewlines);
    expect(root.innerHTML).toMatch(/<strong>text<\/strong>/);
    expect(root.innerHTML).not.toContain('**text**');
    expect(root.textContent).toMatch(/text/);
    // Trailing "." flushes smd's one-char pending buffer (vanilla T150 same).
    session!.writeFull('**text** done.', ensureFenceNewlines);
    expect(root.innerHTML).toMatch(/<strong>text<\/strong>/);
    expect(root.textContent).toMatch(/done/);
  });

  it('pure append writes a delta and keeps the closer', () => {
    const root = document.createElement('div');
    const session = createSession(root);
    session!.writeFull('Hello', ensureFenceNewlines);
    expect(session!.written).toBe(5);
    session!.writeFull('Hello world.', ensureFenceNewlines);
    expect(session!.written).toBe(12);
    expect(root.textContent).toMatch(/Hello world/);
  });
});

function StreamProbe(props: { text: string }) {
  const bodyRef = useRef<HTMLDivElement | null>(null);
  return <StreamingMarkdownBody text={props.text} bodyRef={bodyRef} />;
}

describe('StreamingMarkdownBody', () => {
  it('paints closed emphasis on the live bubble', () => {
    const { container, rerender } = render(<StreamProbe text="**claudia-po" />);
    rerender(<StreamProbe text="**claudia-po**" />);
    const html = container.querySelector('.msg-body')?.innerHTML || '';
    expect(html).toMatch(/<strong>claudia-po<\/strong>/);
    expect(html).not.toContain('**claudia-po**');
  });
});
