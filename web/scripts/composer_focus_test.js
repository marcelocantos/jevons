// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for 🎯T127 composer focus hotkey policy
// and 🎯T153 aggressive focus-return + wiring greps.
// Run: node web/scripts/composer_focus_test.js
// NOT Playwright — pure policy + mock activeElement + index.html greps.

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const CF = require('./composer_focus.js');

const htmlPath = path.join(__dirname, '..', 'index.html');
const html = fs.readFileSync(htmlPath, 'utf8');

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

// ── 🎯T153 focusComposer helper ─────────────────────────────────

test('T153: focusComposer forces focus onto the composer element', function () {
  let active = { tagName: 'BUTTON', className: 'msg-expand-tab' };
  const composer = {
    tagName: 'TEXTAREA',
    id: 'input',
    focus: function () { active = this; },
  };
  const r = CF.focusComposer(composer);
  assert.strictEqual(r.didFocus, true);
  assert.strictEqual(r.reason, 'focused');
  assert.strictEqual(active, composer);
  assert.strictEqual(active.id, 'input');
});

test('T153: focusComposer is unconditional (even when already on composer)', function () {
  let focusCalls = 0;
  const composer = {
    id: 'input',
    focus: function () { focusCalls++; },
  };
  const r = CF.focusComposer(composer);
  assert.strictEqual(r.didFocus, true);
  assert.strictEqual(focusCalls, 1);
});

test('T153: focusComposer no-ops without a composer', function () {
  const r = CF.focusComposer(null);
  assert.strictEqual(r.didFocus, false);
  assert.strictEqual(r.reason, 'no-composer');
});

// ── 🎯T153 wiring greps (index.html) ────────────────────────────

