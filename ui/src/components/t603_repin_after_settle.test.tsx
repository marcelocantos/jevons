// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createElement } from 'react';
import { render, cleanup } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { userTurn } from '../oracle/fixtures';
import { AgentTranscript } from './AgentTranscript';

const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'AgentTranscript.tsx'), 'utf8');

// 🎯T603. Measured live on 2026-08-31 by driving the running cockpit and
// sending an owner message while a large report landed:
//
//   +82s  fb=0    dh=12640  dtop=12640   rows 93
//   +84s  fb=345  dh=2117   dtop=1772    rows 91   <- followed 1772 of 2117
//   +86s  fb=345  dh=0                   rows 91
//
// Follow was correctly RETAINED — 345 <= 2117 is growth, not a leave, so
// this is not the 🎯T587 detach. The transcript simply stopped 345px short
// and stayed there: a re-fold collapsed 93 rows into 91 and re-measured
// AFTER the pin ran, and since no further growth arrived, no further pin
// ran. The owner comes back to a view a bubble or two off the live end,
// which is precisely the complaint 🎯T587 was meant to end.
//
// jsdom has no layout, so the geometry is driven by hand: the point under
// test is that the settle is OBSERVED at all, and that seeing it closes
// the gap.
// TanStack Virtual constructs a ResizeObserver of its own, so the stub
// cannot simply keep "the last callback" — it keys them by observed
// target, and the test fires the one watching the canvas.
class FakeResizeObserver {
  static watchers: { cb: () => void; target: Element }[] = [];
  constructor(private cb: () => void) {}
  observe(target: Element) {
    FakeResizeObserver.watchers.push({ cb: this.cb, target });
  }
  unobserve(target: Element) {
    FakeResizeObserver.watchers = FakeResizeObserver.watchers.filter((w) => w.target !== target);
  }
  disconnect() {
    FakeResizeObserver.watchers = FakeResizeObserver.watchers.filter((w) => w.cb !== this.cb);
  }
  static fireFor(target: Element | null) {
    for (const w of FakeResizeObserver.watchers) if (w.target === target) w.cb();
  }
  static watching(target: Element | null) {
    return FakeResizeObserver.watchers.some((w) => w.target === target);
  }
}

function geometry(el: HTMLElement, scrollHeight: number, clientHeight: number) {
  Object.defineProperty(el, 'scrollHeight', { value: scrollHeight, configurable: true });
  Object.defineProperty(el, 'clientHeight', { value: clientHeight, configurable: true });
}

afterEach(() => {
  cleanup();
  FakeResizeObserver.watchers = [];
});

// The scroller and the canvas it contains: the canvas is what changes
// height when rows re-measure, and what the fix observes.
function panes(container: HTMLElement) {
  const el = container.querySelector('#messages') as HTMLElement;
  return { el, canvas: el.firstElementChild?.nextElementSibling ?? el.firstElementChild };
}

it('re-pins when a late re-measure leaves the transcript short of the end (T603)', () => {
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  const { container } = render(
    createElement(AgentTranscript, {
      name: 'jevons',
      frames: [userTurn('a report lands under us')],
      meta: { start: 0, older: 0, total: 1 },
      ready: true,
    }),
  );
  const { el, canvas } = panes(container);
  expect(el).toBeTruthy();

  // The observer must be watching the canvas at all. Before the fix there
  // was nothing to fire, and in compact density there still would not be
  // if it were bound by #messages-canvas rather than by ref.
  expect(FakeResizeObserver.watching(canvas)).toBe(true);

  // The settled state after the pin: 345px short, exactly as measured.
  const clientHeight = 900;
  const scrollHeight = 20000;
  geometry(el, scrollHeight, clientHeight);
  el.scrollTop = scrollHeight - clientHeight - 345;
  expect(scrollHeight - el.scrollTop - clientHeight).toBe(345);

  // The re-measure lands. Nothing else will happen: no growth, no new row.
  FakeResizeObserver.fireFor(canvas);

  expect(scrollHeight - el.scrollTop - clientHeight).toBeLessThanOrEqual(0);
});

it('leaves the owner alone when they have scrolled away (T603)', () => {
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  const { container } = render(
    createElement(AgentTranscript, {
      name: 'jevons',
      frames: [userTurn('owner is reading history')],
      meta: { start: 0, older: 0, total: 1 },
      ready: true,
      // The scroll and leave listeners are only attached when onPageOlder
      // is supplied (AgentTranscript.tsx: `if (!el || !props.onPageOlder)
      // return`). The cockpit always supplies it; a test that omits it
      // silently exercises a transcript with no leave path at all.
      onPageOlder: () => {},
    }),
  );
  const { el, canvas } = panes(container);
  geometry(el, 20000, 900);

  // The owner leaves the live end. `jevons-leave-track` is the component's
  // own documented leave path (PageUp, 🎯T494.1.3) — used here rather than
  // a synthetic scroll event because during hydrate a scroll is attributed
  // to the pin's own write, which is correct behaviour and not what this
  // case is about.
  el.scrollTop = 2000;
  el.dispatchEvent(new Event('jevons-leave-track'));

  const before = el.scrollTop;
  FakeResizeObserver.fireFor(canvas);
  expect(el.scrollTop).toBe(before);
});

// The guard rails the fix depends on, asserted against the source so a
// later edit cannot quietly remove them and leave the observer fighting
// the owner on every measurement.
it('the settle observer is guarded by follow and paging (T603)', () => {
  const effect = src.slice(src.indexOf('🎯T603'), src.indexOf('🎯T603') + 1600);
  expect(effect).toContain('new ResizeObserver');
  expect(effect).toContain('!followRef.current || pagingRef.current');
  expect(effect).toContain('canvasRef.current');
  expect(effect).not.toContain("querySelector('#messages-canvas')");
});
