// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement } from 'react';
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { userTurn } from '../oracle/fixtures';
import { AgentTranscript } from './AgentTranscript';

class FakeResizeObserver {
  static callbacks = new Map<Element, Set<() => void>>();
  constructor(private cb: () => void) {}
  observe(el: Element) {
    const callbacks = FakeResizeObserver.callbacks.get(el) || new Set();
    callbacks.add(this.cb);
    FakeResizeObserver.callbacks.set(el, callbacks);
  }
  unobserve(el: Element) { FakeResizeObserver.callbacks.get(el)?.delete(this.cb); }
  disconnect() {
    for (const callbacks of FakeResizeObserver.callbacks.values()) callbacks.delete(this.cb);
  }
  static fire(el: Element) {
    for (const cb of FakeResizeObserver.callbacks.get(el) || []) cb();
  }
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  FakeResizeObserver.callbacks.clear();
});

it.each([
  { viewport: 'iPad-sized split', clientHeight: 900 },
  { viewport: 'phone', clientHeight: 500 },
])('keeps the viewport over the old row after a page and late measurements on $viewport (T1060)', ({ clientHeight }) => {
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  const onPageOlder = vi.fn();
  const old = Array.from({ length: 30 }, (_, i) => userTurn(`loaded ${i}`));
  const page = Array.from({ length: 50 }, (_, i) => userTurn(`older ${i}`));
  const props = (frames: unknown[], start: number, older: number) => createElement(AgentTranscript, {
    name: 'jevons', frames, meta: { start, older, total: 200, following: false }, ready: true, onPageOlder,
  });
  const view = render(props(old, 100, 100));
  const el = view.container.querySelector('#messages') as HTMLElement;
  const canvas = view.container.querySelector('#messages-canvas') as HTMLElement;
  let height = 3000;
  Object.defineProperty(el, 'scrollHeight', { get: () => height, configurable: true });
  Object.defineProperty(el, 'clientHeight', { value: clientHeight, configurable: true });
  el.scrollTop = 0;
  fireEvent.scroll(el);
  expect(onPageOlder).toHaveBeenCalledTimes(1);

  // A quick response expands the virtual canvas while the browser stays at
  // scrollTop=0 (overflow-anchor:none). The old first row must not jump.
  height = 6000;
  view.rerender(props([...page, ...old], 50, 50));
  expect(el.scrollTop).toBe(3000);
  fireEvent.scroll(el); // delayed programmatic scroll from anchoring, not a gesture
  expect(onPageOlder).toHaveBeenCalledTimes(1); // no automatic torrent

  // Rows can re-measure one frame after the prepend, including on iPad's
  // narrow split pane. The adjustment must continue after first paint.
  height = 6150;
  act(() => FakeResizeObserver.fire(canvas));
  expect(el.scrollTop).toBe(3150);

  // A new gesture releases the anchor; the owner can scroll back to top and
  // fetch the next page, rather than swiping against an inert top boundary.
  fireEvent.wheel(el, { deltaY: -500 });
  el.scrollTop = 0;
  fireEvent.scroll(el);
  expect(onPageOlder).toHaveBeenCalledTimes(2);
});

// A page shorter than the near-top threshold must not auto-page just because
// the browser emitted the anchor's synthetic scroll after the response.
it('does not cascade after a tiny prepend until a new PageUp gesture (T1060)', () => {
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  const onPageOlder = vi.fn();
  const old = Array.from({ length: 2 }, (_, i) => userTurn(`loaded ${i}`));
  const props = (frames: unknown[], start: number) => createElement(AgentTranscript, {
    name: 'jevons', frames, meta: { start, older: start, total: 200, following: false }, ready: true, onPageOlder,
  });
  const view = render(props(old, 100));
  const el = view.container.querySelector('#messages') as HTMLElement;
  let height = 1000;
  Object.defineProperty(el, 'scrollHeight', { get: () => height, configurable: true });
  Object.defineProperty(el, 'clientHeight', { value: 500, configurable: true });
  el.scrollTop = 0;
  fireEvent.scroll(el);
  expect(onPageOlder).toHaveBeenCalledTimes(1);
  height = 1020;
  view.rerender(props([userTurn('older'), ...old], 99));
  expect(el.scrollTop).toBe(20);
  fireEvent.scroll(el);
  expect(onPageOlder).toHaveBeenCalledTimes(1);
  fireEvent(el, new Event('jevons-page-older')); // explicit PageUp gesture
  expect(onPageOlder).toHaveBeenCalledTimes(2);
});

