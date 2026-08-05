// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic tests for 🎯T248 RHS drag-resize layout (sidebar width + fleet split).
// Run: node web/scripts/rhs_layout_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const RL = require('./rhs_layout.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('ok  -', name);
  } catch (e) {
    failed++;
    console.error('FAIL-', name);
    console.error('    ', e && e.stack ? e.stack.split('\n').slice(0, 6).join('\n     ') : e);
  }
}

function memStorage(seed) {
  const map = Object.assign({}, seed || {});
  return {
    getItem: function (k) {
      return Object.prototype.hasOwnProperty.call(map, k) ? map[k] : null;
    },
    setItem: function (k, v) { map[k] = String(v); },
    removeItem: function (k) { delete map[k]; },
    _map: map,
  };
}

console.log('rhs_layout_test (🎯T248)');

// ── Sidebar width floors ───────────────────────────────────────────

test('clampSidebarWidth enforces min sidebar and min chat floors', function () {
  const main = 1200;
  assert.strictEqual(RL.clampSidebarWidth(420, main), 420);
  assert.strictEqual(RL.clampSidebarWidth(50, main), RL.MIN_SIDEBAR_WIDTH);
  // Request huge width → leave min chat
  assert.strictEqual(RL.clampSidebarWidth(2000, main), main - RL.MIN_CHAT_WIDTH);
  // Cannot collapse to zero
  assert.ok(RL.clampSidebarWidth(0, main) >= RL.MIN_SIDEBAR_WIDTH);
  assert.ok(RL.clampSidebarWidth(-10, main) >= RL.MIN_SIDEBAR_WIDTH);
});

test('clampSidebarWidth on short main still keeps min sidebar', function () {
  const main = 400; // less than MIN_SIDEBAR + MIN_CHAT
  const w = RL.clampSidebarWidth(300, main);
  assert.ok(w >= RL.MIN_SIDEBAR_WIDTH);
  // max is max(minSide, main - minChat) = max(220, 120) = 220
  assert.strictEqual(w, RL.MIN_SIDEBAR_WIDTH);
});

test('sidebarWidthFromPointer maps border X to width', function () {
  const main = 1000;
  // pointer at 580 → width 420
  assert.strictEqual(RL.sidebarWidthFromPointer(580, main), 420);
  // drag left (smaller x) → wider sidebar, clamped by min chat
  const wide = RL.sidebarWidthFromPointer(100, main);
  assert.strictEqual(wide, main - RL.MIN_CHAT_WIDTH);
  // drag right → narrow sidebar, clamped by min sidebar
  const narrow = RL.sidebarWidthFromPointer(900, main);
  assert.strictEqual(narrow, RL.MIN_SIDEBAR_WIDTH);
});

// ── Fleet / bottom vertical split ──────────────────────────────────

test('clampFleetFraction enforces pixel floors (no zero panes)', function () {
  const splitH = 600;
  const handle = RL.SPLIT_HANDLE_PX;
  const usable = splitH - handle;
  // Default mid-ish stays mid-ish
  const mid = RL.clampFleetFraction(0.45, splitH);
  assert.ok(mid > 0.1 && mid < 0.9);
  // Zero request → min fleet px
  const lo = RL.clampFleetFraction(0, splitH);
  assert.ok(lo * usable >= RL.MIN_FLEET_PX - 0.5);
  // One request → leave min bottom
  const hi = RL.clampFleetFraction(1, splitH);
  assert.ok((1 - hi) * usable >= RL.MIN_BOTTOM_PX - 0.5);
});

test('fleetFractionFromPointer converts Y to fraction with floors', function () {
  const splitH = 500;
  const fTop = RL.fleetFractionFromPointer(0, splitH);
  assert.ok(fTop * (splitH - RL.SPLIT_HANDLE_PX) >= RL.MIN_FLEET_PX - 0.5);
  const fBot = RL.fleetFractionFromPointer(splitH, splitH);
  assert.ok((1 - fBot) * (splitH - RL.SPLIT_HANDLE_PX) >= RL.MIN_BOTTOM_PX - 0.5);
  const mid = RL.fleetFractionFromPointer(200, splitH);
  assert.ok(mid > 0 && mid < 1);
});

test('short split height does not NaN or yield unusable zeros', function () {
  const f = RL.clampFleetFraction(0.5, 100);
  assert.ok(Number.isFinite(f));
  assert.ok(f > 0 && f < 1);
  const f2 = RL.clampFleetFraction(0, 50);
  assert.ok(Number.isFinite(f2));
  assert.ok(f2 >= 0 && f2 <= 1);
});

// ── Persist ────────────────────────────────────────────────────────

