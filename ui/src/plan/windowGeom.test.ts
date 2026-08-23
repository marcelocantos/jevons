// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { remainingTimePercent, SESSION_LIMIT_SECONDS } from './windowGeom';

describe('plan window triangle', () => {
  it('places the triangle from resets_at and the session default length', () => {
    const now = Date.parse('2026-01-01T00:00:00Z');
    const resets = new Date(now + SESSION_LIMIT_SECONDS * 1000 * 0.4).toISOString();
    expect(remainingTimePercent({ name: 'session', resets_at: resets }, now)).toBe(40);
  });

  it('does not invent a triangle when resets_at is missing', () => {
    expect(remainingTimePercent({ name: 'session', remaining_percent: 62 }, 0)).toBeNull();
  });
});
