// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { shouldRequestPage } from './page';

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
