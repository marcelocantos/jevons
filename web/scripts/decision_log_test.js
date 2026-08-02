// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for decision-log field builders.
// Run: node web/scripts/decision_log_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const DL = require('./decision_log.js');

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

// ── formatRouteDecision ──────────────────────────────────────────

test('formatRouteDecision includes reason, score, threadId', function () {
  const f = DL.formatRouteDecision({
    threadId: 'att-abc',
    score: 0.82,
    reason: 'match',
  });
  assert.strictEqual(f.component, 'ThreadRoute');
  assert.strictEqual(f.decision, 'route');
  assert.strictEqual(f.threadId, 'att-abc');
  assert.strictEqual(f.score, 0.82);
  assert.strictEqual(f.reason, 'match');
});

test('formatRouteDecision logs no-match with null threadId', function () {
  const f = DL.formatRouteDecision({ threadId: null, score: 0.1, reason: 'no-match' });
  assert.strictEqual(f.threadId, null);
  assert.strictEqual(f.reason, 'no-match');
  assert.ok(f.score === 0.1);
});

test('formatRouteDecision accepts corr', function () {
  const f = DL.formatRouteDecision({ threadId: null, score: 0, reason: 'empty' }, 'c-1');
  assert.strictEqual(f.corr, 'c-1');
});

// ── formatComposerDecision ───────────────────────────────────────

test('formatComposerDecision carries kind, command, purpose, threadId', function () {
  const f = DL.formatComposerDecision(
    { kind: 'send', purpose: 'file-target', threadId: 'att-1', routed: true },
    { command: 'target', draft: 'target: file this' }
  );
  assert.strictEqual(f.component, 'AttentionThreads');
  assert.strictEqual(f.decision, 'composer');
  assert.strictEqual(f.kind, 'send');
  assert.strictEqual(f.command, 'target');
  assert.strictEqual(f.purpose, 'file-target');
  assert.strictEqual(f.threadId, 'att-1');
  assert.strictEqual(f.routed, true);
  assert.ok(f.draft.indexOf('target:') === 0);
});

// ── formatSendDecision ───────────────────────────────────────────

test('formatSendDecision enqueue/send/interrupt actions', function () {
  assert.strictEqual(
    DL.formatSendDecision({ action: 'enqueue', text: 'later' }).action,
    'enqueue'
  );
  assert.strictEqual(
    DL.formatSendDecision({ action: 'send', interrupt: false, text: 'hi' }).action,
    'send'
  );
  const inter = DL.formatSendDecision({ action: 'send', interrupt: true, text: 'now' });
  assert.strictEqual(inter.action, 'interrupt');
  assert.strictEqual(inter.queueAction, 'send');
  assert.strictEqual(inter.interrupt, true);
});

// ── truncation + redaction ───────────────────────────────────────

test('truncateDraft caps at DRAFT_MAX (~120)', function () {
  const long = 'x'.repeat(200);
  const t = DL.truncateDraft(long);
  assert.ok(t.length <= DL.DRAFT_MAX);
  assert.ok(t.endsWith('…'));
  assert.strictEqual(DL.truncateDraft('short'), 'short');
});

test('formatSendDecision truncates long draft', function () {
  const f = DL.formatSendDecision({ action: 'send', text: 'y'.repeat(300) });
  assert.ok(f.draft.length <= DL.DRAFT_MAX);
});

test('sanitizeFields drops secret keys; no password/token in route fields', function () {
  const dirty = DL.sanitizeFields({
    threadId: 'a',
    password: 'nope',
    token: 'secret',
    api_key: 'k',
    Authorization: 'Bearer x',
    reason: 'match',
  });
  assert.strictEqual(dirty.threadId, 'a');
  assert.strictEqual(dirty.reason, 'match');
  assert.strictEqual(dirty.password, undefined);
  assert.strictEqual(dirty.token, undefined);
  assert.strictEqual(dirty.api_key, undefined);
  assert.strictEqual(dirty.Authorization, undefined);
  assert.ok(DL.isSecretKey('password'));
  assert.ok(DL.isSecretKey('access_token'));
  assert.ok(!DL.isSecretKey('threadId'));
});

test('formatFocusDecision main/pursue/park/dismiss', function () {
  const f = DL.formatFocusDecision('pursue', { threadId: 'att-9', from: 'main' });
  assert.strictEqual(f.decision, 'focus');
  assert.strictEqual(f.action, 'pursue');
  assert.strictEqual(f.threadId, 'att-9');
  assert.strictEqual(f.from, 'main');
});

// ── index.html wiring (optional greps) ───────────────────────────

test('index.html wires decision_log + jLog on route', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/decision_log.js'), 'must load decision_log.js');
  assert.ok(html.includes('DecisionLog'), 'must reference DecisionLog');
  assert.ok(
    /ThreadRoute\.route/.test(html) && /formatRouteDecision|jLog/.test(html),
    'send path must consult ThreadRoute and log decisions'
  );
  // Always log route (not only on match rewrite).
  assert.ok(
    html.includes('formatRouteDecision') || /jLog\([^)]*route/i.test(html),
    'route decision must be logged'
  );
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('PASS decision_log_test');
