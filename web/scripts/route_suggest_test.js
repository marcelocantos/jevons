// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const RS = require('./route_suggest.js');
const TR = require('./thread_route.js');

function test(name, fn) {
  try {
    fn();
    console.log('ok - ' + name);
  } catch (e) {
    console.error('not ok - ' + name);
    console.error(e);
    process.exitCode = 1;
  }
}

const threads = [
  { id: 'att-restic', title: 'restic backup', digest: 'restic snapshots prune', body: '' },
  { id: 'att-billing', title: 'billing nit', digest: 'invoice stripe', body: '' },
];

// ── planMainSend: default always main; suggestion only on match ──

test('T135 planMainSend match → wireMode main + suggestion', function () {
  const hit = TR.route("How's restic going?", threads);
  assert.strictEqual(hit.reason, 'match');
  const plan = RS.planMainSend(hit, { threads: threads, body: "How's restic going?" });
  assert.strictEqual(plan.wireMode, 'main');
  assert.ok(plan.suggestion);
  assert.strictEqual(plan.suggestion.threadId, 'att-restic');
  assert.strictEqual(plan.suggestion.title, 'restic backup');
  assert.strictEqual(plan.suggestion.body, "How's restic going?");
  assert.strictEqual(plan.suggestion.reason, 'match');
});

test('T135 planMainSend no-match → main, no suggestion', function () {
  const hit = TR.route('What time is lunch?', threads);
  const plan = RS.planMainSend(hit, { threads: threads, body: 'What time is lunch?' });
  assert.strictEqual(plan.wireMode, 'main');
  assert.strictEqual(plan.suggestion, null);
});

test('T135 planMainSend explicit-prefix → main, no suggestion', function () {
  const hit = TR.route('aside: restic status', threads);
  const plan = RS.planMainSend(hit, { threads: threads });
  assert.strictEqual(plan.wireMode, 'main');
  assert.strictEqual(plan.suggestion, null);
});

test('T135 planMainSend never rewrites wire (no aside text)', function () {
  const hit = { threadId: 'att-x', score: 0.9, reason: 'match' };
  const plan = RS.planMainSend(hit, { body: 'hello restic', title: 'x' });
  assert.strictEqual(plan.wireMode, 'main');
  // Pure planner does not produce attention wire or pursue side effects.
  assert.ok(!('wireText' in plan) || plan.wireText == null);
  assert.ok(!('pursue' in plan));
  assert.ok(!('rewrite' in plan));
});

test('T135 planAutoRouteAction match → steal false + suggestion', function () {
  const hit = TR.route("How's restic going?", threads);
  assert.strictEqual(hit.reason, 'match');
  const act = RS.planAutoRouteAction(hit, { threads: threads, body: "How's restic going?" });
  assert.strictEqual(act.steal, false);
  assert.ok(act.suggestion);
  assert.strictEqual(act.suggestion.threadId, 'att-restic');
});

test('T135 planAutoRouteAction no-match → steal false, null suggestion', function () {
  const hit = TR.route('What time is lunch?', threads);
  const act = RS.planAutoRouteAction(hit, { threads: threads });
  assert.strictEqual(act.steal, false);
  assert.strictEqual(act.suggestion, null);
});

// ── shouldShowSwitch / formatSwitchLabel ─────────────────────────

test('T135 shouldShowSwitch only on match with threadId', function () {
  assert.strictEqual(RS.shouldShowSwitch({ reason: 'match', threadId: 'att-1' }), true);
  assert.strictEqual(RS.shouldShowSwitch({ reason: 'no-match', threadId: null }), false);
  assert.strictEqual(RS.shouldShowSwitch({ reason: 'match', threadId: 'main' }), false);
  assert.strictEqual(RS.shouldShowSwitch({ reason: 'ambiguous', threadId: 'att-1' }), false);
  assert.strictEqual(RS.shouldShowSwitch(null), false);
});

test('T135 formatSwitchLabel', function () {
  assert.strictEqual(RS.formatSwitchLabel({ title: 'restic backup' }), 'Continue in: «restic backup»');
  assert.strictEqual(RS.formatSwitchLabel('billing nit'), 'Continue in: «billing nit»');
  assert.strictEqual(RS.formatSwitchLabel(''), 'Continue in: «thread»');
  assert.strictEqual(RS.formatSwitchLabel(null), 'Continue in: «thread»');
});

// ── planOptInSwitch ──────────────────────────────────────────────

test('T135 planOptInSwitch includes interrupt + deliver + body', function () {
  const sug = {
    threadId: 'att-restic',
    title: 'restic backup',
    body: "How's restic going?",
    score: 0.8,
  };
  const plan = RS.planOptInSwitch(sug);
  assert.strictEqual(plan.ok, true);
  assert.strictEqual(plan.interruptMain, true);
  assert.strictEqual(plan.deliverTo, 'att-restic');
  assert.strictEqual(plan.body, "How's restic going?");
  assert.strictEqual(plan.title, 'restic backup');
});

test('T135 planOptInSwitch prefers explicit body arg', function () {
  const plan = RS.planOptInSwitch(
    { threadId: 'att-1', body: 'stored' },
    'override body'
  );
  assert.strictEqual(plan.ok, true);
  assert.strictEqual(plan.body, 'override body');
  assert.strictEqual(plan.interruptMain, true);
});

test('T135 planOptInSwitch rejects empty / main', function () {
  assert.strictEqual(RS.planOptInSwitch({ threadId: 'main', body: 'x' }).ok, false);
  assert.strictEqual(RS.planOptInSwitch({ threadId: '', body: 'x' }).ok, false);
  assert.strictEqual(RS.planOptInSwitch({ threadId: 'att-1', body: '  ' }).ok, false);
});

// ── index.html: no silent pursue on T99 match in send() ──────────

test('T135 index.html send path does not silent-pursue on match', function () {
  const htmlPath = path.join(__dirname, '..', 'index.html');
  const html = fs.readFileSync(htmlPath, 'utf8');
  // The old T99 steal block: pursue + formatAsideWire after route match.
  // Must not remain as the default send path.
  const stealRe = /if\s*\(\s*hit\.threadId\s*&&\s*hit\.reason\s*===\s*['"]match['"]\s*\)\s*\{[\s\S]{0,400}?AttentionThreads\.pursue/;
  assert.ok(
    !stealRe.test(html),
    'must not pursue on route match inside send() without opt-in'
  );
  // Affordance + opt-in must exist.
  assert.ok(
    /RouteSuggest|planMainSend|planOptInSwitch|Continue in:/.test(html),
    'must wire RouteSuggest / Continue in affordance'
  );
  assert.ok(
    /optInRouteSwitch|route-switch|routeSwitch/.test(html),
    'must have opt-in switch handler'
  );
});

console.log(process.exitCode ? 'FAIL' : 'PASS route_suggest_test');
