// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Two-stop Tab cycle: main composer ↔ sidebar composer (T366 / T547). */

export type TabPlan = {
  target: 'main' | 'sidebar' | null;
  preventDefault: boolean;
  reason: string;
};

/** Chrome that Tab must never land on (T153 / T547). */
export const COMPOSER_TAB_CHROME = [
  'theme-toggle',
  'send',
  'voice-btn',
  'jump-bottom',
  'rhs-width-handle',
  'rhs-split-handle',
] as const;

export function isComposerTabChord(
  key: string,
  mods: { metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean; code?: string },
): boolean {
  const code = mods.code != null ? String(mods.code) : '';
  if (key !== 'Tab' && code !== 'Tab') return false;
  return !(mods.metaKey || mods.ctrlKey || mods.altKey);
}

/**
 * True when the sidebar Transcript composer is a real Tab-cycle stop.
 * A `.rhs-tab-pane` without `.active` is `display:none` — focusing it
 * steals the caret from the visible main box (T549 / T571).
 */
export function isSidebarComposerFocusable(
  root: Pick<ParentNode, 'querySelector'> | null | undefined,
): boolean {
  if (!root || typeof root.querySelector !== 'function') return false;
  const side = root.querySelector('textarea[data-composer="sidebar"]');
  if (!(side instanceof HTMLTextAreaElement)) return false;
  if (side.disabled || side.hidden) return false;
  const pane = side.closest('#agent-inspect, .rhs-tab-pane');
  if (!pane || (pane instanceof HTMLElement && pane.hidden)) return false;
  if (!pane.classList.contains('active')) return false;
  const wrap = side.closest('#agent-inspect-composer');
  if (wrap) {
    if (wrap instanceof HTMLElement && wrap.hidden) return false;
    if (!wrap.classList.contains('visible')) return false;
  }
  return true;
}

export function planComposerTabCycle(
  ev: { key: string; metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean; code?: string },
  ctx: {
    active: 'main' | 'sidebar' | 'other';
    sidebarVisible: boolean;
  },
): TabPlan {
  const none = (reason: string): TabPlan => ({ target: null, preventDefault: false, reason });
  if (!isComposerTabChord(ev.key, ev)) return none('not-tab');
  if (ctx.active === 'main') {
    // T547: already in a composer — claim Tab even when the partner is
    // hidden, so document order cannot walk theme/send/voice/jump/resize.
    if (!ctx.sidebarVisible) {
      return { target: 'main', preventDefault: true, reason: 'stay-main' };
    }
    return { target: 'sidebar', preventDefault: true, reason: 'to-sidebar' };
  }
  if (ctx.active === 'sidebar') {
    return { target: 'main', preventDefault: true, reason: 'to-main' };
  }
  return none('not-in-cycle');
}
