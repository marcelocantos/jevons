// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for composer key policy
// (🎯T126 / 🎯T149 Home/End; 🎯T132 Enter chords).
// Run: node web/scripts/composer_keys_test.js
// Pure policy — no DOM, no Playwright.

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const CK = require('./composer_keys.js');
const VL = require('./virtual_list.js');
const WC = require('./wispr_context.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('ok  -', name);
  } catch (e) {
    failed++;
    console.error('FAIL-', name);
    console.error('    ', e && e.stack ? e.stack.split('\n').slice(0, 4).join('\n     ') : e);
  }
}

function seedOpts(value) {
  return {
    seedPrefixLen: WC.seedPrefixLen(value),
    effectiveLength: WC.stripSeed(value).length,
    isSeedOnly: WC.isSeedOnly(value),
  };
}

// ── caret positions (field-content policy) ──────────────────────────

test('T126 Home → selectionStart 0 for multi-char value', function () {
  const value = 'hello world';
  assert.strictEqual(CK.caretAfterHome(value, 5), 0);
  const sel = CK.selectionAfterHomeEnd('Home', value, 5, 5, {});
  assert.deepStrictEqual(sel, { start: 0, end: 0 });
});

test('T126 End → caret at end of content', function () {
  const value = 'hello world';
  assert.strictEqual(CK.caretAfterEnd(value, 0), value.length);
  const sel = CK.selectionAfterHomeEnd('End', value, 0, 0, {});
  assert.deepStrictEqual(sel, { start: value.length, end: value.length });
});

test('T126 Shift+Home/End extend selection within field', function () {
  const value = 'abcdef';
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, 3, 3, { shiftKey: true }),
    { start: 0, end: 3 }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('End', value, 2, 2, { shiftKey: true }),
    { start: 2, end: 6 }
  );
});

test('T126 Alt Home/End leave caret to other handlers', function () {
  assert.strictEqual(CK.selectionAfterHomeEnd('End', 'ab', 1, 1, { altKey: true }), null);
  assert.strictEqual(CK.selectionAfterHomeEnd('ArrowLeft', 'ab', 1, 1, {}), null);
});

// ── 🎯T149 seed-aware bounds ────────────────────────────────────────

test('T149 seed-only Home/End collapse to insert point (after EMPTY_SEED)', function () {
  const value = WC.EMPTY_SEED;
  assert.ok(WC.isSeedOnly(value));
  assert.strictEqual(WC.seedPrefixLen(value), WC.EMPTY_SEED.length);
  assert.strictEqual(WC.stripSeed(value), '');
  const opts = seedOpts(value);
  const bounds = CK.contentCaretBounds(value, opts);
  assert.deepStrictEqual(bounds, { home: value.length, end: value.length });
  // Caret mid-seed → End snaps to insert point (visible move, not no-op).
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('End', value, 1, 1, {}, opts),
    { start: value.length, end: value.length }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, 1, 1, {}, opts),
    { start: value.length, end: value.length }
  );
  // Already at insert point → both stay (empty-field correct).
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, value.length, value.length, {}, opts),
    { start: value.length, end: value.length }
  );
});

test('T149 seed+text Home skips seed; End at full length', function () {
  const value = WC.EMPTY_SEED + 'hello';
  const opts = seedOpts(value);
  assert.strictEqual(opts.seedPrefixLen, WC.EMPTY_SEED.length);
  assert.strictEqual(opts.effectiveLength, 5);
  assert.strictEqual(opts.isSeedOnly, false);
  assert.deepStrictEqual(CK.contentCaretBounds(value, opts), {
    home: WC.EMPTY_SEED.length,
    end: value.length,
  });
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, value.length, value.length, {}, opts),
    { start: WC.EMPTY_SEED.length, end: WC.EMPTY_SEED.length }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('End', value, WC.EMPTY_SEED.length, WC.EMPTY_SEED.length, {}, opts),
    { start: value.length, end: value.length }
  );
  // Shift+Home selects effective text only (not the invisible seed).
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, value.length, value.length, { shiftKey: true }, opts),
    { start: WC.EMPTY_SEED.length, end: value.length }
  );
});

test('T149 real draft without seed still field-wide Home/End', function () {
  const value = 'draft text';
  const opts = seedOpts(value);
  assert.strictEqual(opts.seedPrefixLen, 0);
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, 5, 5, {}, opts),
    { start: 0, end: 0 }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('End', value, 0, 0, {}, opts),
    { start: value.length, end: value.length }
  );
});

