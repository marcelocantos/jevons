// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Field Home/End caret policy (T126 / T307).
 * Home/End own field start/end; Meta|Ctrl+ArrowLeft/Right stay with the
 * browser as word/line chords. Extracted next to keys/ — not App.tsx.
 */

export type CaretBounds = { home: number; end: number };
export type CaretSelection = { start: number; end: number };

export type SeedCaretOpts = {
  seedPrefixLen?: number;
  effectiveLength?: number;
  isSeedOnly?: boolean;
};

export function contentCaretBounds(value: string, opts?: SeedCaretOpts): CaretBounds {
  const s = String(value == null ? '' : value);
  const o = opts || {};
  let seedLen = 0;
  if (typeof o.seedPrefixLen === 'number' && o.seedPrefixLen > 0) {
    seedLen = Math.min(Math.floor(o.seedPrefixLen), s.length);
  }
  let effectiveLen: number;
  if (typeof o.effectiveLength === 'number' && o.effectiveLength >= 0) {
    effectiveLen = o.effectiveLength;
  } else {
    effectiveLen = Math.max(0, s.length - seedLen);
  }
  const seedOnly =
    o.isSeedOnly === true ||
    (seedLen > 0 && effectiveLen === 0) ||
    (s.length > 0 && seedLen > 0 && s.length === seedLen);
  if (!s.length || seedOnly || effectiveLen === 0) {
    return { home: s.length, end: s.length };
  }
  return { home: seedLen, end: s.length };
}

export function caretAfterHome(value: string, _selStart?: number, opts?: SeedCaretOpts): number {
  return contentCaretBounds(value, opts).home;
}

export function caretAfterEnd(value: string, _selStart?: number, opts?: SeedCaretOpts): number {
  return contentCaretBounds(value, opts).end;
}

export function selectionAfterHomeEnd(
  key: string,
  value: string,
  selStart: number,
  selEnd: number,
  mods?: { metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean; shiftKey?: boolean },
  opts?: SeedCaretOpts,
): CaretSelection | null {
  const m = mods || {};
  if (m.altKey) return null;
  let goHome = false;
  let goEnd = false;
  if (key === 'Home') goHome = true;
  else if (key === 'End') goEnd = true;
  else return null;

  const start = typeof selStart === 'number' ? selStart : 0;
  const end = typeof selEnd === 'number' ? selEnd : start;
  const bounds = contentCaretBounds(value, opts);
  if (goHome) {
    if (m.shiftKey) return { start: bounds.home, end: Math.max(end, bounds.home) };
    return { start: bounds.home, end: bounds.home };
  }
  if (m.shiftKey) return { start: Math.min(start, bounds.end), end: bounds.end };
  return { start: bounds.end, end: bounds.end };
}

export function shouldAllowJumpToBottom(
  key: string,
  mods: { metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean; shiftKey?: boolean },
  opts: { composerFocused?: boolean; inTextField?: boolean },
): boolean {
  const m = mods || {};
  const isJump =
    (key === 'End' && !m.altKey && !m.shiftKey) ||
    (key === 'ArrowDown' && (m.metaKey || m.ctrlKey) && !m.altKey && !m.shiftKey);
  if (!isJump) return false;
  const textFocused = !!(opts.composerFocused || opts.inTextField);
  if (textFocused && key === 'End' && !m.metaKey && !m.ctrlKey) return false;
  return true;
}
