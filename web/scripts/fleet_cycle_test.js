// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for 🎯T370 fleet tree cycle key policy.
// Run: node web/scripts/fleet_cycle_test.js
// NOT Playwright — pure chord/order/step policy + index.html wiring greps.
// Real focus + preventDefault live in
// scripts/chat-ui-test/t370-fleet-cycle-test.js.

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const FC = require('./fleet_cycle.js');

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

// Product-shaped tree: root overseer, a PO, two workers, one aside, plus a
// virtual portfolio folder (T200) that is chrome rather than a cycle stop.
function rows() {
  return [
    { name: 'jevons', purpose: 'overseer', selectable: true },
    { name: 'personal', purpose: 'portfolio', selectable: false },
    { name: 'jevons-po', purpose: 'work', selectable: true },
    { name: 'jv-t370-tree-cycle', purpose: 'work', selectable: true },
    { name: 'att-abc123', purpose: 'aside', selectable: true },
  ];
}
const ORDER = ['jevons', 'jevons-po', 'jv-t370-tree-cycle', 'att-abc123'];

function chord(key, over) {
  return Object.assign({ key: key, metaKey: true, shiftKey: true }, over || {});
}

// ── Binding docs ────────────────────────────────────────────────

test('chord doc names both directions and the root→main rule', function () {
  assert.ok(FC.CHORD_HINT.indexOf('[') >= 0 && FC.CHORD_HINT.indexOf(']') >= 0);
  const doc = FC.CHORD_DOC.toLowerCase();
  assert.ok(doc.indexOf('cmd+shift+]') >= 0, 'forward chord documented');
  assert.ok(doc.indexOf('cmd+shift+[') >= 0, 'reverse chord documented');
  assert.ok(doc.indexOf('overseer') >= 0 && doc.indexOf('main message box') >= 0,
    'root→main composer documented');
});

// ── fleetCycleDirection ─────────────────────────────────────────

test('Cmd+Shift+] is forward, Cmd+Shift+[ is backward', function () {
  assert.strictEqual(FC.fleetCycleDirection(']', chord(']')), FC.FORWARD);
  assert.strictEqual(FC.fleetCycleDirection('[', chord('[')), FC.BACKWARD);
});

test('shifted brace glyphs resolve the same as the bracket keys', function () {
  // US layouts deliver `}`/`{` once Shift is held; the chord must survive it.
  assert.strictEqual(FC.fleetCycleDirection('}', chord('}')), FC.FORWARD);
  assert.strictEqual(FC.fleetCycleDirection('{', chord('{')), FC.BACKWARD);
});

test('physical code decides when the layout gives an unrelated key', function () {
  // Non-US layouts move brackets entirely; `code` is the stable fallback.
  assert.strictEqual(
    FC.fleetCycleDirection('å', chord('å', { code: 'BracketRight' })), FC.FORWARD);
  assert.strictEqual(
    FC.fleetCycleDirection('¨', chord('¨', { code: 'BracketLeft' })), FC.BACKWARD);
});

test('bare and partly-modified brackets are not the chord', function () {
  // Typing `[` in a message box must never move the fleet selection.
  assert.strictEqual(FC.fleetCycleDirection('[', { }), 0);
  assert.strictEqual(FC.fleetCycleDirection('[', { metaKey: true }), 0);
  assert.strictEqual(FC.fleetCycleDirection('[', { shiftKey: true }), 0);
});

test('Ctrl or Alt held rejects the chord', function () {
  assert.strictEqual(FC.fleetCycleDirection(']', chord(']', { ctrlKey: true })), 0);
  assert.strictEqual(FC.fleetCycleDirection(']', chord(']', { altKey: true })), 0);
});

test('unrelated keys are never the chord', function () {
  assert.strictEqual(FC.fleetCycleDirection('Tab', chord('Tab')), 0);
  assert.strictEqual(FC.fleetCycleDirection('a', chord('a')), 0);
  assert.strictEqual(FC.fleetCycleDirection('', chord('')), 0);
});

// ── fleetCycleOrder ─────────────────────────────────────────────

test('order is paint order, includes root, drops portfolio folders', function () {
  assert.deepStrictEqual(FC.fleetCycleOrder(rows()), ORDER);
});