test('T149 Meta/Ctrl+ArrowLeft/Right are field ends', function () {
  const value = 'abcdef';
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('ArrowLeft', value, 4, 4, { metaKey: true }),
    { start: 0, end: 0 }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('ArrowRight', value, 1, 1, { metaKey: true }),
    { start: 6, end: 6 }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('ArrowLeft', value, 4, 4, { ctrlKey: true }),
    { start: 0, end: 0 }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('ArrowRight', value, 1, 1, { ctrlKey: true }),
    { start: 6, end: 6 }
  );
  // Shift extends.
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('ArrowLeft', value, 4, 4, { metaKey: true, shiftKey: true }),
    { start: 0, end: 4 }
  );
  // Meta+ArrowDown stays null (jump-to-bottom owns it).
  assert.strictEqual(
    CK.selectionAfterHomeEnd('ArrowDown', value, 1, 1, { metaKey: true }),
    null
  );
});

test('T149 Meta+Arrow with seed+text uses effective bounds', function () {
  const value = WC.EMPTY_SEED + 'xyz';
  const opts = seedOpts(value);
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('ArrowLeft', value, value.length, value.length, { metaKey: true }, opts),
    { start: WC.EMPTY_SEED.length, end: WC.EMPTY_SEED.length }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('ArrowRight', value, opts.seedPrefixLen, opts.seedPrefixLen, { ctrlKey: true }, opts),
    { start: value.length, end: value.length }
  );
});

// ── jump-to-bottom must not steal Home/End while composer focused ───

test('T126 plain End does not jump when composer focused', function () {
  const opts = {
    composerFocused: true,
    isJumpHotkey: VL.isJumpToBottomHotkey,
  };
  assert.strictEqual(CK.shouldAllowJumpToBottom('End', {}, opts), false);
  // Home is never a jump key.
  assert.strictEqual(CK.shouldAllowJumpToBottom('Home', {}, opts), false);
  assert.strictEqual(VL.isJumpToBottomHotkey('Home', {}), false);
});

test('T126 plain End jumps when composer not focused', function () {
  const opts = {
    composerFocused: false,
    isJumpHotkey: VL.isJumpToBottomHotkey,
  };
  assert.strictEqual(CK.shouldAllowJumpToBottom('End', {}, opts), true);
});

test('T126 Meta/Ctrl+ArrowDown still jumps with composer focused', function () {
  const opts = {
    composerFocused: true,
    isJumpHotkey: VL.isJumpToBottomHotkey,
  };
  assert.strictEqual(
    CK.shouldAllowJumpToBottom('ArrowDown', { metaKey: true }, opts),
    true
  );
  assert.strictEqual(
    CK.shouldAllowJumpToBottom('ArrowDown', { ctrlKey: true }, opts),
    true
  );
});

test('T126 isComposerFocused only true for main input ref', function () {
  const input = { id: 'input' };
  const other = { id: 'other' };
  assert.strictEqual(CK.isComposerFocused(input, input), true);
  assert.strictEqual(CK.isComposerFocused(other, input), false);
  assert.strictEqual(CK.isComposerFocused(null, input), false);
  assert.ok(CK.isComposerTextControl('TEXTAREA'));
  assert.ok(CK.isComposerTextControl('input'));
  assert.ok(!CK.isComposerTextControl('DIV'));
});

// ── index wiring: script load + jump gate uses ComposerKeys ─────────

test('T126/T149 index.html loads composer_keys and gates jump on composer focus', function () {
  const htmlPath = path.join(__dirname, '..', 'index.html');
  const html = fs.readFileSync(htmlPath, 'utf8');
  assert.ok(html.includes('scripts/composer_keys.js'), 'must load composer_keys.js');
  assert.ok(
    html.includes('ComposerKeys') || html.includes('shouldAllowJumpToBottom'),
    'must wire ComposerKeys jump gate'
  );
  assert.ok(
    /selectionAfterHomeEnd|caretAfterHome|Home/.test(html) &&
      (html.includes('T126') || html.includes('T149')),
    'must handle Home/End caret on composer'
  );
  assert.ok(
    html.includes('seedPrefixLen') || html.includes('isSeedOnly') || html.includes('seedOpts'),
    'must pass seed-aware opts into selectionAfterHomeEnd (🎯T149)'
  );
});

