// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import type { ThemePref } from '../theme';

/** Decorative icons: the enclosing buttons own the accessible names. */
export function SettingsIcon() {
  return <svg className="header-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
    <path d="M10 2.7h4l.5 2.1a7.7 7.7 0 0 1 1.8 1l2-.8 2 3.4-1.6 1.5a7.6 7.6 0 0 1 0 2.2l1.6 1.5-2 3.4-2-.8a7.7 7.7 0 0 1-1.8 1L14 19.3h-4l-.5-2.1a7.7 7.7 0 0 1-1.8-1l-2 .8-2-3.4 1.6-1.5a7.6 7.6 0 0 1 0-2.2L3.7 8.4l2-3.4 2 .8a7.7 7.7 0 0 1 1.8-1L10 2.7Z" transform="translate(0 1)" />
    <circle cx="12" cy="12" r="2.7" />
  </svg>;
}

export function ThemeIcon({ theme }: { theme: ThemePref }) {
  const lines = { className: 'header-icon', viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true as const, focusable: false as const };
  if (theme === 'light') return <svg {...lines}><circle cx="12" cy="12" r="4" /><path d="M12 2v2m0 16v2M2 12h2m16 0h2M4.93 4.93l1.42 1.42m11.3 11.3 1.42 1.42m0-14.14-1.42 1.42M6.35 17.65l-1.42 1.42" /></svg>;
  if (theme === 'dark') return <svg {...lines}><path d="M20.2 15.5A8.6 8.6 0 0 1 8.5 3.8a8.6 8.6 0 1 0 11.7 11.7Z" /></svg>;
  return <svg {...lines}><circle cx="12" cy="12" r="8.5" /><path d="M12 3.5a8.5 8.5 0 0 1 0 17Z" fill="currentColor" stroke="none" /></svg>;
}
