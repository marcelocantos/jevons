// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { holdLastPlanSnapshot } from './holdSnapshot';

const reading = {
  backends: [{ windows: [{ name: 'weekly', remaining_percent: 72 }] }],
};

describe('holdLastPlanSnapshot', () => {
  it('keeps the last reading when the next poll is pending or empty', () => {
    expect(holdLastPlanSnapshot(reading, { pending: true })).toEqual(reading);
    expect(holdLastPlanSnapshot(reading, undefined)).toEqual(reading);
    expect(holdLastPlanSnapshot(reading, { backends: [] })).toEqual(reading);
  });

  it('takes a newer reading when it has remaining percents', () => {
    const newer = {
      backends: [{ windows: [{ name: 'weekly', remaining_percent: 60 }] }],
    };
    expect(holdLastPlanSnapshot(reading, newer)).toEqual(newer);
  });

  it('shows pending when nothing has landed yet', () => {
    expect(holdLastPlanSnapshot(undefined, { pending: true })).toEqual({ pending: true });
  });
});
