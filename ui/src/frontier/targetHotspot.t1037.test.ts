// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { splitSeatNameTarget } from './targetHotspot';

describe('splitSeatNameTarget (🎯T1037)', () => {
  it('splits a dotted id under an arbitrary repo prefix', () => {
    expect(splitSeatNameTarget('ge-t65.8-cancel-implement')).toEqual({
      prefix: 'ge-',
      matched: 't65.8',
      suffix: '-cancel-implement',
      id: 'T65.8',
    });
  });

  it('splits a whole-name id', () => {
    expect(splitSeatNameTarget('T1014')).toEqual({
      prefix: '',
      matched: 'T1014',
      suffix: '',
      id: 'T1014',
    });
  });

  it('splits jv-tN-slug and keeps the rest of the name', () => {
    expect(splitSeatNameTarget('jv-t1037-seat-name-hotlink')).toEqual({
      prefix: 'jv-',
      matched: 't1037',
      suffix: '-seat-name-hotlink',
      id: 'T1037',
    });
  });

  it('returns null when the name has no target id', () => {
    expect(splitSeatNameTarget('jevons-po')).toBeNull();
    expect(splitSeatNameTarget('jv-compact-a7a1dc5e')).toBeNull();
    expect(splitSeatNameTarget('claudia-po')).toBeNull();
    expect(splitSeatNameTarget('')).toBeNull();
  });
});
