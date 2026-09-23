// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it, afterEach } from 'vitest';
import { reset, setNow } from './clock';
import { relTime } from './relTime';

afterEach(() => reset());

describe('relTime', () => {
  it('uses harnessable now, not wall clock', () => {
    setNow(1_700_000_000_000);
    expect(relTime(1_700_000_000_000 - 18 * 3600 * 1000)).toBe('18h');
    expect(relTime(1_700_000_000_000 - 4 * 3600 * 1000)).toBe('4h');
    expect(relTime(1_700_000_000_000 - 30 * 1000)).toBe('now');
  });

  it('keeps a weekday stamp together with a narrow no-break space', () => {
    setNow(1_700_000_000_000);
    expect(relTime(1_700_000_000_000 - 8 * 24 * 3600 * 1000)).toMatch(/^\S{2}\u202f\d{2}:\d{2}$/);
  });
});
