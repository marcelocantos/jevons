// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T300 — thrash oracle for the "Loading earlier…" chrome.
// The owner's complaint was a strobe: background history pages toggled the
// overlay once per page. These tests script the real trigger sequences
// (connect hydrate, progressive loop, sentinel, reconnect, graph open/close)
// and assert the chrome never appears — while a genuine owner-triggered
// load-earlier shows exactly once and stays put until its fetch settles.

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const HL = require('./history_loading.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('ok  -', name);
  } catch (e) {
    failed++;
    console.error('FAIL -', name, e.message);
  }
}

// Background page: what progressiveLoadRemainingHistory / the sentinel do.
function autoPage(older) {
  return [
    { type: 'begin', reason: HL.AUTO },
    { type: 'settle', older: older },
  ];
}
// Owner-triggered page: loadEarlier({owner: true}).
function ownerPage(older) {
  return [
    { type: 'begin', reason: HL.OWNER },
    { type: 'settle', older: older },
  ];
}

test('connect hydrate + progressive pages never show chrome', function () {
  const events = [{ type: 'reset' }, { type: 'older', older: 1200 }]
    .concat(autoPage(1000), autoPage(800), autoPage(600), autoPage(400),
            autoPage(200), autoPage(0));
  const r = HL.run(events);
  assert.strictEqual(r.shows, 0, 'no chrome during background hydrate');
  assert.ok(r.trace.every(function (v) { return v === false; }), 'never visible');
});

test('sentinel false positives never show chrome', function () {
  // IntersectionObserver fires repeatedly with a 400px rootMargin on a short
  // list; each fire nudges loadEarlier with no owner intent behind it.
  let events = [{ type: 'older', older: 600 }];
  for (let i = 0; i < 20; i++) events = events.concat(autoPage(600 - i * 10));
  const r = HL.run(events);
  assert.strictEqual(r.shows, 0, 'sentinel nudges are silent');
});

test('reconnect replay never shows chrome', function () {
  const events = [{ type: 'older', older: 500 }]
    .concat(autoPage(300))
    .concat([{ type: 'reset' }, { type: 'older', older: 500 }])
    .concat(autoPage(300), autoPage(100), autoPage(0));
  const r = HL.run(events);
  assert.strictEqual(r.shows, 0, 'reconnect hydrate is silent');
});

test('graph open/close around background pages never shows chrome', function () {
  const events = [{ type: 'older', older: 900 }]
    .concat(autoPage(800))
    .concat([{ type: 'graph', open: true }])
    .concat(autoPage(700), autoPage(600))
    .concat([{ type: 'graph', open: false }])
    .concat(autoPage(500));
  const r = HL.run(events);
  assert.strictEqual(r.shows, 0, 'graph toggling does not flash chrome');
});

test('owner load-earlier shows once and stays until settle', function () {
  const s0 = HL.reduce(HL.initialState(), { type: 'older', older: 400 });
  const r = HL.run([{ type: 'begin', reason: HL.OWNER }], s0);
  assert.strictEqual(r.shows, 1, 'shown for the owner request');
  assert.strictEqual(r.visible, true, 'still up while the fetch is in flight');
  // Background pages landing underneath must not blink it off and on.
  const mid = HL.run([
    { type: 'begin', reason: HL.AUTO },
    { type: 'begin', reason: HL.AUTO },
  ], r.state);
  assert.strictEqual(mid.shows, 0, 'no re-show');
  assert.ok(mid.trace.every(function (v) { return v === true; }), 'stable while loading');
  const done = HL.run([{ type: 'settle', older: 200 }], mid.state);
  assert.strictEqual(done.visible, false, 'off once the fetch settles');
});

test('owner load-earlier total transitions across a full session = 1', function () {
  const events = [{ type: 'reset' }, { type: 'older', older: 1000 }]
    .concat(autoPage(800), autoPage(600))
    .concat([{ type: 'begin', reason: HL.OWNER },
             { type: 'begin', reason: HL.AUTO },
             { type: 'settle', older: 400 }])
    .concat(autoPage(200), autoPage(0));
  const r = HL.run(events);
  assert.strictEqual(r.shows, 1, 'exactly one appearance, the owner one');
  assert.strictEqual(r.visible, false, 'ends hidden');
});

