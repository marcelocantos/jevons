// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for composer Home/End caret policy (🎯T126).
// Run: node web/scripts/composer_keys_test.js
// Pure policy — no DOM, no Playwright.

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const CK = require('./composer_keys.js');
const VL = require('./virtual_list.js');

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

test('T126 Meta/Ctrl Home End leave caret to other handlers', function () {
  assert.strictEqual(CK.selectionAfterHomeEnd('Home', 'ab', 1, 1, { metaKey: true }), null);
  assert.strictEqual(CK.selectionAfterHomeEnd('End', 'ab', 1, 1, { ctrlKey: true }), null);
  assert.strictEqual(CK.selectionAfterHomeEnd('End', 'ab', 1, 1, { altKey: true }), null);
  assert.strictEqual(CK.selectionAfterHomeEnd('ArrowLeft', 'ab', 1, 1, {}), null);
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

test('T126 index.html loads composer_keys and gates jump on composer focus', function () {
  const htmlPath = path.join(__dirname, '..', 'index.html');
  const html = fs.readFileSync(htmlPath, 'utf8');
  assert.ok(html.includes('scripts/composer_keys.js'), 'must load composer_keys.js');
  assert.ok(
    html.includes('ComposerKeys') || html.includes('shouldAllowJumpToBottom'),
    'must wire ComposerKeys jump gate'
  );
  assert.ok(
    /selectionAfterHomeEnd|caretAfterHome|Home/.test(html) &&
      html.includes('T126'),
    'must handle Home/End caret (🎯T126) on composer'
  );
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('PASS composer_keys_test');
