// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T21 — Wispr Flow document-mode context for the Jevons composer.
//
// Diagnosis: empty isolated chat textarea → Wispr chat-casual (weak
// punctuation). Sentence-shaped content in/near the field → document
// mode (capitalised, punctuated, including "?"). History lives outside
// the composer, so empty-field dictation has no style anchor.
//
// Fix = context for Wispr, not post-hoc grammar/`?` fixers on send:
//   A) Live aria-describedby region with recent transcript excerpts
//      (sentence-shaped continuation context).
//   B) Optional empty-composer seed (invisible sentence-shaped anchor)
//      stripped before wire so it never pollutes send payload.
//
// DOM-free pure helpers so Node hermetic tests can require(); browser
// glue lives in index.html.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.WisprContext = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const LIVE_REGION_ID = 'wispr-context';
  const STATIC_HINT_ID = 'input-hint';
  const DEFAULT_MAX_TURNS = 6;
  const DEFAULT_EXCERPT_CHARS = 220;

  // Bootstrap always present in the live region so empty history still
  // exposes sentence-shaped document context to STT / a11y bridges.
  const BOOTSTRAP =
    'Previous messages are complete sentences with capital letters and ' +
    'punctuation. Continue in the same document style. Questions end with ' +
    'a question mark.';

  // Fallback B: zero-width spaces around a period — sentence-shaped for
  // Wispr field inspection, not meaningfully visible in the textarea.
  // Must be stripped before wire (prepareWireText / stripSeed).
  const EMPTY_SEED = '\u200B.\u200B';

  function roleLabel(role) {
    const r = String(role || '').toLowerCase();
    if (r === 'user' || r === 'owner' || r === 'human') return 'Owner';
    if (r === 'assistant' || r === 'jevons' || r === 'model') return 'Assistant';
    return 'Speaker';
  }

  function collapseWs(text) {
    return String(text == null ? '' : text).replace(/\s+/g, ' ').trim();
  }

  // True when text already looks sentence-shaped (cap start and/or terminal punct).
  function isSentenceShaped(text) {
    const s = collapseWs(text);
    if (!s) return false;
    if (/[.!?]$/.test(s)) return true;
    // Capital letter start + internal punctuation is enough of an anchor.
    if (/^[A-Z]/.test(s) && /[.!?,;:]/.test(s)) return true;
    return false;
  }

  // Capitalise first alphabetic char; ensure terminal . ! or ?.
  function ensureSentence(text) {
    let s = collapseWs(text);
    if (!s) return '';
    // Capitalise first letter without dropping leading quotes/brackets.
    s = s.replace(/^([^A-Za-z]*)([a-z])/, function (_, pre, ch) {
      return pre + ch.toUpperCase();
    });
    if (!/^[A-Z]/.test(s.replace(/^[^A-Za-z]+/, ''))) {
      // Still no capital letter — force first alpha if any.
      s = s.replace(/[a-zA-Z]/, function (ch) { return ch.toUpperCase(); });
    }
    if (!/[.!?]$/.test(s)) s = s + '.';
    return s;
  }

  function excerptTurn(text, maxChars) {
    const cap = typeof maxChars === 'number' && maxChars > 0
      ? maxChars
      : DEFAULT_EXCERPT_CHARS;
    let s = collapseWs(text);
    if (!s) return '';
    // Prefer first sentence-ish chunk; else hard cap.
    const m = s.match(/^.{1,200}?[.!?](?:\s|$)/);
    if (m) s = m[0].trim();
    if (s.length > cap) {
      s = s.slice(0, Math.max(1, cap - 1)).replace(/\s+\S*$/, '') + '…';
    }
    return ensureSentence(s);
  }

  /**
   * Build live-region text from recent turns.
   * @param {Array<{role?: string, text?: string}>} turns chronological
   * @param {{maxTurns?: number, excerptChars?: number, bootstrap?: string}} opts
   * @returns {string} non-empty sentence-shaped context
   */
  function buildContextText(turns, opts) {
    const o = opts || {};
    const maxTurns = typeof o.maxTurns === 'number' && o.maxTurns > 0
      ? o.maxTurns
      : DEFAULT_MAX_TURNS;
    const excerptChars = typeof o.excerptChars === 'number' && o.excerptChars > 0
      ? o.excerptChars
      : DEFAULT_EXCERPT_CHARS;
    const bootstrap = o.bootstrap != null ? String(o.bootstrap) : BOOTSTRAP;

    const list = Array.isArray(turns) ? turns : [];
    const slice = list.slice(-maxTurns);
    const lines = [];
    for (let i = 0; i < slice.length; i++) {
      const t = slice[i] || {};
      const ex = excerptTurn(t.text, excerptChars);
      if (!ex) continue;
      lines.push(roleLabel(t.role) + ': ' + ex);
    }

    const body = lines.length
      ? ('Recent conversation:\n' + lines.join('\n'))
      : '';
    const out = [ensureSentence(bootstrap), body].filter(Boolean).join('\n');
    return out || ensureSentence(BOOTSTRAP);
  }

  /** aria-describedby value: static hint + live region ids. */
  function describedByIds(staticId, liveId) {
    const parts = [];
    if (staticId) parts.push(String(staticId));
    if (liveId) parts.push(String(liveId));
    return parts.join(' ');
  }

  function hasSeedPrefix(value) {
    const s = value == null ? '' : String(value);
    return s.indexOf(EMPTY_SEED) === 0;
  }

  /** Strip empty-composer seed only; never strip real user punctuation. */
  function stripSeed(value) {
    let s = value == null ? '' : String(value);
    while (hasSeedPrefix(s)) {
      s = s.slice(EMPTY_SEED.length);
    }
    return s;
  }

  /**
   * Wire-bound text: seed removed. Does not invent/fix grammar or `?`.
   * Caller still applies .trim() as product policy requires.
   */
  function prepareWireText(value) {
    return stripSeed(value);
  }

  function isEffectivelyEmpty(value) {
    return collapseWs(prepareWireText(value)) === '';
  }

  /**
   * When composer is empty of real draft, ensure seed is present.
   * Returns the value to assign to the textarea (may equal input).
   */
  function applySeedIfEmpty(value) {
    if (!isEffectivelyEmpty(value)) {
      // Real draft — ensure seed is not left as a prefix ahead of user text
      // after partial deletes; strip only the seed prefix if present.
      return stripSeed(value);
    }
    return EMPTY_SEED;
  }

  function needsSeed(value) {
    return isEffectivelyEmpty(value);
  }

  // 🎯T133: CSS class on #input when only EMPTY_SEED (or seed-like empty) is present.
  // Transparent text hides the period; caret-color keeps the caret visible.
  const SEED_ONLY_CLASS = 'composer-seed-only';

  /**
   * True when the composer value has no real draft and is not a blank string —
   * i.e. seed (or seed-shaped empty content) is what would render. Blank '' is
   * not seed-only (nothing to hide yet).
   */
  function isSeedOnly(value) {
    const s = value == null ? '' : String(value);
    if (!s) return false;
    return isEffectivelyEmpty(s);
  }

  /** Alias for class-toggle callers / tests. */
  function needsSeedOnlyClass(value) {
    return isSeedOnly(value);
  }

  /**
   * Key policy: Backspace on seed-only clears the whole seed in one stroke
   * (no char-by-char ZWSP/period thrash that can flash a visible '.').
   * Returns { consume: true, value: '' } when the event should be handled;
   * null when the browser default should run.
   */
  function handleSeedBackspace(value, key) {
    if (key !== 'Backspace') return null;
    if (!isSeedOnly(value)) return null;
    return { consume: true, value: '' };
  }

  return {
    LIVE_REGION_ID: LIVE_REGION_ID,
    STATIC_HINT_ID: STATIC_HINT_ID,
    DEFAULT_MAX_TURNS: DEFAULT_MAX_TURNS,
    DEFAULT_EXCERPT_CHARS: DEFAULT_EXCERPT_CHARS,
    BOOTSTRAP: BOOTSTRAP,
    EMPTY_SEED: EMPTY_SEED,
    SEED_ONLY_CLASS: SEED_ONLY_CLASS,
    roleLabel: roleLabel,
    isSentenceShaped: isSentenceShaped,
    ensureSentence: ensureSentence,
    excerptTurn: excerptTurn,
    buildContextText: buildContextText,
    describedByIds: describedByIds,
    hasSeedPrefix: hasSeedPrefix,
    stripSeed: stripSeed,
    prepareWireText: prepareWireText,
    isEffectivelyEmpty: isEffectivelyEmpty,
    applySeedIfEmpty: applySeedIfEmpty,
    needsSeed: needsSeed,
    isSeedOnly: isSeedOnly,
    needsSeedOnlyClass: needsSeedOnlyClass,
    handleSeedBackspace: handleSeedBackspace,
  };
}));