test('purpose=portfolio is dropped even when selectable is unset', function () {
  const out = FC.fleetCycleOrder([
    { name: 'jevons' },
    { name: 'personal', purpose: 'portfolio' },
    { name: 'jevons-po' },
  ]);
  assert.deepStrictEqual(out, ['jevons', 'jevons-po']);
});

test('hidden rows are not cycle stops', function () {
  // Collapsed/hidden subtrees follow the tree UI; the chord does not
  // resurrect rows the owner cannot see.
  const out = FC.fleetCycleOrder([
    { name: 'jevons' },
    { name: 'jevons-po' },
    { name: 'jv-hidden', hidden: true },
  ]);
  assert.deepStrictEqual(out, ['jevons', 'jevons-po']);
});

test('duplicate names collapse to one stop', function () {
  const out = FC.fleetCycleOrder([
    { name: 'jevons' }, { name: 'jevons-po' }, { name: 'jevons-po' },
  ]);
  assert.deepStrictEqual(out, ['jevons', 'jevons-po']);
});

test('descriptor rows keyed by `key` work too', function () {
  const out = FC.fleetCycleOrder([
    { key: 'jevons', selectable: true },
    { key: 'personal', selectable: false },
    { key: 'jevons-po', selectable: true },
  ]);
  assert.deepStrictEqual(out, ['jevons', 'jevons-po']);
});

test('root is prepended when the tree has not painted it', function () {
  assert.deepStrictEqual(FC.fleetCycleOrder([{ name: 'jevons-po' }]),
    ['jevons', 'jevons-po']);
});

test('empty tree still yields the root stop', function () {
  assert.deepStrictEqual(FC.fleetCycleOrder([]), ['jevons']);
  assert.deepStrictEqual(FC.fleetCycleOrder(null), ['jevons']);
});

test('root is not duplicated when already painted, wherever it sits', function () {
  assert.deepStrictEqual(FC.fleetCycleOrder([{ name: 'a' }, { name: 'jevons' }]),
    ['a', 'jevons']);
});

// ── currentCycleNode ────────────────────────────────────────────

test('empty selection resolves to root (main chat is the overseer stream)', function () {
  assert.strictEqual(FC.currentCycleNode(ORDER, null), 'jevons');
  assert.strictEqual(FC.currentCycleNode(ORDER, ''), 'jevons');
});

test('a live selection resolves to itself', function () {
  assert.strictEqual(FC.currentCycleNode(ORDER, 'jevons-po'), 'jevons-po');
});

test('a reaped selection falls back to root rather than stranding', function () {
  assert.strictEqual(FC.currentCycleNode(ORDER, 'jv-gone'), 'jevons');
});

// ── stepFleetCycle ──────────────────────────────────────────────

test('forward walks the order and wraps at the end', function () {
  assert.strictEqual(FC.stepFleetCycle(ORDER, 'jevons', FC.FORWARD), 'jevons-po');
  assert.strictEqual(FC.stepFleetCycle(ORDER, 'att-abc123', FC.FORWARD), 'jevons');
});

test('backward walks the order and wraps at the start', function () {
  assert.strictEqual(FC.stepFleetCycle(ORDER, 'jevons-po', FC.BACKWARD), 'jevons');
  assert.strictEqual(FC.stepFleetCycle(ORDER, 'jevons', FC.BACKWARD), 'att-abc123');
});

test('a full forward lap returns to the start, root included', function () {
  let at = 'jevons';
  const seen = [at];
  for (let i = 0; i < ORDER.length; i++) {
    at = FC.stepFleetCycle(ORDER, at, FC.FORWARD);
    seen.push(at);
  }
  assert.deepStrictEqual(seen, ORDER.concat(['jevons']));
});

test('forward then backward is the identity at every stop', function () {
  ORDER.forEach(function (n) {
    const there = FC.stepFleetCycle(ORDER, n, FC.FORWARD);
    assert.strictEqual(FC.stepFleetCycle(ORDER, there, FC.BACKWARD), n);
  });
});

test('single-stop order stays put in both directions', function () {
  assert.strictEqual(FC.stepFleetCycle(['jevons'], 'jevons', FC.FORWARD), 'jevons');
  assert.strictEqual(FC.stepFleetCycle(['jevons'], 'jevons', FC.BACKWARD), 'jevons');
});

