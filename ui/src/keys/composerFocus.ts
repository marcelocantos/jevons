// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Dedicated `/` focus-composer hotkey (T127) and unconditional
 * return-to-composer (T153). Extracted next to keys/ so the hook does
 * not own the policy inline.
 */

export const FOCUS_COMPOSER_HOTKEY = '/';

export function isFocusComposerHotkey(
  key: string,
  mods?: { metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean },
): boolean {
  if (key !== FOCUS_COMPOSER_HOTKEY && key !== 'Slash') return false;
  const m = mods || {};
  if (m.metaKey || m.ctrlKey || m.altKey) return false;
  return true;
}

export function isEditableTarget(el: {
  tagName?: string;
  type?: string;
  isContentEditable?: boolean;
  contentEditable?: boolean | string;
} | null): boolean {
  if (!el) return false;
  const tag = String(el.tagName || '').toUpperCase();
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (tag === 'INPUT') {
    const t = String(el.type || 'text').toLowerCase();
    if (
      t === 'button' ||
      t === 'submit' ||
      t === 'reset' ||
      t === 'checkbox' ||
      t === 'radio' ||
      t === 'file' ||
      t === 'image' ||
      t === 'range' ||
      t === 'color' ||
      t === 'hidden'
    ) {
      return false;
    }
    return true;
  }
  if (el.isContentEditable === true) return true;
  const ce = el.contentEditable;
  return ce === true || ce === 'true';
}

export function shouldFocusComposer(
  key: string,
  mods: { metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean } | undefined,
  activeElement: { tagName?: string; type?: string; isContentEditable?: boolean; contentEditable?: boolean | string } | null,
): boolean {
  if (!isFocusComposerHotkey(key, mods)) return false;
  if (isEditableTarget(activeElement)) return false;
  return true;
}

export function tryFocusComposer(
  eventLike: { key?: string; metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean },
  composerEl: { focus: () => void } | null,
  activeElement: object | null,
): { didFocus: boolean; reason: string } {
  const ev = eventLike || {};
  if (!composerEl || typeof composerEl.focus !== 'function') {
    return { didFocus: false, reason: 'no-composer' };
  }
  if (activeElement === composerEl) {
    return { didFocus: false, reason: 'already-focused' };
  }
  if (!shouldFocusComposer(ev.key || '', ev, activeElement as { tagName?: string } | null)) {
    return { didFocus: false, reason: 'not-applicable' };
  }
  composerEl.focus();
  return { didFocus: true, reason: 'focused' };
}

export function focusComposer(composerEl: { focus: () => void } | null): { didFocus: boolean; reason: string } {
  if (!composerEl || typeof composerEl.focus !== 'function') {
    return { didFocus: false, reason: 'no-composer' };
  }
  composerEl.focus();
  return { didFocus: true, reason: 'focused' };
}