// A later ordinary tail append or an expand is not a prepend measurement.
it('does not claim unrelated growth after a new live row or owner gesture (T1060)', () => {
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  const old = [userTurn('loaded 0'), userTurn('loaded 1')];
  const props = (frames: unknown[], start: number) => createElement(AgentTranscript, {
    name: 'jevons', frames, meta: { start, older: start, total: 200, following: false }, ready: true, onPageOlder: () => {},
  });
  const view = render(props(old, 100));
  const el = view.container.querySelector('#messages') as HTMLElement;
  const canvas = view.container.querySelector('#messages-canvas') as HTMLElement;
  let height = 1000;
  Object.defineProperty(el, 'scrollHeight', { get: () => height, configurable: true });
  Object.defineProperty(el, 'clientHeight', { value: 500, configurable: true });
  el.scrollTop = 0;
  fireEvent.scroll(el);
  height = 1200;
  view.rerender(props([userTurn('older'), ...old], 99));
  expect(el.scrollTop).toBe(200);
  // Live append while history is open: the old anchor must be released.
  height = 1300;
  view.rerender(props([userTurn('older'), ...old, userTurn('new live tail')], 99));
  act(() => FakeResizeObserver.fire(canvas));
  expect(el.scrollTop).toBe(200);

  // A second history page starts a fresh anchor; the next gesture releases
  // it before an unrelated expand.
  el.scrollTop = 0;
  fireEvent.scroll(el);
  height = 1550;
  view.rerender(props([userTurn('even older'), userTurn('older'), ...old, userTurn('new live tail')], 98));
  expect(el.scrollTop).toBe(250);
  fireEvent.wheel(el, { deltaY: 50 });
  height = 1750;
  act(() => FakeResizeObserver.fire(canvas));
  expect(el.scrollTop).toBe(250);
});

it('an empty page releases pending anchor on next explicit PageUp (T1060)', () => {
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  const onPageOlder = vi.fn();
  const old = [userTurn('loaded')];
  const props = (start: number) => createElement(AgentTranscript, {
    name: 'jevons', frames: old, meta: { start, older: start, total: 200, following: false }, ready: true, onPageOlder,
  });
  const view = render(props(100));
  const el = view.container.querySelector('#messages') as HTMLElement;
  Object.defineProperty(el, 'scrollHeight', { value: 1000, configurable: true });
  Object.defineProperty(el, 'clientHeight', { value: 500, configurable: true });
  el.scrollTop = 0;
  fireEvent.scroll(el);
  expect(onPageOlder).toHaveBeenCalledTimes(1);
  view.rerender(props(99)); // empty page: cursor advances, no new rows
  fireEvent(el, new Event('jevons-page-older'));
  expect(onPageOlder).toHaveBeenCalledTimes(2);
});

it('settled history stops shifting when an unrelated row expands without a gesture (T1060)', () => {
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  const old = [userTurn('loaded 0'), userTurn('loaded 1')];
  const props = (frames: unknown[], start: number) => createElement(AgentTranscript, {
    name: 'jevons', frames, meta: { start, older: start, total: 200, following: false }, ready: true, onPageOlder: () => {},
  });
  const view = render(props(old, 100));
  const el = view.container.querySelector('#messages') as HTMLElement;
  const canvas = view.container.querySelector('#messages-canvas') as HTMLElement;
  let height = 1000;
  Object.defineProperty(el, 'scrollHeight', { get: () => height, configurable: true });
  Object.defineProperty(el, 'clientHeight', { value: 500, configurable: true });
  vi.useFakeTimers();
  try {
    el.scrollTop = 0;
    fireEvent.scroll(el);
    height = 1200;
    view.rerender(props([userTurn('older'), ...old], 99));
    expect(el.scrollTop).toBe(200);
    height = 1250;
    act(() => FakeResizeObserver.fire(canvas)); // late measurement belongs to page
    expect(el.scrollTop).toBe(250);
    act(() => vi.advanceTimersByTime(201)); // canvas has settled
    height = 1350;
    act(() => FakeResizeObserver.fire(canvas)); // unrelated bubble expand
    expect(el.scrollTop).toBe(250);
  } finally {
    vi.useRealTimers();
  }
});
