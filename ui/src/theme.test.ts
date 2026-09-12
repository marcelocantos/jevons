// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { nextThemePref, THEME_GLYPH, themeCycle, type ThemePref } from './theme';

describe('🎯T642 theme cycle', () => {
  it('OS light: system, dark, light', () => {
    expect(themeCycle('light')).toEqual(['system', 'dark', 'light']);
  });

  it('OS dark: system, light, dark', () => {
    expect(themeCycle('dark')).toEqual(['system', 'light', 'dark']);
  });

  it('first click from system is the opposite of the OS', () => {
    expect(nextThemePref('system', 'light')).toBe('dark');
    expect(nextThemePref('system', 'dark')).toBe('light');
  });

  it('walks the full ring and returns to system', () => {
    const walk = (start: ThemePref, system: 'light' | 'dark') => {
      const seen: ThemePref[] = [];
      let cur = start;
      for (let i = 0; i < 3; i++) {
        cur = nextThemePref(cur, system);
        seen.push(cur);
      }
      return seen;
    };
    expect(walk('system', 'light')).toEqual(['dark', 'light', 'system']);
    expect(walk('system', 'dark')).toEqual(['light', 'dark', 'system']);
  });

  it('keeps sun and moon; system is the two-tone circle', () => {
    expect(THEME_GLYPH.light).toBe('☀️');
    expect(THEME_GLYPH.system).toBe('◐');
    expect(THEME_GLYPH.dark).toBe('🌙');
    expect(new Set(Object.values(THEME_GLYPH)).size).toBe(3);
  });
});