test('T153: expand tab creation sets tabIndex = -1 (not a Tab stop)', function () {
  // 🎯T480: pocket tab lives in ConversationWidget (one implementation).
  const cw = fs.readFileSync(path.join(__dirname, 'conversation_widget.js'), 'utf8');
  const ensureIdx = cw.indexOf('function ensureExpandToggle');
  assert.ok(ensureIdx >= 0, 'ensureExpandToggle must exist on ConversationWidget');
  const slice = cw.slice(ensureIdx, ensureIdx + 1800);
  assert.ok(
    /btn\.tabIndex\s*=\s*-1/.test(slice) || /tabindex\s*=\s*["']-1["']/.test(slice),
    'expand tab must set tabIndex=-1 in ensureExpandToggle'
  );
  assert.ok(
    /className\s*=\s*['"]msg-expand-tab['"]/.test(slice),
    'ensureExpandToggle creates .msg-expand-tab'
  );
});

test('T153: expand click path calls focusComposer', function () {
  const ensureIdx = html.indexOf('function ensureExpandToggle');
  assert.ok(ensureIdx >= 0, 'host keep ensureExpandToggle as the T153 seam');
  const slice = html.slice(ensureIdx, ensureIdx + 900);
  assert.ok(
    /onToggle[\s\S]*?focusComposer\s*\(/.test(slice),
    'expand/collapse onToggle must call focusComposer()'
  );
});

test('T153: send path returns focus via clearComposerAfterQueueOrSend → focusComposer', function () {
  const clearIdx = html.indexOf('function clearComposerAfterQueueOrSend');
  assert.ok(clearIdx >= 0, 'clearComposerAfterQueueOrSend must exist');
  const slice = html.slice(clearIdx, clearIdx + 700);
  assert.ok(
    /focusComposer\s*\(/.test(slice),
    'clearComposerAfterQueueOrSend (used by send/enqueue) must call focusComposer'
  );
  // send() must still route through clearComposer for wire sends.
  assert.ok(
    /clearComposerAfterQueueOrSend\s*\(/.test(html),
    'send path must use clearComposerAfterQueueOrSend'
  );
});

test('T153: route-switch is pointer-only and returns focus', function () {
  const attachIdx = html.indexOf('function attachRouteSwitch');
  assert.ok(attachIdx >= 0, 'attachRouteSwitch must exist');
  const slice = html.slice(attachIdx, attachIdx + 1200);
  assert.ok(
    /btn\.tabIndex\s*=\s*-1/.test(slice),
    'route-switch must set tabIndex=-1'
  );
  assert.ok(
    /focusComposer\s*\(/.test(slice),
    'route-switch click must call focusComposer'
  );
});

test('T153: target-aside auto-close returns focus to composer', function () {
  const idx = html.indexOf('function maybeCloseTargetAside');
  assert.ok(idx >= 0, 'maybeCloseTargetAside must exist');
  const slice = html.slice(idx, idx + 900);
  assert.ok(
    /focusComposer\s*\(/.test(slice),
    'maybeCloseTargetAside must call focusComposer'
  );
});

test('T153: mermaid bubble toolbar buttons are not Tab stops', function () {
  const idx = html.indexOf('function attachMermaidToolbar');
  assert.ok(idx >= 0, 'attachMermaidToolbar must exist');
  const slice = html.slice(idx, idx + 900);
  assert.ok(
    /btn\.tabIndex\s*=\s*-1/.test(slice),
    'mermaid toolbar buttons in bubbles must set tabIndex=-1'
  );
});

test('T153: page defines focusComposer helper wired to #input / ComposerFocus', function () {
  assert.ok(
    /function focusComposer\s*\(/.test(html),
    'index.html must define focusComposer()'
  );
  assert.ok(
    /ComposerFocus\.focusComposer/.test(html) || /focusComposer\(input\)/.test(html),
    'focusComposer must use ComposerFocus or input'
  );
});

// ── A+B: T106 tab bg must not regress under T153 ────────────────

test('T106: .msg-expand-tab still uses background: var(--bg) (no regress)', function () {
  // Base rule + user + jevons overrides all use transcript ground.
  assert.ok(
    /\.msg-expand-tab\s*\{[^}]*background:\s*var\(--bg\)/.test(html),
    'base .msg-expand-tab must use var(--bg)'
  );
  assert.ok(
    /\.msg\.user\s+\.msg-expand-tab\s*\{[^}]*background:\s*var\(--bg\)/.test(html),
    '.msg.user .msg-expand-tab must use var(--bg)'
  );
  assert.ok(
    /\.msg\.jevons\s+\.msg-expand-tab\s*\{[^}]*background:\s*var\(--bg\)/.test(html),
    '.msg.jevons .msg-expand-tab must use var(--bg)'
  );
});

// ── 🎯T366: Tab cycles main ↔ sidebar message boxes ────────────

// Minimal element mocks: only the properties isCycleStopFocusable reads.
function mkBox(extra) {
  const el = Object.assign({ focused: 0 }, extra || {});
  el.focus = function () { el.focused++; };
  return el;
}
function mkWrap(classes, extra) {
  const set = new Set(classes || []);
  return Object.assign({
    classList: { contains: function (c) { return set.has(c); } },
  }, extra || {});
}

// Standard shape: sidebar visible, Transcript pane active.
function visibleCtx(over) {
  const main = mkBox();
  const side = mkBox();
  return Object.assign({
    activeElement: main,
    mainEl: main,
    sidebarEl: side,
    sidebarComposerEl: mkWrap(['cw-composer', 'visible']),
    sidebarPaneEl: mkWrap(['rhs-tab-pane', 'active']),
  }, over || {});
}

test('T366: Tab from main composer focuses the sidebar composer', function () {
  const ctx = visibleCtx();
  const r = CF.applyComposerTabCycle({ key: 'Tab' }, ctx);
  assert.strictEqual(r.target, 'sidebar');
  assert.strictEqual(r.didFocus, true);
  assert.strictEqual(r.preventDefault, true);
  assert.strictEqual(ctx.sidebarEl.focused, 1);
  assert.strictEqual(ctx.mainEl.focused, 0);
});

test('T366: Tab from sidebar composer returns to main — stable two-stop cycle', function () {
  const ctx = visibleCtx();
  CF.applyComposerTabCycle({ key: 'Tab' }, ctx);
  ctx.activeElement = ctx.sidebarEl;
  const back = CF.applyComposerTabCycle({ key: 'Tab' }, ctx);
  assert.strictEqual(back.target, 'main');
  assert.strictEqual(ctx.mainEl.focused, 1);
  // Third press cycles out to the sidebar again — never a third stop.
  ctx.activeElement = ctx.mainEl;
  const again = CF.applyComposerTabCycle({ key: 'Tab' }, ctx);
  assert.strictEqual(again.target, 'sidebar');
  assert.strictEqual(ctx.sidebarEl.focused, 2);
});

test('T366: Shift+Tab is the documented reverse — also toggles the pair', function () {
  const ctx = visibleCtx();
  ctx.activeElement = ctx.sidebarEl;
  const r = CF.applyComposerTabCycle({ key: 'Tab', shiftKey: true }, ctx);
  assert.strictEqual(r.target, 'main');
  assert.strictEqual(ctx.mainEl.focused, 1);
  ctx.activeElement = ctx.mainEl;
  const fwd = CF.applyComposerTabCycle({ key: 'Tab', shiftKey: true }, ctx);
  assert.strictEqual(fwd.target, 'sidebar');
});

test('T366: hidden sidebar composer does not claim Tab (no trap)', function () {
  // Composer wrapper without `visible` = collapsed / no agent selected.
  const ctx = visibleCtx({ sidebarComposerEl: mkWrap(['cw-composer']) });
  const r = CF.applyComposerTabCycle({ key: 'Tab' }, ctx);
  assert.strictEqual(r.target, null);
  assert.strictEqual(r.didFocus, false);
  assert.strictEqual(r.preventDefault, false, 'must leave normal focus order alone');
  assert.strictEqual(r.reason, 'sidebar-unavailable');
  assert.strictEqual(ctx.sidebarEl.focused, 0);
});

test('T366: inactive Transcript pane does not claim Tab', function () {
  const ctx = visibleCtx({ sidebarPaneEl: mkWrap(['rhs-tab-pane']) });
  const r = CF.applyComposerTabCycle({ key: 'Tab' }, ctx);
  assert.strictEqual(r.target, null);
  assert.strictEqual(r.preventDefault, false);
});

test('T366: disabled or missing sidebar input does not claim Tab', function () {
  const disabled = visibleCtx();
  disabled.sidebarEl.disabled = true;
  assert.strictEqual(CF.applyComposerTabCycle({ key: 'Tab' }, disabled).target, null);

  const absent = visibleCtx({ sidebarEl: null });
  const r = CF.applyComposerTabCycle({ key: 'Tab' }, absent);
  assert.strictEqual(r.target, null);
  assert.strictEqual(r.preventDefault, false);
});

test('T366: focus outside both boxes is left to the browser', function () {
  const ctx = visibleCtx({ activeElement: mkBox() });
  const r = CF.applyComposerTabCycle({ key: 'Tab' }, ctx);
  assert.strictEqual(r.target, null);
  assert.strictEqual(r.reason, 'not-in-cycle');
});

test('T366: modified Tab (Cmd/Ctrl/Alt) is not the cycle chord', function () {
  ['metaKey', 'ctrlKey', 'altKey'].forEach(function (mod) {
    const ev = { key: 'Tab' };
    ev[mod] = true;
    const r = CF.applyComposerTabCycle(ev, visibleCtx());
    assert.strictEqual(r.target, null, mod + '+Tab must not cycle');
    assert.strictEqual(r.reason, 'not-tab');
  });
});

test('T366: non-Tab keys never enter the cycle', function () {
  ['/', 'Enter', 'a', 'ArrowDown'].forEach(function (k) {
    assert.strictEqual(CF.applyComposerTabCycle({ key: k }, visibleCtx()).target, null, k);
  });
  // code fallback still recognised (layouts that leave key unset).
  assert.strictEqual(CF.isComposerTabChord('', { code: 'Tab' }), true);
});

test('T366: sidebar exit only needs the main box focusable', function () {
  // Sidebar hidden mid-cycle (pane switched) — focus is already there, so
  // Tab must still be able to get back out to main.
  const ctx = visibleCtx({ sidebarComposerEl: mkWrap(['cw-composer']) });
  ctx.activeElement = ctx.sidebarEl;
  const r = CF.applyComposerTabCycle({ key: 'Tab' }, ctx);
  assert.strictEqual(r.target, 'main');
  assert.strictEqual(ctx.mainEl.focused, 1);
});

test('T366: index.html wires the Tab cycle on the document keydown path', function () {
  assert.ok(
    /ComposerFocus\.applyComposerTabCycle/.test(html),
    'index.html must call ComposerFocus.applyComposerTabCycle'
  );
  const call = html.slice(html.indexOf('ComposerFocus.applyComposerTabCycle'));
  assert.ok(/mainEl:\s*input/.test(call.slice(0, 600)), 'main composer passed as mainEl');
  assert.ok(
    /sidebarEl:\s*side/.test(call.slice(0, 600)),
    'sidebar composer passed as sidebarEl'
  );
  assert.ok(
    /sidebarPaneEl:\s*agentInspectEl/.test(call.slice(0, 600)),
    'Transcript pane passed so a collapsed pane cannot trap Tab'
  );
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nAll composer_focus tests passed.');
