// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { AUTO_FILL_MAX_PAGES, shouldAutoFillPane, shouldOfferShowEarlier, shouldRequestPage } from './page';

describe('shouldRequestPage', () => {
  it('requests once at the top, then waits until inFlight clears', () => {
    expect(shouldRequestPage({ scrollTop: 10, older: 20, inFlight: false })).toBe(true);
    expect(shouldRequestPage({ scrollTop: 10, older: 20, inFlight: true })).toBe(false);
    expect(shouldRequestPage({ scrollTop: 80, older: 20, inFlight: false })).toBe(false);
    expect(shouldRequestPage({ scrollTop: 0, older: 0, inFlight: false })).toBe(false);
    expect(shouldRequestPage({ scrollTop: 10, older: 0, truncated: true, inFlight: false })).toBe(true);
  });

  it('does not page the live tail when following or the pane is short', () => {
    expect(shouldRequestPage({ scrollTop: 0, older: 20, inFlight: false, following: true })).toBe(false);
    expect(
      shouldRequestPage({
        scrollTop: 0,
        older: 20,
        inFlight: false,
        scrollHeight: 400,
        clientHeight: 689,
      }),
    ).toBe(false);
    expect(
      shouldRequestPage({
        scrollTop: 10,
        older: 20,
        inFlight: false,
        following: false,
        scrollHeight: 4000,
        clientHeight: 689,
      }),
    ).toBe(true);
  });
});

describe('shouldOfferShowEarlier', () => {
  it('offers a control when the pane is taller than the tail and older history exists (🎯T1159)', () => {
    expect(
      shouldOfferShowEarlier({ older: 40, scrollHeight: 120, clientHeight: 689, inFlight: false }),
    ).toBe(true);
    expect(
      shouldOfferShowEarlier({ older: 40, scrollHeight: 1200, clientHeight: 689, inFlight: false }),
    ).toBe(false);
    expect(
      shouldOfferShowEarlier({ older: 0, truncated: true, scrollHeight: 50, clientHeight: 689 }),
    ).toBe(true);
    expect(
      shouldOfferShowEarlier({ older: 10, scrollHeight: 50, clientHeight: 689, inFlight: true }),
    ).toBe(false);
  });
});

describe('shouldAutoFillPane', () => {
  const base = { older: 100, inFlight: false, scrollHeight: 300, clientHeight: 800, pages: 0 };
  it('fills a pane whose content is shorter than a screen and a half', () => {
    expect(shouldAutoFillPane(base)).toBe(true);
    expect(shouldAutoFillPane({ ...base, scrollHeight: 1100 })).toBe(true);
    expect(shouldAutoFillPane({ ...base, scrollHeight: 1200 })).toBe(false);
  });
  it('stops when history runs out, a page is in flight, or the bound is hit', () => {
    expect(shouldAutoFillPane({ ...base, older: 0 })).toBe(false);
    expect(shouldAutoFillPane({ ...base, inFlight: true })).toBe(false);
    expect(shouldAutoFillPane({ ...base, pages: AUTO_FILL_MAX_PAGES })).toBe(false);
  });
  it('does nothing before the pane has a measured height', () => {
    expect(shouldAutoFillPane({ ...base, clientHeight: 0 })).toBe(false);
  });
});
