// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it, afterEach } from 'vitest';
import { PACE_AHEAD, PACE_HOT, classifyPace, resetThresholds } from './pace';

afterEach(() => resetThresholds());

// 🎯T591. Damping approaches 1 from above and never crosses it, so at
// ahead_ratio 1.0 any overspend at all — rounding included — was amber.
describe('a burning-fast verdict needs real overspend', () => {
  it('leaves a barely-started week alone', () => {
    // The live 2026-08-31 reading: claude had spent about seven minutes
    // of a seven-day window. used is published as whole percentage
    // points, so 1% here is a rounding step against 0.38% elapsed.
    // remainingTime 99.62 → elapsed 0.38.
    expect(classifyPace(1, 99, 99.62, 'weekly')).not.toBe(PACE_AHEAD);
    expect(classifyPace(1, 99, 99.62, 'weekly')).not.toBe(PACE_HOT);
  });

  it('still flags the early blowout the damping comment cites', () => {
    // 9% used at 5.6% elapsed overspends by 3.4pp — past the margin, and
    // damps to 1.32. The margin must not swallow this one.
    expect(classifyPace(9, 91, 94.4, 'weekly')).toBe(PACE_AHEAD);
  });

  it('still flags a mid-window burn as hot', () => {
    // 80/50 overspends by 30pp and damps to 1.55.
    expect(classifyPace(80, 20, 50, 'weekly')).toBe(PACE_HOT);
  });

  it('turns amber once overspend clears the margin, not before', () => {
    // Same elapsed, walking used across the 2pp margin.
    expect(classifyPace(2, 98, 99, 'weekly')).not.toBe(PACE_AHEAD); // 1.0pp
    expect(classifyPace(3, 97, 99, 'weekly')).not.toBe(PACE_AHEAD); // 2.0pp: the
    // gate is strictly greater, so sitting exactly on the margin is not over it
    expect(classifyPace(4, 96, 99, 'weekly')).toBe(PACE_AHEAD); // 3.0pp
  });

  it('an exhausted window is still hot regardless of margin', () => {
    expect(classifyPace(100, 0, 99, 'weekly')).toBe(PACE_HOT);
  });
});