test('save/load round-trip preserves width and fleet fraction', function () {
  const storage = memStorage();
  const state = { sidebarWidth: 360, fleetFraction: 0.33 };
  const r = RL.save(storage, state);
  assert.strictEqual(r.ok, true);
  const loaded = RL.load(storage);
  assert.strictEqual(loaded.ok, true);
  assert.strictEqual(loaded.present, true);
  assert.strictEqual(loaded.state.sidebarWidth, 360);
  assert.strictEqual(loaded.state.fleetFraction, 0.33);
  assert.ok(storage.getItem(RL.STORAGE_KEY) != null);
});

test('load missing key returns defaults without error', function () {
  const loaded = RL.load(memStorage());
  assert.strictEqual(loaded.ok, true);
  assert.strictEqual(loaded.present, false);
  assert.strictEqual(loaded.state.sidebarWidth, RL.DEFAULT_SIDEBAR_WIDTH);
  assert.strictEqual(loaded.state.fleetFraction, RL.DEFAULT_FLEET_FRACTION);
});

test('deserialize garbage falls back to defaults with ok:false', function () {
  const bad = RL.deserialize('{not json');
  assert.strictEqual(bad.ok, false);
  assert.strictEqual(bad.present, true);
  assert.strictEqual(bad.state.sidebarWidth, RL.DEFAULT_SIDEBAR_WIDTH);
});

test('stylesForState emits px width and percent flex-basis', function () {
  const s = RL.stylesForState({ sidebarWidth: 380, fleetFraction: 0.4 });
  assert.strictEqual(s.sidebarWidthPx, 380);
  assert.strictEqual(s.fleetFlexBasis, '40%');
});

test('reclamp applies live geometry after window resize', function () {
  const next = RL.reclamp(
    { sidebarWidth: 900, fleetFraction: 0.99 },
    1000,
    500
  );
  assert.strictEqual(next.sidebarWidth, 1000 - RL.MIN_CHAT_WIDTH);
  assert.ok(next.fleetFraction < 0.99);
  assert.ok((1 - next.fleetFraction) * (500 - RL.SPLIT_HANDLE_PX) >= RL.MIN_BOTTOM_PX - 0.5);
});

test('dragEnabled false below MIN_MAIN_FOR_DRAG (narrow residual)', function () {
  assert.strictEqual(RL.dragEnabled(RL.MIN_MAIN_FOR_DRAG), true);
  assert.strictEqual(RL.dragEnabled(RL.MIN_MAIN_FOR_DRAG - 1), false);
  assert.strictEqual(RL.dragEnabled(200), false);
});

// ── index.html wiring ──────────────────────────────────────────────

test('index.html loads RhsLayout and wires drag handles', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(/scripts\/rhs_layout\.js/.test(html), 'loads rhs_layout.js');
  assert.ok(/id="rhs-width-handle"/.test(html), 'width drag handle present');
  assert.ok(/id="rhs-split-handle"/.test(html), 'vertical split handle present');
  assert.ok(/id="rhs-split"/.test(html), 'rhs-split wrapper present');
  assert.ok(/RhsLayout\.bind/.test(html), 'bind() wired at boot');
  assert.ok(/🎯T248/.test(html) || /T248/.test(html), 'T248 marked in product');
  // Agents + bottom live inside split (order: agents, handle, bottom)
  const agentsIdx = html.indexOf('id="agents"');
  const splitHandleIdx = html.indexOf('id="rhs-split-handle"');
  const bottomIdx = html.indexOf('id="rhs-bottom"');
  assert.ok(agentsIdx > 0 && splitHandleIdx > agentsIdx && bottomIdx > splitHandleIdx,
    'DOM order: agents → split handle → rhs-bottom');
});

test('simulated drag sequence updates dimensions (pure state machine)', function () {
  // Emulate pointer moves through pure helpers — same path bind() uses.
  const mainW = 1280;
  const splitH = 640;
  let state = RL.defaultState();
  // Drag width handle left (x smaller → wider sidebar)
  state = {
    sidebarWidth: RL.sidebarWidthFromPointer(700, mainW),
    fleetFraction: state.fleetFraction,
  };
  state = RL.reclamp(state, mainW, splitH);
  assert.strictEqual(state.sidebarWidth, 580);
  // Drag fleet split down (more fleet)
  state = {
    sidebarWidth: state.sidebarWidth,
    fleetFraction: RL.fleetFractionFromPointer(400, splitH),
  };
  state = RL.reclamp(state, mainW, splitH);
  assert.ok(state.fleetFraction > 0.5);
  const styles = RL.stylesForState(state);
  assert.strictEqual(styles.sidebarWidthPx, 580);
  assert.ok(parseFloat(styles.fleetFlexBasis) > 50);
  // Persist mid-drag end
  const storage = memStorage();
  RL.save(storage, state);
  const again = RL.load(storage).state;
  assert.strictEqual(again.sidebarWidth, 580);
  assert.ok(Math.abs(again.fleetFraction - state.fleetFraction) < 1e-9);
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nall passed');
