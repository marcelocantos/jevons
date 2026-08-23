// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { clampRhsWidth, RHS_MIN } from './rhsWidth';

describe('clampRhsWidth', () => {
  it('stays between min and 55% of viewport', () => {
    expect(clampRhsWidth(100, 1000)).toBe(RHS_MIN);
    expect(clampRhsWidth(900, 1000)).toBe(550);
    expect(clampRhsWidth(360, 1000)).toBe(360);
  });
});
