// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// 🎯T1019: the bar's BORDER (box-shadow inset) must be neutral gray for
// every pace band except exhausted (red) and open/<1%-used (green).
// Commit 7bd6c014 (T390.1.1) gave under/locked a coloured border that
// predated, and was never reconciled with, that red/green-only rule.
// ahead and hot never had a coloured border; this pins under/locked to
// the same neutral default they already use.

const cockpitCSS = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '..', 'cockpit.css'), 'utf8');

/** The CSS rule whose selector is exactly `#plan-ticker <selector>`, or null if absent. */
function ruleFor(selector: string): string | null {
  const needle = '#plan-ticker ' + selector;
  const at = cockpitCSS.indexOf(needle + ' {');
  if (at < 0) return null;
  return cockpitCSS.slice(at, cockpitCSS.indexOf('}', at));
}

describe('plan bar border colour by pace band (🎯T1019)', () => {
  it('gives ahead and hot no bar-specific border rule — they ride the neutral default', () => {
    expect(ruleFor('.plan-win.plan-ahead .plan-bar')).toBeNull();
    expect(ruleFor('.plan-win.plan-hot .plan-bar')).toBeNull();
  });

  it('gives under and locked no coloured border rule either — same neutral default as ahead/hot', () => {
    expect(ruleFor('.plan-win.plan-under .plan-bar')).toBeNull();
    expect(ruleFor('.plan-win.plan-locked .plan-bar')).toBeNull();
  });

  it('still paints under and locked fills in their own colour — only the border changed', () => {
    const underFill = ruleFor('.plan-win.plan-under .plan-bar-fill');
    const lockedFill = ruleFor('.plan-win.plan-locked .plan-bar-fill');
    expect(underFill).toContain('background: var(--plan-under)');
    expect(lockedFill).toContain('background: var(--plan-locked)');
  });

  it('keeps exhausted red and open green as the only coloured borders', () => {
    const exhausted = ruleFor('.plan-win.plan-exhausted .plan-bar');
    const open = ruleFor('.plan-win.plan-open .plan-bar');
    expect(exhausted).toContain('box-shadow: inset 0 0 0 1px var(--plan-exhausted)');
    expect(open).toContain('box-shadow: inset 0 0 0 1px var(--green)');
  });

  it('the base .plan-bar rule (the neutral default every other band rides) uses --border, not a pace colour', () => {
    const base = ruleFor('.plan-bar');
    expect(base).toContain('box-shadow: inset 0 0 0 1px var(--border)');
  });
});
