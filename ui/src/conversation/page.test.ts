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
  });
});
