// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Two-stop Tab cycle: main composer ↔ sidebar composer (T366). */

export type TabPlan = {
  target: 'main' | 'sidebar' | null;
  preventDefault: boolean;
  reason: string;
};

export function isComposerTabChord(key: string, mods: { metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean }): boolean {
  if (key !== 'Tab') return false;
  return !(mods.metaKey || mods.ctrlKey || mods.altKey);
}

export function planComposerTabCycle(
  ev: { key: string; metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean },
  ctx: {
    active: 'main' | 'sidebar' | 'other';
    sidebarVisible: boolean;
  },
): TabPlan {
  const none = (reason: string): TabPlan => ({ target: null, preventDefault: false, reason });
  if (!isComposerTabChord(ev.key, ev)) return none('not-tab');
  if (ctx.active === 'main') {
    if (!ctx.sidebarVisible) return none('sidebar-unavailable');
    return { target: 'sidebar', preventDefault: true, reason: 'to-sidebar' };
  }
  if (ctx.active === 'sidebar') {
    return { target: 'main', preventDefault: true, reason: 'to-main' };
  }
  return none('not-in-cycle');
}
