// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T361 — client half of the 🎯T355 UX-degrade path (pure layer + wiring).

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const UX = require('./owner_ux.js');

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

test('yield_hydrate hint is recognised on the level frame that carries it', function () {
  assert.strictEqual(UX.isYieldHydrateFrame({ type: 'status', state: 'idle', ux: 'yield_hydrate' }), true);
  assert.strictEqual(UX.isYieldHydrateFrame({ type: 'status', state: 'thinking', ux: 'yield_hydrate' }), true);
  // Chrome-only level frames are not a yield request.
  assert.strictEqual(UX.isYieldHydrateFrame({ type: 'status', state: 'idle' }), false);
  assert.strictEqual(UX.isYieldHydrateFrame({ type: 'error', ux: 'yield_hydrate' }), false);
  assert.strictEqual(UX.isYieldHydrateFrame(null), false);
});

test('owner-interaction alert is its own degraded class', function () {
  const alert = 'owner interaction degraded: interaction_usable (ux_degraded) — unresolved for 42s after 5 recovery steps';
  assert.strictEqual(UX.isOwnerDegradeAlert(alert), true);
  assert.ok(UX.ownerDegradeReason(alert).indexOf('interaction_usable') === 0);
  // Not every error is this class — overseer-down keeps its own banner.
  assert.strictEqual(UX.isOwnerDegradeAlert('overseer is not running'), false);
  assert.strictEqual(UX.isOwnerDegradeAlert('message not delivered: prompt already in flight'), false);
  assert.strictEqual(UX.isOwnerDegradeAlert(''), false);
});

test('recovery frame clears by marker or by wording', function () {
  assert.strictEqual(UX.isOwnerRecoveredFrame({ type: 'status', ux: 'recovered' }), true);
  assert.strictEqual(UX.isOwnerRecoveredFrame({
    type: 'status', text: 'owner interaction recovered: interaction_usable (interaction_usable)',
  }), true);
  assert.strictEqual(UX.isOwnerRecoveredFrame({ type: 'status', state: 'idle', text: 'chat settled' }), false);
});

test('overseer-down outranks the interaction class in one banner', function () {
  assert.strictEqual(UX.bannerText({}), '');
  assert.ok(UX.bannerText({ ownerDegraded: true, ownerReason: 'send_landed (send_not_landed)' })
    .indexOf('Owner interaction degraded: send_landed') === 0);
  const both = UX.bannerText({
    overseerDown: true, overseerReason: 'overseer is not running',
    ownerDegraded: true, ownerReason: 'send_landed',
  });
  assert.ok(both.indexOf('Cockpit degraded') === 0, 'overseer-down text wins');
  assert.ok(both.indexOf('Owner interaction degraded') < 0, 'never two faults in one line');
});

test('composer-blocked is the owner question, not one widget attribute', function () {
  assert.strictEqual(UX.composerBlocked({}), false);
  assert.strictEqual(UX.composerBlocked({ sendDisabled: true }), true);
  assert.strictEqual(UX.composerBlocked({ inputDisabled: true }), true);
  assert.strictEqual(UX.composerBlocked({ overseerDown: true }), true);
  assert.strictEqual(UX.composerBlockedReason({ overseerDown: true, sendDisabled: true }), 'overseer_down');
  assert.strictEqual(UX.composerBlockedReason({ sendDisabled: true }), 'send_disabled');
  assert.strictEqual(UX.composerBlockedReason({}), '');
});

test('composer level is reported on the edge, and never into a dead socket', function () {
  assert.strictEqual(UX.shouldReportComposer(null, false, true), true, 'first report states the level');
  assert.strictEqual(UX.shouldReportComposer(false, false, true), false, 'no level change, no frame');
  assert.strictEqual(UX.shouldReportComposer(false, true, true), true);
  assert.strictEqual(UX.shouldReportComposer(true, false, true), true);
  assert.strictEqual(UX.shouldReportComposer(null, true, false), false, 'closed socket cannot report');
});

test('composer frame matches the server control-frame shape', function () {
  const blocked = JSON.parse(UX.composerStateFrame(true, 'overseer_down'));
  assert.strictEqual(blocked.type, 'ux_state');
  assert.strictEqual(blocked.composer_blocked, true);
  assert.strictEqual(blocked.reason, 'overseer_down');
  const ok = JSON.parse(UX.composerStateFrame(false, ''));
  assert.strictEqual(ok.composer_blocked, false);
  assert.ok(!('reason' in ok), 'an unblocked composer has no reason');
});

// Wiring gates: the pure layer is worthless if index.html never calls it.
test('index.html wires the three T361 halves', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/owner_ux.js'), 'owner_ux.js loaded');
  assert.ok(/isYieldHydrateFrame\(m\)\)\s*yieldAndHydrate/.test(html), 'yield/hydrate driven by the frame');
  assert.ok(/function yieldAndHydrate[\s\S]{0,900}cancelAnimationFrame[\s\S]{0,900}setTimeout/.test(html),
    'yield cancels queued paint work, hydrate runs on a later task');
  assert.ok(html.includes('setOwnerDegraded(true'), 'sticky banner raised on the alert');
  assert.ok(html.includes('isOwnerRecoveredFrame') && html.includes('setOwnerDegraded(false)'),
    'banner cleared by the server recovery frame');
  assert.ok(html.includes('reportComposerState'), 'composer level reported');
  assert.ok(html.includes('composerStateFrame'), 'composer report uses the wire shape');
});

// The alert is journaled; replaying it must not re-raise a banner for a gap
// that may be long closed.
test('owner_ux level: degraded raises, ok/recovered clears; journaled error is not a level', function () {
  assert.strictEqual(UX.ownerUXLevel({ type: 'status', owner_ux: 'degraded', text: 'owner interaction degraded: x' }), 'degraded');
  assert.strictEqual(UX.ownerUXLevel({ type: 'status', owner_ux: 'ok' }), 'ok');
  assert.strictEqual(UX.ownerUXLevel({ type: 'status', ux: 'recovered', text: 'owner interaction recovered: x' }), 'ok');
  assert.strictEqual(UX.ownerUXLevel({ type: 'history_meta', owner_ux: 'ok' }), 'ok');
  assert.strictEqual(UX.ownerUXLevel({ type: 'error', error: 'owner interaction degraded: interaction_usable (ux_degraded)' }), '');
});

test('sticky banner follows owner_ux level, not journaled error edges', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('ownerUXLevel') >= 0, 'hydrate/live apply owner_ux level');
  assert.ok(/typ === 'error'[\s\S]{0,800}Do not raise it from a journaled error/.test(html) ||
    html.indexOf('Do not raise it from a journaled error') >= 0,
    'journaled type=error no longer raises the banner');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('PASS owner_ux_test');
