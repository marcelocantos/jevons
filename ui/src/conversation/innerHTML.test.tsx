// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { useInnerHTML } from './innerHTML';

function Stable(props: { html: string; tick: number }) {
  const inner = useInnerHTML(props.html);
  return <div className="msg-body" data-tick={props.tick} dangerouslySetInnerHTML={inner} />;
}

function Inline(props: { html: string; tick: number }) {
  return <div className="msg-body" data-tick={props.tick} dangerouslySetInnerHTML={{ __html: props.html }} />;
}

// On 2026-10-01 the cockpit rebuilt every transcript body on every poll
// (~780 nodes per 10 s), clobbering the owner's selection and flickering the
// bubbles, though nothing had changed.
describe('useInnerHTML', () => {
  afterEach(cleanup);
  const html = '<p>What&#39;s up with Claude usage reporting?</p>';

  it('keeps an unchanged body’s nodes across a re-render, so a selection survives', () => {
    const { container, rerender } = render(<Stable html={html} tick={1} />);
    const p = container.querySelector('p');
    rerender(<Stable html={html} tick={2} />);
    expect(container.querySelector('p')).toBe(p);
  });

  it('control: an inline {__html} object is rewritten by React on every re-render', () => {
    const { container, rerender } = render(<Inline html={html} tick={1} />);
    const p = container.querySelector('p');
    rerender(<Inline html={html} tick={2} />);
    expect(container.querySelector('p')).not.toBe(p);
  });

  it('no component passes an inline {__html} object', () => {
    const sources = import.meta.glob('../**/*.tsx', { query: '?raw', import: 'default', eager: true }) as Record<string, string>;
    const offenders = Object.entries(sources)
      .filter(([path, src]) => !path.endsWith('.test.tsx') && /dangerouslySetInnerHTML=\{\{/.test(src))
      .map(([path]) => path);
    expect(offenders).toEqual([]);
  });
});
