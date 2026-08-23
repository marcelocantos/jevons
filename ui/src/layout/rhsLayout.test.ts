// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  STORAGE_KEY,
  DEFAULT_SIDEBAR_WIDTH,
  clampSidebarWidth,
  load,
  save,
  sidebarWidthFromPointer,
  stylesForState,
} from './rhsLayout';

describe('rhsLayout', () => {
  it('uses the old app storage key and 420px default', () => {
    expect(STORAGE_KEY).toBe('jevons-rhs-layout-v1');
    expect(DEFAULT_SIDEBAR_WIDTH).toBe(420);
  });

  it('sidebarWidthFromPointer maps border X to width like the old module', () => {
    const main = 1000;
    expect(sidebarWidthFromPointer(580, main)).toBe(420);
  });

  it('round-trips mouse-adjusted width through the same JSON as the old app', () => {
    const storage = new Map<string, string>();
    const mem = {
      getItem: (k: string) => storage.get(k) ?? null,
      setItem: (k: string, v: string) => {
        storage.set(k, v);
      },
    };
    save(mem, { sidebarWidth: 458, fleetFraction: 0.33 });
    const loaded = load(mem);
    expect(loaded.present).toBe(true);
    expect(loaded.state.sidebarWidth).toBe(458);
    expect(loaded.state.fleetFraction).toBe(0.33);
    expect(storage.get(STORAGE_KEY)).toBe(
      JSON.stringify({ sidebarWidth: 458, fleetFraction: 0.33 }),
    );
  });

  it('clampSidebarWidth keeps chat usable', () => {
    expect(clampSidebarWidth(100, 1000)).toBe(220);
    expect(clampSidebarWidth(900, 1000)).toBe(720);
  });

  it('stylesForState matches old flex-basis percent', () => {
    const s = stylesForState({ sidebarWidth: 380, fleetFraction: 0.4 });
    expect(s.sidebarWidthPx).toBe(380);
    expect(s.fleetFlexBasis).toBe('40%');
  });
});
