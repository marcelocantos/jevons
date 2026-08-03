// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for 🎯T127 composer focus hotkey policy.
// Run: node web/scripts/composer_focus_test.js
// NOT Playwright — pure policy + mock activeElement.

'use strict';

const assert = require('assert');
const CF = require('./composer_focus.js');

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

// ── Binding docs ────────────────────────────────────────────────

test('HOTKEY is plain slash; hint documents the binding', function () {
  assert.strictEqual(CF.HOTKEY, '/');
  assert.ok(CF.HOTKEY_HINT.indexOf('/') >= 0);
  assert.ok(CF.HOTKEY_DOC.toLowerCase().indexOf('composer') >= 0 ||
            CF.HOTKEY_DOC.toLowerCase().indexOf('message') >= 0);
});

// ── isFocusComposerHotkey ───────────────────────────────────────

test('bare / is the focus-composer hotkey', function () {
  assert.strictEqual(CF.isFocusComposerHotkey('/', {}), true);
  assert.strictEqual(CF.isFocusComposerHotkey('Slash', {}), true);
});

test('modified / is not the focus-composer hotkey', function () {
  assert.strictEqual(CF.isFocusComposerHotkey('/', { metaKey: true }), false);
  assert.strictEqual(CF.isFocusComposerHotkey('/', { ctrlKey: true }), false);
  assert.strictEqual(CF.isFocusComposerHotkey('/', { altKey: true }), false);
});

test('other keys are not focus-composer', function () {
  assert.strictEqual(CF.isFocusComposerHotkey('End', {}), false);
  assert.strictEqual(CF.isFocusComposerHotkey('Home', {}), false);
  assert.strictEqual(CF.isFocusComposerHotkey('Escape', {}), false);
  assert.strictEqual(CF.isFocusComposerHotkey('a', {}), false);
  assert.strictEqual(CF.isFocusComposerHotkey('ArrowDown', { metaKey: true }), false);
});

// ── isEditableTarget ────────────────────────────────────────────

test('textarea / text input / contenteditable are editable', function () {
  assert.strictEqual(CF.isEditableTarget({ tagName: 'TEXTAREA' }), true);
  assert.strictEqual(CF.isEditableTarget({ tagName: 'INPUT', type: 'text' }), true);
  assert.strictEqual(CF.isEditableTarget({ tagName: 'INPUT' }), true);
  assert.strictEqual(CF.isEditableTarget({ tagName: 'INPUT', type: 'search' }), true);
  assert.strictEqual(CF.isEditableTarget({ tagName: 'DIV', isContentEditable: true }), true);
  assert.strictEqual(CF.isEditableTarget({ tagName: 'DIV', contentEditable: 'true' }), true);
});

test('buttons, fleet chrome, non-text inputs are not editable', function () {
  assert.strictEqual(CF.isEditableTarget({ tagName: 'BUTTON' }), false);
  assert.strictEqual(CF.isEditableTarget({ tagName: 'DIV' }), false);
  assert.strictEqual(CF.isEditableTarget({ tagName: 'A' }), false);
  assert.strictEqual(CF.isEditableTarget({ tagName: 'INPUT', type: 'button' }), false);
  assert.strictEqual(CF.isEditableTarget({ tagName: 'INPUT', type: 'checkbox' }), false);
  assert.strictEqual(CF.isEditableTarget(null), false);
});

// ── shouldFocusComposer ─────────────────────────────────────────

test('should focus when / and focus on non-composer chrome', function () {
  const button = { tagName: 'BUTTON' };
  const fleetNode = { tagName: 'DIV', className: 'agent-node' };
  assert.strictEqual(CF.shouldFocusComposer('/', {}, button), true);
  assert.strictEqual(CF.shouldFocusComposer('/', {}, fleetNode), true);
  assert.strictEqual(CF.shouldFocusComposer('/', {}, { tagName: 'BODY' }), true);
});

