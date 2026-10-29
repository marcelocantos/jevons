import { describe, expect, it } from 'vitest';
import { BURN_HEIGHT, paceLine } from './burnGeom';
import type { PlanWindow } from './tickerGroups';

describe('T1284 mined burn pace', () => {
  // Monday 00:00 UTC through the next Monday.
  const window = (): PlanWindow => ({ name: 'weekly', resets_at: '2026-10-12T00:00:00Z' });
  it('spends most of its allowance on Saturday and Sunday', () => {
    const w = window();
    w.burn_profile = { windows: 3, fallback: false, weekday_shares: [0.3, 0.08, 0.08, 0.08, 0.08, 0.08, 0.3] };
    const path = paceLine(w)!;
    expect(path).toContain('L71.4,19.2'); // Friday: 40% used, below even pace.
    expect(path).toContain('L85.7,9.6'); // Saturday adds 30%.
    expect(path).toMatch(/L100,0$/);
  });
  it('thin or absent history keeps an honest diagonal', () => {
    for (const profile of [undefined, { windows: 1, fallback: true, weekday_shares: [0, 0, 0, 0, 0, 0, 0] }]) {
      const w = window(); w.burn_profile = profile;
      expect(paceLine(w)).toBe(`M0,${BURN_HEIGHT} L100,0`);
    }
  });
});