// ── planFleetCycle ──────────────────────────────────────────────

test('non-chord keydown is not claimed', function () {
  const p = FC.planFleetCycle({ key: 'Tab', metaKey: true, shiftKey: true }, { rows: rows() });
  assert.strictEqual(p.claim, false);
  assert.strictEqual(p.reason, 'not-chord');
  assert.strictEqual(p.focus, null);
});

test('root → first worker: select it, focus the sidebar box', function () {
  const p = FC.planFleetCycle(chord(']'), { rows: rows(), selected: null });
  assert.strictEqual(p.claim, true);
  assert.strictEqual(p.target, 'jevons-po');
  assert.strictEqual(p.isRoot, false);
  assert.strictEqual(p.focus, 'sidebar');
  assert.strictEqual(p.select, true);
});

test('last node wraps to root: focus main, not a missing sidebar', function () {
  const p = FC.planFleetCycle(chord(']'), { rows: rows(), selected: 'att-abc123' });
  assert.strictEqual(p.target, 'jevons');
  assert.strictEqual(p.isRoot, true);
  assert.strictEqual(p.focus, 'main');
  // Still a real selection change: the sidebar selection must be cleared.
  assert.strictEqual(p.select, true);
});

test('reverse from the first worker lands on root and focuses main', function () {
  const p = FC.planFleetCycle(chord('['), { rows: rows(), selected: 'jevons-po' });
  assert.strictEqual(p.direction, FC.BACKWARD);
  assert.strictEqual(p.target, 'jevons');
  assert.strictEqual(p.focus, 'main');
});

test('reverse from root wraps to the last node', function () {
  const p = FC.planFleetCycle(chord('['), { rows: rows(), selected: null });
  assert.strictEqual(p.target, 'att-abc123');
  assert.strictEqual(p.focus, 'sidebar');
});

test('empty-beyond-root: chord is claimed, stays on root, no re-select', function () {
  // The acceptance case with no fleet: the key is still ours (never falls
  // through to the browser) and `select` is false so the caller does not
  // call selectAgent with the current name — that toggles selection off.
  const p = FC.planFleetCycle(chord(']'), { rows: [], selected: null });
  assert.strictEqual(p.claim, true);
  assert.strictEqual(p.target, 'jevons');
  assert.strictEqual(p.isRoot, true);
  assert.strictEqual(p.focus, 'main');
  assert.strictEqual(p.select, false);
});

test('empty-beyond-root reverse behaves identically', function () {
  const p = FC.planFleetCycle(chord('['), { rows: [], selected: null });
  assert.strictEqual(p.claim, true);
  assert.strictEqual(p.target, 'jevons');
  assert.strictEqual(p.select, false);
});

test('a stale selection is cleared even when the step lands on root', function () {
  // Resolves to root for stepping, but selectedAgent still points at the
  // dead agent — select must stay true so the sidebar is actually cleared.
  const p = FC.planFleetCycle(chord('['), { rows: [], selected: 'jv-gone' });
  assert.strictEqual(p.target, 'jevons');
  assert.strictEqual(p.select, true);
});

test('explicit order overrides row derivation', function () {
  const p = FC.planFleetCycle(chord(']'), { order: ['jevons', 'x'], selected: null });
  assert.strictEqual(p.target, 'x');
});

test('an order explicitly emptied is not claimed', function () {
  const p = FC.planFleetCycle(chord(']'), { order: [], selected: null });
  assert.strictEqual(p.claim, false);
  assert.strictEqual(p.reason, 'no-nodes');
});

test('custom overseer name drives the root→main rule', function () {
  const ctx = { rows: [{ name: 'boss' }, { name: 'w1' }], overseer: 'boss', selected: 'w1' };
  const p = FC.planFleetCycle(chord(']'), ctx);
  assert.strictEqual(p.target, 'boss');
  assert.strictEqual(p.isRoot, true);
  assert.strictEqual(p.focus, 'main');
});

test('overseer match is case-insensitive', function () {
  assert.ok(FC.isOverseerName('Jevons'));
  assert.ok(!FC.isOverseerName('jevons-po'));
});

