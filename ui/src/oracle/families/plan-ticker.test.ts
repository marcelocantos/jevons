// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { classifyPace, PACE_AHEAD, PACE_HOT, PACE_OK, PACE_UNDER } from '../../plan/pace';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('plan-ticker'), () => {
  itOracle('T390.1.6.2', 'no elapsed cutoff — Codex 26% used at ~5% elapsed is hot', () => {
    expect(classifyPace(26, 74, 95.1, 'weekly')).toBe(PACE_HOT);
  });

  itOracle('T390.1.6.1', 'week-start 9%/5.6% damps to ahead, not hot', () => {
    const weekStart = classifyPace(9, 91, 94.4, 'weekly');
    expect(weekStart === PACE_OK || weekStart === PACE_AHEAD).toBe(true);
  });

  itOracle('T390', 'on-pace mid-window is green', () => {
    expect(classifyPace(50, 50, 50, 'weekly')).toBe(PACE_OK);
  });

  itOracle.todo('T117', 'cost ticker is honest or absent — no invented zero rates');
  itOracle.todo('T390.1.3', 'exhausted Claude still paints the boxed session+weekly pair');
  itOracle.todo('T390.1.6', 'ticker vertices come from the served thresholds document');
});
