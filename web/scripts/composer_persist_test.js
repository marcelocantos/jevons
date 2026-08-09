// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic tests for 🎯T239 composer draft + pending-send persistence.
// Run: node web/scripts/composer_persist_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const CP = require('./composer_persist.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('ok  -', name);
  } catch (e) {
    failed++;
    console.error('FAIL-', name);
    console.error('    ', e && e.stack ? e.stack.split('\n').slice(0, 5).join('\n     ') : e);
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

// ── Draft ──────────────────────────────────────────────────────────

test('saveDraft / loadDraft round-trip preserves multi-line body', function () {
  const storage = memStorage();
  const body = 'line one\nline two\n  indented';
  const r = CP.saveDraft(storage, body);
  assert.strictEqual(r.ok, true);
  // Simulate full page reload.
  const loaded = CP.loadDraft(storage);
  assert.strictEqual(loaded.ok, true);
  assert.strictEqual(loaded.present, true);
  assert.strictEqual(loaded.state.text, body);
  assert.ok(storage.getItem(CP.DRAFT_KEY) != null);
});

test('empty / whitespace draft clears storage', function () {
  const storage = memStorage();
  CP.saveDraft(storage, 'keep me');
  assert.ok(storage.getItem(CP.DRAFT_KEY));
  const r = CP.saveDraft(storage, '   ');
  assert.strictEqual(r.ok, true);
  assert.strictEqual(r.cleared, true);
  assert.strictEqual(storage.getItem(CP.DRAFT_KEY), null);
  const loaded = CP.loadDraft(storage);
  assert.strictEqual(loaded.state.text, '');
  assert.strictEqual(loaded.present, false);
});

test('fixture write draft → simulated reload → restore matches', function () {
  const storage = memStorage();
  CP.saveDraft(storage, 'owner typed this before restart');
  // Fresh load only from storage (new page).
  const again = CP.restoreDraft(storage, null);
  assert.strictEqual(again.ok, true);
  assert.strictEqual(again.text, 'owner typed this before restart');
  assert.strictEqual(again.present, true);
});

test('migrate legacy sessionStorage jevons-input into localStorage', function () {
  const local = memStorage();
  const session = memStorage();
  session.setItem(CP.LEGACY_SESSION_KEY, 'legacy draft body');
  const r = CP.restoreDraft(local, session);
  assert.strictEqual(r.ok, true);
  assert.strictEqual(r.text, 'legacy draft body');
  assert.strictEqual(r.migrated, true);
  assert.strictEqual(session.getItem(CP.LEGACY_SESSION_KEY), null);
  const reloaded = CP.loadDraft(local);
  assert.strictEqual(reloaded.state.text, 'legacy draft body');
});

test('localStorage wins over empty session migration', function () {
  const local = memStorage();
  const session = memStorage();
  CP.saveDraft(local, 'primary');
  session.setItem(CP.LEGACY_SESSION_KEY, 'should not win');
  const r = CP.restoreDraft(local, session);
  assert.strictEqual(r.text, 'primary');
  assert.ok(!r.migrated);
});

test('fail loud when draft key present but corrupt', function () {
  const storage = memStorage();
  storage.setItem(CP.DRAFT_KEY, '{not-valid-json');
  const loaded = CP.loadDraft(storage);
  assert.strictEqual(loaded.ok, false);
  assert.strictEqual(loaded.present, true);
  assert.ok(loaded.error && /parse failed/.test(loaded.error));
  const restored = CP.restoreDraft(storage, null);
  assert.strictEqual(restored.ok, false);
  assert.ok(restored.error);
});

// ── Pending ────────────────────────────────────────────────────────

test('stagePending then save/load preserves bodies', function () {
  const storage = memStorage();
  let p = CP.emptyPending();
  p = CP.stagePending(p, 'submitted while wire open');
  p = CP.stagePending(p, 'second unacked');
  CP.savePending(storage, p);
  const loaded = CP.loadPending(storage);
  assert.strictEqual(loaded.ok, true);
  assert.strictEqual(loaded.state.items.length, 2);
  assert.strictEqual(loaded.state.items[0].text, 'submitted while wire open');
  assert.strictEqual(loaded.state.items[1].text, 'second unacked');
});

test('stagePending dedupes identical body while waiting', function () {
  let p = CP.emptyPending();
  p = CP.stagePending(p, 'same');
  p = CP.stagePending(p, 'same');
  assert.strictEqual(p.items.length, 1);
});

test('ackPendingAgainstHistory drops matched; leaves unacked', function () {
  let p = CP.emptyPending();
  p = CP.stagePending(p, 'seen in chatlog');
  p = CP.stagePending(p, 'still missing');
  const r = CP.ackPendingAgainstHistory(p, ['older', 'seen in chatlog', 'newer']);
  assert.strictEqual(r.acked.length, 1);
  assert.strictEqual(r.acked[0].text, 'seen in chatlog');
  assert.strictEqual(r.state.items.length, 1);
  assert.strictEqual(r.state.items[0].text, 'still missing');
});

test('planRestore requeues unacked not in history or queue', function () {
  let p = CP.emptyPending();
  p = CP.stagePending(p, 'already on server');
  p = CP.stagePending(p, 'already queued');
  p = CP.stagePending(p, 'needs requeue');
  const plan = CP.planRestore({
    draftText: 'live draft',
    pendingState: p,
    historyTexts: ['already on server', 'other'],
    queueTexts: ['already queued'],
  });
  assert.strictEqual(plan.draftText, 'live draft');
  assert.deepStrictEqual(plan.requeueTexts, ['needs requeue']);
  assert.strictEqual(plan.pendingAfterAck.items.length, 2); // queued + needs (acked dropped)
  assert.strictEqual(plan.acked.length, 1);
  assert.strictEqual(plan.acked[0].text, 'already on server');
});

test('planRestore empty pending → no requeue', function () {
  const plan = CP.planRestore({
    draftText: 'x',
    pendingState: CP.emptyPending(),
    historyTexts: ['a'],
    queueTexts: [],
  });
  assert.deepStrictEqual(plan.requeueTexts, []);
  assert.strictEqual(plan.pendingAfterAck.items.length, 0);
});

test('fail loud when pending key present but corrupt', function () {
  const storage = memStorage();
  storage.setItem(CP.PENDING_KEY, 'not-json{{{');
  const loaded = CP.loadPending(storage);
  assert.strictEqual(loaded.ok, false);
  assert.strictEqual(loaded.present, true);
  assert.ok(/parse failed/.test(loaded.error));
});

test('historyHasText merge helper', function () {
  assert.ok(CP.historyHasText(['a', 'b'], 'b'));
  assert.ok(!CP.historyHasText(['a'], 'missing'));
  assert.ok(!CP.historyHasText([], 'x'));
});

// ── Full reload fixture (draft + queue + pending) ──────────────────

test('T239 full fixture: write draft+pending → reload → restore matches', function () {
  const storage = memStorage();
  // Owner typed draft, submitted one (staged pending), queue has another.
  CP.saveDraft(storage, 'composer draft multi\nline');
  let pending = CP.stagePending(CP.emptyPending(), 'wire-accepted unacked');
  CP.savePending(storage, pending);

  // Simulated reload
  const draftR = CP.restoreDraft(storage, null);
  const pendR = CP.loadPending(storage);
  assert.strictEqual(draftR.ok, true);
  assert.strictEqual(draftR.text, 'composer draft multi\nline');
  assert.strictEqual(pendR.ok, true);
  assert.strictEqual(pendR.state.items[0].text, 'wire-accepted unacked');

  // Server history does not yet have the pending → requeue
  const plan = CP.planRestore({
    draftText: draftR.text,
    pendingState: pendR.state,
    historyTexts: ['unrelated earlier message'],
    queueTexts: [],
  });
  assert.deepStrictEqual(plan.requeueTexts, ['wire-accepted unacked']);

  // After server has it → no requeue (no dupe)
  const plan2 = CP.planRestore({
    draftText: draftR.text,
    pendingState: pendR.state,
    historyTexts: ['wire-accepted unacked'],
    queueTexts: [],
  });
  assert.deepStrictEqual(plan2.requeueTexts, []);
  assert.strictEqual(plan2.pendingAfterAck.items.length, 0);
});

// ── index.html wiring greps ────────────────────────────────────────

test('index.html loads ComposerPersist and uses localStorage draft paths', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/composer_persist.js'), 'must load composer_persist.js');
  assert.ok(html.includes('ComposerPersist'), 'must reference ComposerPersist');
  assert.ok(html.includes('restoreDraft') || html.includes('ComposerPersist.restoreDraft'),
    'boot must restore draft via ComposerPersist');
  assert.ok(html.includes('saveDraft') || html.includes('persistComposerDraft'),
    'must persist draft on edit');
  assert.ok(html.includes('stagePending') || html.includes('persistPendingSend'),
    'must stage pending after wire accept');
  assert.ok(html.includes('planRestore') || html.includes('reconcilePendingSends'),
    'must reconcile pending after history');
  // Soft/hard onOpen must not wipe draft storage.
  const onOpen = html.match(/transport\.onOpen\s*=\s*\(\)\s*=>\s*\{[\s\S]*?\n\};/);
  assert.ok(onOpen, 'transport.onOpen must exist');
  assert.ok(
    !/clearDraft|removeItem\(['"]jevons-composer-draft/.test(onOpen[0]),
    'onOpen must not clear durable draft'
  );
});

test('embed.go and Makefile include composer_persist', function () {
  const embed = fs.readFileSync(path.join(__dirname, '..', 'embed.go'), 'utf8');
  assert.ok(embed.includes('composer_persist.js'), 'embed.go must embed composer_persist.js');
  const mk = fs.readFileSync(path.join(__dirname, '..', '..', 'Makefile'), 'utf8');
  assert.ok(mk.includes('composer_persist_test.js'), 'Makefile test-web must run composer_persist_test');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('PASS composer_persist_test');
