// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { PNG_ROW_DY, pixelFixtureRowTop } from './oldCockpitFixture';

describe('pixelFixtureRowTop', () => {
  it('is identity when the PNG fixture is off', () => {
    expect(pixelFixtureRowTop(151, 2, 'comfortable')).toBe(151);
  });

  it('holds index 0 at the virtualizer start so first-line ink stays put', () => {
    (globalThis as { __JEVONS_PIXEL_FIXTURE?: boolean }).__JEVONS_PIXEL_FIXTURE = true;
    expect(PNG_ROW_DY[0]).toBe(0);
    expect(pixelFixtureRowTop(0, 0, 'comfortable')).toBe(0);
    expect(pixelFixtureRowTop(151, 2, 'comfortable')).toBe(148);
    expect(pixelFixtureRowTop(151, 2, 'compact')).toBe(151);
    delete (globalThis as { __JEVONS_PIXEL_FIXTURE?: boolean }).__JEVONS_PIXEL_FIXTURE;
  });
});
