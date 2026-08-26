// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { normalizeDensity } from './density';

describe('density', () => {
  it('normalizes compact vs comfortable', () => {
    expect(normalizeDensity('compact')).toBe('compact');
    expect(normalizeDensity('COMPACT')).toBe('compact');
    expect(normalizeDensity('comfortable')).toBe('comfortable');
    expect(normalizeDensity(undefined)).toBe('comfortable');
  });

  it('App mounts one AgentInteraction twice with density params, not a second viewer', () => {
    const root = dirname(fileURLToPath(import.meta.url));
    const app = readFileSync(join(root, 'App.tsx'), 'utf8');
    const n = app.split('<AgentInteraction').length - 1;
    expect(n).toBe(2);
    expect(app).toContain('density="comfortable"');
    expect(app).toContain('density="compact"');
    expect(app).not.toMatch(/SidebarTranscript|InspectTranscript|AgentInspect/);
  });

  it('bubble chrome is one family parameterized by data-density', () => {
    const root = dirname(fileURLToPath(import.meta.url));
    const css = readFileSync(join(root, 'cockpit.css'), 'utf8');
    const widget = readFileSync(join(root, 'components/AgentInteraction.tsx'), 'utf8');
    const userRules = css.match(/(?:^|\n)\.msg\.user\s*\{/g) || [];
    expect(userRules.length, 'one .msg.user rule, compact is data-density').toBe(1);
    expect(css).toMatch(/\.density-compact|\[data-density=['"]compact['"]\]/);
    expect(widget).toMatch(/data-density=\{density\}/);
    expect(css).not.toMatch(/sidebar-bubble|inspect-msg|rhs-msg/);
  });
});