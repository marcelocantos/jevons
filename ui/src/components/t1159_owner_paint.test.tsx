// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement } from 'react';
import { act, cleanup, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AgentTranscript } from './AgentTranscript';

function toolFrame(i: number) {
  return {
    type: 'assistant',
    message: {
      content: [{ type: 'tool_use', id: `t${i}`, name: 'jevons_agent_list', input: {} }],
    },
  };
}

function ownerFrame(text: string) {
  return {
    type: 'user',
    turn_origin: 'owner',
    message: { role: 'user', content: [{ type: 'text', text }] },
  };
}

function assistantReply(text: string) {
  return {
    type: 'assistant',
    message: { content: [{ type: 'text', text }] },
    stop_reason: 'end_turn',
  };
}

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(72);
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: 0, y: 0, top: 0, left: 0, right: 900, bottom: 72, width: 900, height: 72, toJSON() {},
  });
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  HTMLElement.prototype.scrollTo = vi.fn();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('owner conversation after reload (🎯T1159)', () => {
  it('pages older automatically when the connect tail has no owner turn', async () => {
    const onPageOlder = vi.fn();
    const tail = Array.from({ length: 80 }, (_, i) => toolFrame(i));
    const props = {
      name: 'jevons',
      frames: tail,
      meta: { start: 9000, older: 9000, total: 9080, following: true },
      ready: true,
      onPageOlder,
    };
    const view = render(createElement(AgentTranscript, props));
    const el = view.container.querySelector('#messages') as HTMLElement;
    act(() => {
      Object.defineProperty(el, 'clientHeight', { value: 689, configurable: true });
      Object.defineProperty(el, 'scrollHeight', { get: () => 120, configurable: true });
      view.rerender(createElement(AgentTranscript, props));
    });
    await waitFor(() => expect(onPageOlder).toHaveBeenCalled());
  });

  it('keeps auto-paging when UI follow detaches but mux is still following (T1159)', async () => {
    const onPageOlder = vi.fn();
    const tail = Array.from({ length: 80 }, (_, i) => toolFrame(i));
    const props = {
      name: 'jevons',
      frames: tail,
      meta: { start: 9000, older: 9000, total: 9080, following: true },
      ready: true,
      following: false,
      onPageOlder,
    };
    render(createElement(AgentTranscript, props));
    await waitFor(() => expect(onPageOlder).toHaveBeenCalled());
  });

  // 🎯T1164 / 🎯T493.1: a hard reload lands at the live end. The owner's
  // turn is reached by folding machine turns (displayRows ownerChat), never
  // by detaching follow and scrolling away from Latest.
  it('never detaches live follow to reveal an older owner turn', async () => {
    const onFollowChange = vi.fn();
    const tail = Array.from({ length: 200 }, (_, i) => toolFrame(i));
    const frames = [ownerFrame('owner turn'), assistantReply('sealed reply'), ...tail];
    render(
      createElement(AgentTranscript, {
        name: 'jevons',
        ownerChat: true,
        frames,
        meta: { start: 1000, older: 1000, total: 1200, following: true },
        ready: true,
        onFollowChange,
      }),
    );
    await act(async () => { await Promise.resolve(); });
    expect(onFollowChange).not.toHaveBeenCalledWith(false);
  });

  it('stops auto-paging once owner and reply are in the window', async () => {
    const onPageOlder = vi.fn();
    const tail = Array.from({ length: 40 }, (_, i) => toolFrame(i));
    const frames = [ownerFrame('last owner turn'), assistantReply('sealed reply'), ...tail];
    render(
      createElement(AgentTranscript, {
        name: 'jevons',
        frames,
        meta: { start: 100, older: 100, total: 200, following: true },
        ready: true,
        onPageOlder,
      }),
    );
    await act(async () => { await Promise.resolve(); });
    expect(onPageOlder).not.toHaveBeenCalled();
  });
});
