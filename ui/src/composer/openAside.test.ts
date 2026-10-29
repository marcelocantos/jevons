// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it, vi } from 'vitest';
import {
  asideKindOf,
  createAsideRequest,
  mintAsideId,
  planExplicitAside,
  postCreateAside,
} from './openAside';

describe('planExplicitAside (T95 / T247 / T368)', () => {
  it('target: mints an att- id, kind=target, register-only', () => {
    const plan = planExplicitAside('target: T540.1 census aside from journey', () => 'att-fixed');
    expect(plan).toMatchObject({
      command: 'target',
      kind: 'target',
      id: 'att-fixed',
      title: 'T540.1 census aside from journey',
      body: 'T540.1 census aside from journey',
    });
    expect(plan?.text).toBeUndefined();
    expect(createAsideRequest(plan!)).toEqual({
      id: 'att-fixed',
      title: 'T540.1 census aside from journey',
      kind: 'target',
    });
  });

  it('aside: delivers the opening text (T263)', () => {
    const plan = planExplicitAside('aside: how does bullseye compare to beads?', () => 'att-side');
    expect(plan?.kind).toBe('side');
    expect(plan?.text).toBe('how does bullseye compare to beads?');
    expect(createAsideRequest(plan!).text).toBe('how does bullseye compare to beads?');
  });

  it('leading image markers do not hide target: (T368)', () => {
    const plan = planExplicitAside('[image: abcdef] target: file this', () => 'att-img');
    expect(plan?.command).toBe('target');
    expect(plan?.body).toMatch(/file this/);
    expect(plan?.images).toMatch(/\[image: abcdef\]/);
  });

  it('ordinary chat is not an aside', () => {
    expect(planExplicitAside('hello there')).toBeNull();
    expect(planExplicitAside('park: restic')).toBeNull();
    expect(planExplicitAside('main: back')).toBeNull();
  });

  it('capture and idea are capture-kind, register-only', () => {
    expect(asideKindOf('capture')).toBe('capture');
    expect(asideKindOf('idea')).toBe('capture');
    expect(planExplicitAside('idea: a spark', () => 'att-idea')?.text).toBeUndefined();
  });

  it('mintAsideId is att-<time36>-<rand>', () => {
    expect(mintAsideId(() => 36, () => 0.5)).toMatch(/^att-[0-9a-z]+-[0-9a-z]+$/);
  });
});

describe('postCreateAside', () => {
  it('POSTs /api/asides and returns the registered name', async () => {
    const fetchImpl = vi.fn(async () => ({
      ok: true,
      json: async () => ({ name: 'att-smoke', purpose: 'aside' }),
      text: async () => '',
    }));
    const plan = planExplicitAside('target: smoke', () => 'att-smoke')!;
    const out = await postCreateAside(plan, fetchImpl as unknown as typeof fetch);
    expect(out.name).toBe('att-smoke');
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    const [url, init] = fetchImpl.mock.calls[0];
    expect(url).toBe('/api/asides');
    expect((init as { method?: string }).method).toBe('POST');
    expect(JSON.parse(String((init as { body?: string }).body))).toEqual({
      id: 'att-smoke',
      title: 'smoke',
      kind: 'target',
    });
  });
});
