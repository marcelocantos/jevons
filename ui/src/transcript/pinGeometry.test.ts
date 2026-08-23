// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { pinnedFirstY, pinnedLastY, type PinGeometry } from './pinGeometry';

/** 1440×900 cockpit: status 34px, #messages pad 24, clientHeight 789. */
const VIEW: Omit<PinGeometry, 'contentHeight' | 'paddingEnd' | 'lastHeight'> = {
  messagesTop: 34,
  padTop: 24,
  padBottom: 24,
  clientHeight: 789,
};

describe('pinned pin-to-end geometry', () => {
  it('paddingEnd moves first and last together (the extra-14 trap)', () => {
    const base = { ...VIEW, contentHeight: 755, paddingEnd: 0, lastHeight: 13 };
    const extra = { ...base, paddingEnd: 14 };
    expect(pinnedLastY(extra) - pinnedLastY(base)).toBe(-14);
    expect(pinnedFirstY(extra) - pinnedFirstY(base)).toBe(-14);
  });

  it('taller content between first and last moves only the first row', () => {
    const short = { ...VIEW, contentHeight: 755, paddingEnd: 0, lastHeight: 13 };
    const tall = { ...short, contentHeight: 765 };
    expect(pinnedLastY(tall)).toBe(pinnedLastY(short));
    expect(pinnedFirstY(short) - pinnedFirstY(tall)).toBe(10);
  });

  it('golden targets: first bubble 34 / last marker 786 need paddingEnd 0 and C=765', () => {
    const g = { ...VIEW, contentHeight: 765, paddingEnd: 0, lastHeight: 13 };
    expect(pinnedFirstY(g)).toBe(34);
    expect(pinnedLastY(g)).toBe(786);
  });
});
