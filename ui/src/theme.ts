// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

export type ThemePref = 'light' | 'dark' | 'system';

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