test('T149 wispr_context exports seedPrefixLen / isSeedOnly / stripSeed / EMPTY_SEED', function () {
  assert.ok(typeof WC.EMPTY_SEED === 'string' && WC.EMPTY_SEED.length > 0);
  assert.strictEqual(typeof WC.seedPrefixLen, 'function');
  assert.strictEqual(typeof WC.isSeedOnly, 'function');
  assert.strictEqual(typeof WC.stripSeed, 'function');
  assert.strictEqual(WC.seedPrefixLen(WC.EMPTY_SEED + 'x'), WC.EMPTY_SEED.length);
  assert.strictEqual(WC.seedPrefixLen('plain'), 0);
  assert.strictEqual(WC.stripSeed(WC.EMPTY_SEED + 'hi'), 'hi');
  assert.ok(WC.isSeedOnly(WC.EMPTY_SEED));
  assert.ok(!WC.isSeedOnly('hi'));
});

// ── 🎯T132 Enter-chord policy ───────────────────────────────────────

test('T132 Ctrl+Enter → interrupt (immediate send), not Alt', function () {
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { ctrlKey: true }, { composerEmpty: false }),
    'interrupt'
  );
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { ctrlKey: true }, { composerEmpty: true }),
    'interrupt'
  );
  // Alt alone must never be interrupt/send.
  assert.notStrictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, { composerEmpty: false }),
    'interrupt'
  );
  assert.notStrictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, { composerEmpty: true }),
    'interrupt'
  );
  assert.notStrictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, { composerEmpty: false }),
    'send'
  );
});

test('T132 Alt+Enter empty → pop_last; non-empty → noop (no steal)', function () {
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, { composerEmpty: true }),
    'pop_last'
  );
  // Seed-only is effectively empty (caller sets composerEmpty from Wispr).
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, { composerEmpty: WC.isEffectivelyEmpty(WC.EMPTY_SEED) }),
    'pop_last'
  );
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, { composerEmpty: false }),
    'noop'
  );
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, {
      composerEmpty: WC.isEffectivelyEmpty('real draft'),
    }),
    'noop'
  );
});

test('T132 plain Enter → send; Shift+Enter → newline; Meta is not interrupt', function () {
  assert.strictEqual(CK.classifyEnterAction('Enter', {}, { composerEmpty: false }), 'send');
  assert.strictEqual(CK.classifyEnterAction('Enter', { metaKey: true }, {}), 'send');
  assert.strictEqual(CK.classifyEnterAction('Enter', { shiftKey: true }, {}), 'newline');
  // Shift wins over other modifiers for newline.
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { shiftKey: true, ctrlKey: true }, {}),
    'newline'
  );
  assert.strictEqual(CK.classifyEnterAction('ArrowUp', {}, {}), null);
});

test('T132 Ctrl+Alt+Enter still interrupt (Ctrl owns immediate send)', function () {
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { ctrlKey: true, altKey: true }, { composerEmpty: true }),
    'interrupt'
  );
});

test('T132 lastOwnerHistoryEntry returns tail text for pop_last wire-up', function () {
  assert.strictEqual(CK.lastOwnerHistoryEntry(null), null);
  assert.strictEqual(CK.lastOwnerHistoryEntry([]), null);
  const hist = [
    { text: 'first', el: { id: 1 } },
    { text: 'most recent owner', el: { id: 2 } },
  ];
  const last = CK.lastOwnerHistoryEntry(hist);
  assert.ok(last);
  assert.strictEqual(last.text, 'most recent owner');
  assert.strictEqual(last.index, 1);
  assert.strictEqual(last.el.id, 2);
});

test('T132 index.html wires Ctrl+Enter interrupt and Alt+Enter pop_last', function () {
  const htmlPath = path.join(__dirname, '..', 'index.html');
  const html = fs.readFileSync(htmlPath, 'utf8');
  assert.ok(html.includes('classifyEnterAction') || html.includes('T132'),
    'must use classifyEnterAction / T132 Enter policy');
  assert.ok(html.includes('popLastOwnerAsInterjection') || html.includes('pop_last'),
    'must wire empty Alt+Enter pop path');
  assert.ok(
    /Ctrl\+Enter|Control\+Enter/.test(html) && /interrupt|interject/.test(html),
    'UI must document Ctrl+Enter interject/immediate send'
  );
  assert.ok(
    /Alt\+Enter/.test(html) && /pop/.test(html.toLowerCase()),
    'UI must document Alt+Enter empty pop'
  );
  // Must not treat Alt alone as interrupt chord in the send path.
  assert.ok(
    !/interrupt\s*=\s*!!\s*\(?e\.altKey/.test(html) &&
      !/interrupt:\s*!!e\.altKey/.test(html),
    'must not set interrupt from altKey alone'
  );
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('PASS composer_keys_test');
