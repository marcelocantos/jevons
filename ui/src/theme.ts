// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

export type ThemePref = 'light' | 'dark' | 'system';
export type ThemeAppearance = 'light' | 'dark';

/** Color emoji — the old ☼/◐/☾ dingbats read as blank circles at 11px. */
export const THEME_GLYPH: Record<ThemePref, string> = {
  light: '☀️',
  system: '☯️',
  dark: '🌙',
};

export function themeLabel(pref: ThemePref): string {
  if (pref === 'light') return 'Light';
  if (pref === 'dark') return 'Dark';
  return 'System';
}

/** Live OS appearance. Dark unless the light media query matches. */
export function systemAppearance(media?: Pick<Window, 'matchMedia'>): ThemeAppearance {
  const src = media ?? (typeof window !== 'undefined' ? window : undefined);
  if (!src || typeof src.matchMedia !== 'function') return 'dark';
  return src.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

/**
 * Click order depends on the OS so the first click from system leaves it.
 * Light OS: system → dark → light. Dark OS: system → light → dark.
 */
export function themeCycle(system: ThemeAppearance): ThemePref[] {
  return system === 'light' ? ['system', 'dark', 'light'] : ['system', 'light', 'dark'];
}

export function nextThemePref(current: ThemePref, system: ThemeAppearance): ThemePref {
  const cycle = themeCycle(system);
  const i = cycle.indexOf(current);
  return cycle[(i < 0 ? 0 : i + 1) % cycle.length];
}

export function readThemePref(): ThemePref {
  const m = document.cookie.match(/(?:^|; )theme=([^;]*)/);
  const v = m ? decodeURIComponent(m[1]) : '';
  if (v === 'light' || v === 'dark' || v === 'system') return v;
  return 'system';
}

export function applyTheme(pref: ThemePref): void {
  const html = document.documentElement;
  if (pref === 'system') {
    const mq = window.matchMedia('(prefers-color-scheme: light)');
    html.setAttribute('data-theme', mq.matches ? 'light' : 'dark');
  } else {
    html.setAttribute('data-theme', pref);
  }
}

export function persistTheme(pref: ThemePref): void {
  document.cookie = 'theme=' + pref + ';path=/;max-age=31536000';
  applyTheme(pref);
}