test('should NOT focus when already typing in an editable field', function () {
  const composer = { tagName: 'TEXTAREA', id: 'input' };
  const mermaidPaste = { tagName: 'TEXTAREA', id: 'mvp-paste' };
  assert.strictEqual(CF.shouldFocusComposer('/', {}, composer), false);
  assert.strictEqual(CF.shouldFocusComposer('/', {}, mermaidPaste), false);
  assert.strictEqual(CF.shouldFocusComposer('/', {}, { tagName: 'INPUT', type: 'text' }), false);
});

test('should NOT focus for non-hotkey or modified /', function () {
  assert.strictEqual(CF.shouldFocusComposer('End', {}, { tagName: 'BUTTON' }), false);
  assert.strictEqual(CF.shouldFocusComposer('/', { metaKey: true }, { tagName: 'BUTTON' }), false);
});

// ── tryFocusComposer (hermetic activeElement oracle) ────────────

test('T127: focus non-composer control, dispatch /, activeElement becomes composer', function () {
  let activeElement = { tagName: 'BUTTON', id: 'agent-inspect-refresh' };
  const composer = {
    tagName: 'TEXTAREA',
    id: 'input',
    focus: function () { activeElement = this; },
  };
  const r = CF.tryFocusComposer({ key: '/' }, composer, activeElement);
  assert.strictEqual(r.didFocus, true);
  assert.strictEqual(r.reason, 'focused');
  // Acceptance: after shortcut, activeElement is the composer input.
  assert.strictEqual(activeElement, composer);
  assert.strictEqual(activeElement.id, 'input');
});

test('T127: works from RHS fleet / inspect-like chrome mocks', function () {
  const surfaces = [
    { tagName: 'DIV', id: 'agents' },
    { tagName: 'DIV', id: 'agent-inspect-body' },
    { tagName: 'BUTTON', id: 'open-viz-btn' },
    { tagName: 'SPAN', className: 'attn-chip' },
  ];
  surfaces.forEach(function (el) {
    let active = el;
    const composer = {
      tagName: 'TEXTAREA',
      id: 'input',
      focus: function () { active = this; },
    };
    const r = CF.tryFocusComposer({ key: '/' }, composer, el);
    assert.strictEqual(r.didFocus, true, 'surface ' + (el.id || el.className));
    assert.strictEqual(active, composer);
  });
});

test('T127: does not steal / while composer focused (typing)', function () {
  let activeElement = null;
  const composer = {
    tagName: 'TEXTAREA',
    id: 'input',
    focus: function () { activeElement = this; },
  };
  activeElement = composer;
  const r = CF.tryFocusComposer({ key: '/' }, composer, activeElement);
  assert.strictEqual(r.didFocus, false);
  assert.strictEqual(r.reason, 'already-focused');
});

test('T127: does not steal / from other textareas (mermaid paste)', function () {
  let activeElement = { tagName: 'TEXTAREA', id: 'mvp-paste' };
  const composer = {
    tagName: 'TEXTAREA',
    id: 'input',
    focus: function () { activeElement = this; },
  };
  const r = CF.tryFocusComposer({ key: '/' }, composer, activeElement);
  assert.strictEqual(r.didFocus, false);
  assert.strictEqual(activeElement.id, 'mvp-paste');
});

test('T127: missing composer is a no-op', function () {
  const r = CF.tryFocusComposer({ key: '/' }, null, { tagName: 'BUTTON' });
  assert.strictEqual(r.didFocus, false);
  assert.strictEqual(r.reason, 'no-composer');
});

// ── Non-conflict with T126 / T119 bindings ──────────────────────

test('T127 binding does not claim Home/End or jump-to-bottom keys', function () {
  assert.strictEqual(CF.isFocusComposerHotkey('Home', {}), false);
  assert.strictEqual(CF.isFocusComposerHotkey('End', {}), false);
  assert.strictEqual(CF.isFocusComposerHotkey('ArrowDown', { metaKey: true }), false);
  assert.strictEqual(CF.isFocusComposerHotkey('ArrowDown', { ctrlKey: true }), false);
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nAll composer_focus tests passed.');
