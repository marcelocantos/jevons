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
});
