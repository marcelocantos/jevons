// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Wispr / dictation helpers (🎯T80 / T183).
 * Tidy is the vanilla index.html insert; seed-only class is the T183
 * visibility gate so a restored draft is never transparent.
 */

const EMPTY_SEED = '\u200B.\u200B';
const INVISIBLE_FORMAT_RE = /[\u200B\u200C\u200D\u2060\uFEFF]/g;

/** Light tidy on a bulk/empty-field dictation insert (vanilla T80). */
export function tidyDictationInsert(s: string): string {
  let t = String(s).replace(/^\s+|\s+$/g, '');
  if (!t) return t;
  if (/[.?!]/.test(t) && t !== t.toLowerCase()) return t;
  if (/^[a-z]/.test(t)) t = t.charAt(0).toUpperCase() + t.slice(1);
  if (!/[.?!]$/.test(t)) t += '.';
  return t;
}

function stripInvisibleFormat(value: string): string {
  return String(value ?? '').replace(INVISIBLE_FORMAT_RE, '');
}

function collapseWs(text: string): string {
  return String(text ?? '').replace(/\s+/g, ' ').trim();
}

function isSeedShapedResidue(value: string): boolean {
  const s = collapseWs(stripInvisibleFormat(value));
  return s === '' || s === '.';
}

export function stripSeed(value: string): string {
  let s = String(value ?? '');
  while (s.indexOf(EMPTY_SEED) === 0) s = s.slice(EMPTY_SEED.length);
  return s;
}

/** Length of a leading EMPTY_SEED prefix (0 if none). Caret bounds (T149). */
export function seedPrefixLen(value: string): number {
  let s = String(value ?? '');
  let n = 0;
  while (s.indexOf(EMPTY_SEED) === 0) {
    n += EMPTY_SEED.length;
    s = s.slice(EMPTY_SEED.length);
  }
  return n;
}

/** No real owner draft: '', whitespace, EMPTY_SEED, or seed-shaped residue. */
export function isEffectivelyEmpty(value: string): boolean {
  return isSeedShapedResidue(stripSeed(value));
}

/** True when the value is only EMPTY_SEED residue (transparent class). */
export function isSeedOnly(value: string): boolean {
  const s = String(value ?? '');
  if (!s) return false;
  return isSeedShapedResidue(stripSeed(s));
}

/** T183: restored non-empty draft must not request the seed-only class. */
export function needsSeedOnlyClass(value: string): boolean {
  return isSeedOnly(value);
}

export { EMPTY_SEED };
