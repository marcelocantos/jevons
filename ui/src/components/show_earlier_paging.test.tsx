// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement } from 'react';
import { cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { assistantTool, userTurn } from '../oracle/fixtures';
import { AUTO_FILL_MAX_PAGES } from '../conversation/page';
import { AgentTranscript } from './AgentTranscript';

// One "Show earlier" click asks for more visible history. A page made only
// of events that fold into existing rows must not end the request: the
// button looked dead on 2026-10-23 because a 250-event page of machine-turn
// notices added nothing visible.
beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(100);
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(800);
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

it('keeps paging until a page adds a visible row', () => {
  const onPageOlder = vi.fn();
  let older = 500;
  const props = (frames: unknown[], olderPage: { loading: boolean } | null) =>
    createElement(AgentTranscript, {
      name: 'jv-worker',
      frames,
      meta: { start: older, older, total: 501 },
      ready: true,
      following: true,
      onPageOlder,
      olderPage: olderPage as never,
    });
  const one = [userTurn('latest')];
  const { getByText, queryByText, rerender } = render(props(one, null));
  rerender(props(one, null)); // the scroller ref measures on the second render

  // The pane is short, so it fills itself without a click, up to the bound;
  // only then does the owner get the button.
  for (let i = 1; i < AUTO_FILL_MAX_PAGES; i++) {
    rerender(props(one, { loading: true }));
    older -= 10; // each page lands and moves the window start
    rerender(props(one, null));
  }
  rerender(props(one, { loading: true }));
  older -= 10;
  rerender(props(one, null));
  expect(onPageOlder).toHaveBeenCalledTimes(AUTO_FILL_MAX_PAGES);
  expect(queryByText('Show earlier')).not.toBeNull();
  const base = AUTO_FILL_MAX_PAGES;

  fireEvent.click(getByText('Show earlier'));
  expect(onPageOlder).toHaveBeenCalledTimes(base + 1);

  // The page arrives and adds no visible row: page again.
  rerender(props(one, { loading: true }));
  rerender(props(one, null));
  expect(onPageOlder).toHaveBeenCalledTimes(base + 2);

  // This page only grows the steps marker (a tool call): still nothing the
  // owner can read, so page again.
  const steps = [assistantTool('Bash'), ...one];
  rerender(props(one, { loading: true }));
  rerender(props(steps, null));
  expect(onPageOlder).toHaveBeenCalledTimes(base + 3);

  // This page adds a message: stop.
  rerender(props(steps, { loading: true }));
  rerender(props([userTurn('older'), ...steps], null));
  expect(onPageOlder).toHaveBeenCalledTimes(base + 3);
});

it('an opened short transcript pages older history by itself, with no click', () => {
  const onPageOlder = vi.fn();
  const props = (older: number, olderPage: { loading: boolean } | null) =>
    createElement(AgentTranscript, {
      name: 'jv-worker',
      frames: [userTurn('latest')],
      meta: { start: older, older, total: older + 1 },
      ready: true,
      following: true,
      onPageOlder,
      olderPage: olderPage as never,
    });
  const { rerender } = render(props(500, null));
  rerender(props(500, null));
  expect(onPageOlder).toHaveBeenCalledTimes(1);
  rerender(props(450, { loading: true }));
  rerender(props(450, null));
  expect(onPageOlder).toHaveBeenCalledTimes(2);
  // History runs out: nothing more to fetch.
  rerender(props(0, { loading: true }));
  rerender(props(0, null));
  expect(onPageOlder).toHaveBeenCalledTimes(2);
});

it('keeps filling after a page lands rows and its scroll anchor is applied', () => {
  const onPageOlder = vi.fn();
  let frames: unknown[] = [userTurn('latest')];
  let older = 500;
  const props = (olderPage: { loading: boolean } | null) =>
    createElement(AgentTranscript, {
      name: 'jv-worker',
      frames,
      meta: { start: older, older, total: 501 },
      ready: true,
      following: true,
      onPageOlder,
      olderPage: olderPage as never,
    });
  const { rerender } = render(props(null));
  rerender(props(null));
  expect(onPageOlder).toHaveBeenCalledTimes(1);
  for (let i = 1; i <= 3; i++) {
    rerender(props({ loading: true }));
    frames = [userTurn('older ' + i), ...frames];
    older -= 10;
    rerender(props(null));
    rerender(props(null));
    expect(onPageOlder).toHaveBeenCalledTimes(i + 1);
  }
});

it('at the top of history, each wheel up asks for the next page even when the last page added no height', () => {
  vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(3000);
  const onPageOlder = vi.fn();
  let older = 5000;
  const frames = [userTurn('latest')];
  const props = (olderPage: { loading: boolean } | null) =>
    createElement(AgentTranscript, {
      name: 'jv-worker',
      frames,
      meta: { start: older, older, total: 5001, following: false },
      ready: true,
      following: false,
      onPageOlder,
      olderPage: olderPage as never,
    });
  const { container, rerender } = render(props(null));
  rerender(props(null));
  const scroller = container.firstElementChild as HTMLElement;
  scroller.scrollTop = 0; // the owner has scrolled to the top
  fireEvent.scroll(scroller);
  expect(onPageOlder).toHaveBeenCalledTimes(1);

  // The page lands with nothing visible (folded machine turns). No gesture,
  // no further page (T1060: no automatic torrent).
  rerender(props({ loading: true }));
  older -= 250;
  rerender(props(null));
  expect(onPageOlder).toHaveBeenCalledTimes(1);

  // A wheel up at scrollTop 0 fires no scroll event; it pages by itself.
  fireEvent.wheel(scroller, { deltaY: -400 });
  expect(onPageOlder).toHaveBeenCalledTimes(2);
  // Wheeling down does not.
  rerender(props({ loading: true }));
  older -= 250;
  rerender(props(null));
  fireEvent.wheel(scroller, { deltaY: 400 });
  expect(onPageOlder).toHaveBeenCalledTimes(2);
});
