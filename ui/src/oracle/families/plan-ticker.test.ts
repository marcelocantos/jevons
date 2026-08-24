// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterEach, expect } from 'vitest';
import {
  CLASS_EXHAUSTED,
  CLASS_HOT,
  PACE_AHEAD,
  PACE_HOT,
  PACE_OK,
  applyThresholds,
  classifyPace,
  formatWindow,
  resetThresholds,
} from '../../plan/pace';
import { tickerGroups } from '../../plan/tickerGroups';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

const uiSrc = join(dirname(fileURLToPath(import.meta.url)), '../..');

afterEach(() => {
  resetThresholds();
});

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

  itOracle('T117', 'cost ticker is honest or absent — no invented zero rates', () => {
    expect(classifyPace(null, null, null)).toBe('');
    expect(classifyPace(80, 20, null)).toBe('');
    const absent = formatWindow({ name: 'weekly' }, Date.now());
    expect(absent.remainingPercent).toBeNull();
    expect(absent.remainingTimePercent).toBeNull();
    expect(absent.pace).toBe('');
    expect(absent.className.split(' ')).not.toContain(CLASS_EXHAUSTED);

    const unpublished = tickerGroups({
      backends: [
        {
          provider: 'grok',
          status: 'unavailable',
          reason: 'SuperGrok publishes no plan-remaining API',
        },
      ],
    });
    expect(unpublished[0]?.available).toBe(false);
    expect(unpublished[0]?.windows).toEqual([]);

    expect(classifyPace(100, 0, 40)).toBe(PACE_HOT);
    const publishedZero = formatWindow(
      { name: 'weekly', remaining_percent: 0, used_percent: 100 },
      Date.now(),
    );
    expect(publishedZero.remainingPercent).toBe(0);

    const app = readFileSync(join(uiSrc, 'App.tsx'), 'utf8');
    expect(app).toMatch(/<div id="cost-ticker"[^>]*\/>/);
    expect(app).not.toMatch(/\$0\.00/);
    const css = readFileSync(join(uiSrc, 'cockpit.css'), 'utf8');
    expect(css).toMatch(/#cost-ticker\s*\{[^}]*display:\s*none/);
  });

  itOracle('T390.1.3', 'exhausted Claude still paints the boxed session+weekly pair', () => {
    const groups = tickerGroups({
      backends: [
        {
          provider: 'claude',
          status: 'unavailable',
          reason: 'Claude usage HTTP 429: rate_limit_error',
          windows: [],
        },
        {
          provider: 'grok',
          status: 'unavailable',
          reason: 'SuperGrok publishes no plan-remaining API',
        },
      ],
    });
    const cl = groups.find((g) => g.provider === 'claude');
    expect(cl?.available).toBe(true);
    expect(cl?.windows.map((w) => w.name)).toEqual(['session', 'weekly']);
    expect(cl?.windows.map((w) => w.remaining_percent)).toEqual([0, 0]);
    for (const w of cl?.windows || []) {
      const painted = formatWindow(w, Date.now());
      expect(painted.remainingPercent).toBe(0);
      expect(painted.className.split(' ')).toContain(CLASS_EXHAUSTED);
      expect(painted.className.split(' ')).toContain(CLASS_HOT);
    }
    const gk = groups.find((g) => g.provider === 'grok');
    expect(gk?.available).toBe(false);
    expect(gk?.windows).toEqual([]);

    const paint = readFileSync(join(uiSrc, 'components/PlanUsageBar.tsx'), 'utf8');
    expect(paint).toMatch(/className=["']plan-box["']/);
    expect(paint).toMatch(/g\.windows\.length/);
    expect(paint).toMatch(/formatWindow/);
    const css = readFileSync(join(uiSrc, 'cockpit.css'), 'utf8');
    expect(css).toMatch(/--plan-exhausted:/);
    expect(css).toMatch(/plan-win\.plan-exhausted \.plan-bar/);
  });

  itOracle('T390.1.6', 'ticker vertices come from the served thresholds document', () => {
    expect(classifyPace(65, 35, 50)).toBe(PACE_AHEAD);
    applyThresholds({ hot_ratio: 1.2 });
    expect(classifyPace(65, 35, 50)).toBe(PACE_HOT);
    resetThresholds();
    applyThresholds({ damp_lambda_percent: 0 });
    expect(classifyPace(9, 91, 94.4, 'weekly')).toBe(PACE_HOT);

    const paint = readFileSync(join(uiSrc, 'components/PlanUsageBar.tsx'), 'utf8');
    expect(paint).toMatch(/\/api\/plan-usage\/thresholds/);
    expect(paint).toMatch(/applyThresholds/);
  });
});