test('no older pages remain → chrome off and stays off', function () {
  const s = HL.reduce(HL.initialState(), { type: 'older', older: 0 });
  assert.strictEqual(HL.isVisible(s), false);
  const r = HL.run([{ type: 'begin', reason: HL.OWNER }], s);
  assert.strictEqual(r.shows, 0, 'exhausted history cannot show chrome');
});

test('T274 graph suppress: owner load behind the panel stays hidden', function () {
  const s0 = HL.reduce(HL.initialState(), { type: 'older', older: 400 });
  const open = HL.run([
    { type: 'graph', open: true },
    { type: 'begin', reason: HL.OWNER },
  ], s0);
  assert.strictEqual(open.shows, 0, 'suppressed while large graph open');
  // Closing the panel must not resurrect a stale overlay (T274 residual drop).
  const closed = HL.run([{ type: 'graph', open: false }], open.state);
  assert.strictEqual(closed.visible, false, 'no stale overlay after close');
});

test('T483 auto pages are volume-capped at 0; owner pages are not', function () {
  assert.strictEqual(HL.AUTO_PAGES_MAX, 0, 'connect tail only');
  assert.strictEqual(HL.mayAutoPage(0), false, 'first auto page refused');
  assert.strictEqual(HL.mayAutoPage(0, {}), false);
  assert.strictEqual(HL.mayAutoPage(0, { owner: false }), false);
  assert.strictEqual(HL.mayAutoPage(99, { owner: true }), true, 'owner is uncapped');
  // A 263k-line leftover must not produce a single auto fetch.
  let auto = 0;
  while (HL.mayAutoPage(auto)) auto++;
  assert.strictEqual(auto, 0);
});

test('reasonFor maps loadEarlier options', function () {
  assert.strictEqual(HL.reasonFor(), HL.AUTO);
  assert.strictEqual(HL.reasonFor({}), HL.AUTO);
  assert.strictEqual(HL.reasonFor({ owner: false }), HL.AUTO);
  assert.strictEqual(HL.reasonFor({ owner: true }), HL.OWNER);
});

test('reduce does not mutate the input state', function () {
  const s = HL.reduce(HL.initialState(), { type: 'older', older: 5 });
  const next = HL.reduce(s, { type: 'begin', reason: HL.OWNER });
  assert.strictEqual(s.ownerActive, false, 'input untouched');
  assert.strictEqual(next.ownerActive, true);
});

test('index.html routes the chrome through the policy', function () {
  const html = fs.readFileSync(path.join(__dirname, '../index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/history_loading.js') >= 0, 'module loaded');
  assert.ok(html.indexOf('historyLoadingEvent(') >= 0, 'policy events emitted');
  // loadEarlier takes an owner flag and reports the reason to the policy.
  const leStart = html.indexOf('async function loadEarlier');
  assert.ok(leStart >= 0);
  const leEnd = html.indexOf('\nfunction ', leStart + 10);
  const leBody = html.slice(leStart, leEnd > leStart ? leEnd : leStart + 3000);
  assert.ok(leBody.indexOf('reasonFor') >= 0, 'loadEarlier classifies its caller');
  assert.ok(leBody.indexOf("type: 'settle'") >= 0, 'loadEarlier settles the policy');
  assert.ok(leBody.indexOf('showHistoryLoading(true)') < 0,
    'loadEarlier no longer forces the chrome on every page');
  // 🎯T274 suppression stays wired on both the fetch and the paint paths.
  assert.ok(leBody.indexOf('isLargeGraphPanelOpen') >= 0, 'T274 fetch guard kept');
  assert.ok(html.indexOf('isOwnerNearHistoryTop') >= 0, 'T483 sentinel is owner-at-top');
  assert.ok(html.indexOf('mayAutoPage') >= 0, 'T483 volume cap is wired');
  const start = html.indexOf('function startProgressiveHistoryLoad');
  const startEnd = html.indexOf('\nfunction ', start + 10);
  const startBody = html.slice(start, startEnd > start ? startEnd : start + 800);
  assert.ok(startBody.indexOf('progressiveLoadRemainingHistory()') < 0,
    'connect must not start the unbounded while (oldestIndex > 0) walk');
  const shStart = html.indexOf('function showHistoryLoading');
  assert.ok(shStart >= 0);
  const shEnd = html.indexOf('\nfunction ', shStart + 10);
  const shBody = html.slice(shStart, shEnd > shStart ? shEnd : shStart + 1500);
  assert.ok(shBody.indexOf('isLargeGraphPanelOpen') >= 0, 'T274 paint guard kept');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('PASS history_loading_test');
