// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for decision-log field builders (🎯T120).
// Run: node web/scripts/decision_log_test.js
// Contract: docs/design/logging-telemetry-audit.md §3.2

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

// ── formatRouteDecision (T99 opacity) ────────────────────────────

test('formatRouteDecision: component=thread_route, decision=reason, score+threadId', function () {
  const f = DL.formatRouteDecision({
    threadId: 'att-abc',
    score: 0.82,
    reason: 'match',
  });
  assert.strictEqual(f.component, 'thread_route');
  assert.strictEqual(f.decision, 'match');
  assert.strictEqual(f.reason, 'match');
  assert.strictEqual(f.threadId, 'att-abc');
  assert.strictEqual(f.score, 0.82);
});

test('formatRouteDecision logs no-match with null threadId', function () {
  const f = DL.formatRouteDecision({ threadId: null, score: 0.1, reason: 'no-match' });
  assert.strictEqual(f.decision, 'no-match');
  assert.strictEqual(f.threadId, null);
  assert.ok(f.score === 0.1);
});

test('formatRouteDecision accepts corr', function () {
  const f = DL.formatRouteDecision({ threadId: null, score: 0, reason: 'empty' }, 'c-1');
  assert.strictEqual(f.corr, 'c-1');
});

// ── formatComposerDecision ───────────────────────────────────────

test('formatComposerDecision: component=attention, decision=kind', function () {
  const f = DL.formatComposerDecision(
    { kind: 'send', purpose: 'file-target', threadId: 'att-1', routed: true },
    { command: 'target', draft: 'target: file this' }
  );
  assert.strictEqual(f.component, 'attention');
  assert.strictEqual(f.decision, 'send');
  assert.strictEqual(f.kind, 'send');
  assert.strictEqual(f.command, 'target');
  assert.strictEqual(f.purpose, 'file-target');
  assert.strictEqual(f.threadId, 'att-1');
  assert.strictEqual(f.routed, true);
  assert.ok(f.draft.indexOf('target:') === 0);
});

// ── formatSendDecision ───────────────────────────────────────────

test('formatSendDecision: component=send_queue, decision=enqueue|send|interrupt', function () {
  const enq = DL.formatSendDecision({ action: 'enqueue', text: 'later' });
  assert.strictEqual(enq.component, 'send_queue');
  assert.strictEqual(enq.decision, 'enqueue');

  const send = DL.formatSendDecision({ action: 'send', interrupt: false, text: 'hi' });
  assert.strictEqual(send.decision, 'send');

  const inter = DL.formatSendDecision({ action: 'send', interrupt: true, text: 'now' });
  assert.strictEqual(inter.decision, 'interrupt');
  assert.strictEqual(inter.interrupt, true);
});

// ── history (T120.4) ─────────────────────────────────────────────

test('formatHistoryDecision hydrate_page carries bounds + corr', function () {
  const f = DL.formatHistoryDecision('hydrate_page', {
    before: 400,
    after: 200,
    lines: 50,
    corr: 'c-hist',
  });
  assert.strictEqual(f.component, 'history');
  assert.strictEqual(f.decision, 'hydrate_page');
  assert.strictEqual(f.before, 400);
  assert.strictEqual(f.after, 200);
  assert.strictEqual(f.lines, 50);
  assert.strictEqual(f.corr, 'c-hist');
});

test('formatConnectDecision open carries conn_id concurrent corr', function () {
  const f = DL.formatConnectDecision('open', {
    conn_id: 'abc',
    concurrent: 2,
    ms: 0,
    corr: 'c1',
  });
  assert.strictEqual(f.component, 'chat_conn');
  assert.strictEqual(f.decision, 'open');
  assert.strictEqual(f.conn_id, 'abc');
  assert.strictEqual(f.concurrent, 2);
  assert.strictEqual(f.corr, 'c1');
});

test('formatConnectDecision history_meta carries replay metrics', function () {
  const f = DL.formatConnectDecision('history_meta', {
    conn_id: 'abc',
    ms: 120,
    frames: 10000,
    bytes: 1500000,
    replay_ms: 80,
  });
  assert.strictEqual(f.frames, 10000);
  assert.strictEqual(f.bytes, 1500000);
  assert.strictEqual(f.replay_ms, 80);
  assert.strictEqual(f.ms, 120);
});

test('decisionMsg prefixes decision.', function () {
  assert.strictEqual(DL.decisionMsg('thread_route'), 'decision.thread_route');
  assert.strictEqual(DL.decisionMsg('history'), 'decision.history');
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

test('sanitizeFields drops secret keys', function () {
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
  assert.ok(DL.isSecretKey('password'));
  assert.ok(!DL.isSecretKey('threadId'));
});

test('formatFocusDecision main/pursue/park/dismiss', function () {
  const f = DL.formatFocusDecision('pursue', { threadId: 'att-9', from: 'main' });
  assert.strictEqual(f.component, 'attention');
  assert.strictEqual(f.decision, 'pursue');
  assert.strictEqual(f.threadId, 'att-9');
  assert.strictEqual(f.from, 'main');
});

// ── index.html wiring ────────────────────────────────────────────

test('index.html wires decision_log + always logs route + history hydrate', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/decision_log.js'), 'must load decision_log.js');
  assert.ok(html.includes('DecisionLog'), 'must reference DecisionLog');
  assert.ok(html.includes('formatRouteDecision'), 'route decision must be logged');
  assert.ok(html.includes('formatHistoryDecision') || html.includes('hydrate_page'),
    'history hydrate must be logged (T120.4)');
  assert.ok(html.includes('pageCorr') || /corr:\s*pageCorr|corr:\s*corr/.test(html) ||
    html.includes('pageSessionCorr'),
    'page-session corr should be wired');
  assert.ok(/decision\.thread_route|decisionMsg\(['"]thread_route/.test(html),
    'msg convention decision.thread_route');
  // 🎯T259: progressive hydrate rate-limit + sampled page logs.
  assert.ok(html.includes('HISTORY_PAGE_GAP_MS'),
    'T259 must rate-limit progressive /api/history pages');
  assert.ok(html.includes('HISTORY_PAGE_LOG_EVERY'),
    'T259 must sample hydrate_page info logs');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('PASS decision_log_test');