// ── keypress suppression: Firefox's tab switch reads the keypress ──────
//
// Gecko decides NEXT_TAB/PREVIOUS_TAB in a system-group `keypress` listener
// that returns early when the event is already cancelled. Cancelling only
// the keydown leaves that outcome dependent on an engine internal this repo
// cannot observe, so the chord's keypress is cancelled too. These pin the
// predicate that drives it — including that it stays narrow enough to leave
// ordinary bracket typing alone.

test('the chord keypress is claimed for suppression, both directions', function () {
  assert.ok(FC.claimsChordKeypress({ key: '}', code: 'BracketRight', metaKey: true, shiftKey: true }));
  assert.ok(FC.claimsChordKeypress({ key: '{', code: 'BracketLeft', metaKey: true, shiftKey: true }));
});

test('keypress suppression is as narrow as the keydown chord', function () {
  // Plain typing of brackets/braces must survive untouched.
  assert.ok(!FC.claimsChordKeypress({ key: ']', code: 'BracketRight' }));
  assert.ok(!FC.claimsChordKeypress({ key: '}', code: 'BracketRight', shiftKey: true }));
  // Meta without Shift is the browser's own bracket chord (back/forward).
  assert.ok(!FC.claimsChordKeypress({ key: ']', code: 'BracketRight', metaKey: true }));
  // Ctrl/Alt variants belong to other bindings.
  assert.ok(!FC.claimsChordKeypress({ key: '}', code: 'BracketRight', metaKey: true, shiftKey: true, ctrlKey: true }));
  assert.ok(!FC.claimsChordKeypress({ key: '}', code: 'BracketRight', metaKey: true, shiftKey: true, altKey: true }));
  // Unrelated keys, and junk.
  assert.ok(!FC.claimsChordKeypress({ key: 'a', code: 'KeyA', metaKey: true, shiftKey: true }));
  assert.ok(!FC.claimsChordKeypress(null));
  assert.ok(!FC.claimsChordKeypress({}));
});

test('index.html cancels the chord keypress as well as the keydown', function () {
  assert.ok(/addEventListener\('keypress'/.test(html),
    'index.html must register a keypress listener for the chord');
  const at = html.indexOf('FleetCycle.claimsChordKeypress');
  assert.ok(at > 0, 'index.html must consult FleetCycle.claimsChordKeypress');
  const near = html.slice(at, at + 400);
  assert.ok(/preventDefault\(\)/.test(near),
    'a claimed chord keypress must be cancelled so Firefox skips the tab switch');
  // The keypress path is suppression only — the keydown path owns behaviour.
  assert.ok(!/selectAgent\(/.test(near),
    'the keypress path must not select anything (keydown already did)');
  assert.ok(!/\.focus\(\)/.test(near),
    'the keypress path must not move focus (keydown already did)');
});

// ── index.html wiring ───────────────────────────────────────────

test('index.html loads fleet_cycle.js', function () {
  assert.ok(/scripts\/fleet_cycle\.js/.test(html),
    'index.html must load web/scripts/fleet_cycle.js');
});

test('index.html applies the plan on the document keydown path', function () {
  assert.ok(/FleetCycle\.planFleetCycle/.test(html),
    'index.html must call FleetCycle.planFleetCycle');
  const call = html.slice(html.indexOf('FleetCycle.planFleetCycle'));
  const near = call.slice(0, 1400);
  assert.ok(/selected:\s*selectedAgent/.test(near),
    'current selection passed as ctx.selected');
  assert.ok(/rows:/.test(near), 'painted rows passed as ctx.rows');
  assert.ok(/preventDefault\(\)/.test(near),
    'claimed chord must preventDefault so the key is not double-handled');
  assert.ok(/selectAgent\(/.test(near),
    'plan must drive the existing selectAgent path (no selection fork)');
});

test('the cycle reuses existing composers — no new send/display fork (🎯T372)', function () {
  const call = html.slice(html.indexOf('FleetCycle.planFleetCycle'));
  const near = call.slice(0, 1400);
  // Root focuses the main box; every other stop focuses the sidebar box.
  assert.ok(/focus\s*===\s*'main'/.test(near), 'root branch focuses the main composer');
  assert.ok(/\.focus\(\)/.test(near), 'plan actually focuses a message box');
  assert.ok(!/new ConversationWidget|ConversationWidget\.mount/.test(near),
    'must not mount another conversation surface for the cycle');
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nAll fleet_cycle tests passed.');
