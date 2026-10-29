// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement } from 'react';
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { userTurn } from '../oracle/fixtures';
import { AgentTranscript } from './AgentTranscript';

// 🎯T1126: when the owner sends while scrolled up, the transcript must
// re-attach to the live end — the sent bubble and the reply paint in view,
// with no ↓ Latest detour left behind. AgentInteraction drives this by
// calling conv.rejoinLive() + setFollowing(true) + bumping followEpoch on
// any send that actually goes out (not merely queued); this is the unit
// AgentTranscript exposes for that reattach (the same mechanism the
// existing ↓ Latest button already uses — AgentInteraction.tsx).

function geometry(el: HTMLElement, scrollHeight: number, clientHeight: number) {
  Object.defineProperty(el, 'scrollHeight', { value: scrollHeight, configurable: true });
  Object.defineProperty(el, 'clientHeight', { value: clientHeight, configurable: true });
}

afterEach(() => cleanup());

it('owner send (followEpoch bump) re-attaches follow after a scroll-away (T1126)', async () => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  let following = true;
  const onFollowChange = vi.fn((next: boolean) => { following = next; });
  const { container, rerender } = render(
    createElement(AgentTranscript, {
      name: 'jevons',
      frames: [userTurn('owner is reading history')],
      meta: { start: 0, older: 0, total: 1 },
      ready: true,
      following,
      followEpoch: 0,
      onFollowChange,
      onPageOlder: () => {},
    }),
  );
  const el = container.querySelector('#messages') as HTMLElement;
  geometry(el, 20000, 900);
  // Let the hydrate pin's own rAF-scheduled write settle before driving a
  // real scroll — a scroll fired while that write is still in flight is
  // the pin's own write, not a user leave (see t603_repin_after_settle.ts).
  await act(async () => { await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))); });
  onFollowChange.mockClear();

  // The owner scrolls well away from the live end.
  el.scrollTop = 2000;
  fireEvent.scroll(el);
  expect(onFollowChange).toHaveBeenCalledWith(false);
  onFollowChange.mockClear();

  // The owner sends: AgentInteraction's onSend wrapper calls rejoinLive(),
  // setFollowing(true) and bumps followEpoch — reproduce that here by
  // rerendering with following=true and a bumped followEpoch, exactly as
  // the wrapper does.
  rerender(
    createElement(AgentTranscript, {
      name: 'jevons',
      frames: [userTurn('owner is reading history')],
      meta: { start: 0, older: 0, total: 1 },
      ready: true,
      following: true,
      followEpoch: 1,
      onFollowChange,
      onPageOlder: () => {},
    }),
  );

  // The epoch bump pins back to the live end and reports following again.
  expect(onFollowChange).toHaveBeenCalledWith(true);
  expect(el.scrollTop).toBe(el.scrollHeight);
});
