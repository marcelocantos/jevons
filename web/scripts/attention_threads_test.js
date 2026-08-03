// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic tests for 🎯T65 attention-thread model (prefix-first).
// Run: node web/scripts/attention_threads_test.js

'use strict';

const assert = require('assert');
const AT = require('./attention_threads.js');

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

test('parsePrefix is case-insensitive and strips prefix', function () {
  const a = AT.parsePrefix('aside: hello world');
  assert.strictEqual(a.command, 'aside');
  assert.strictEqual(a.body, 'hello world');
  const b = AT.parsePrefix('  CAPTURE : side thought');
  assert.strictEqual(b.command, 'capture');
  assert.strictEqual(b.body, 'side thought');
  const c = AT.parsePrefix('no prefix here');
  assert.strictEqual(c.command, null);
  assert.strictEqual(c.body, 'no prefix here');
  const d = AT.parsePrefix('target: virtualise history');
  assert.strictEqual(d.command, 'target');
  assert.strictEqual(d.body, 'virtualise history');
});

test('target: opens file-target aside, wire, main focus (T93/T95)', function () {
  const r = AT.handleComposer(AT.emptyState(), 'target: Chat paste images work');
  assert.strictEqual(r.kind, 'send');
  assert.strictEqual(r.purpose, 'file-target');
  assert.ok(r.text.indexOf('[target-aside:') === 0);
  assert.ok(r.text.indexOf('jevons_target_file') > 0);
  assert.strictEqual(r.state.focusId, AT.MAIN_ID);
  assert.strictEqual(r.state.threads[0].purpose, 'file-target');
});

test('detectTargetFiled + closeTargetAside dismisses filing aside (done, not parked)', function () {
  const open = AT.handleComposer(AT.emptyState(), 'target: Foo bar');
  const id = open.threadId;
  assert.ok(id);
  const filed = AT.detectTargetFiled('Filed 🎯T120 — Foo\n__TARGET_FILED__:T120\n');
  assert.strictEqual(filed, 'T120');
  const closed = AT.closeTargetAside(open.state, id);
  assert.strictEqual(closed.threads[0].status, 'done');
  assert.strictEqual(closed.focusId, AT.MAIN_ID);
  // Default chrome excludes completed filing asides (🎯T95.1).
  assert.strictEqual(AT.stack(closed).length, 0);
  // Archive still lists them for discoverability.
  const arch = AT.archive(closed);
  assert.strictEqual(arch.length, 1);
  assert.strictEqual(arch[0].id, id);
  assert.strictEqual(arch[0].status, 'done');
});

test('closeTargetAside without id closes most recent open file-target', function () {
  let s = AT.handleComposer(AT.emptyState(), 'target: First filing').state;
  s = AT.handleComposer(s, 'target: Second filing').state;
  const closed = AT.closeTargetAside(s); // most recent open file-target
  const second = closed.threads.find(function (t) { return t.body === 'Second filing'; });
  const first = closed.threads.find(function (t) { return t.body === 'First filing'; });
  assert.strictEqual(second.status, 'done');
  assert.strictEqual(first.status, 'open');
  assert.strictEqual(AT.stack(closed).length, 1);
  assert.strictEqual(AT.archive(closed).length, 1);
});

test('general dismiss: done leaves chrome, stays in archive', function () {
  const cap = AT.handleComposer(AT.emptyState(), 'aside: billing nit');
  const id = cap.threadId;
  const done = AT.dismiss(cap.state, id);
  assert.strictEqual(done.threads[0].status, 'done');
  assert.strictEqual(done.focusId, AT.MAIN_ID);
  assert.strictEqual(AT.stack(done).length, 0);
  assert.strictEqual(AT.archive(done).length, 1);
  assert.strictEqual(AT.archivedStack(done)[0].id, id);
});

test('manual park still appears in default stack (not done)', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: Workstream').state;
  const id = s.threads[0].id;
  s = AT.park(s, id);
  assert.strictEqual(s.threads[0].status, 'parked');
  const st = AT.stack(s);
  assert.strictEqual(st.length, 1);
  assert.strictEqual(st[0].status, 'parked');
  assert.strictEqual(AT.archive(s).length, 0);
});

test('serialize/deserialize preserves done status', function () {
  let s = AT.handleComposer(AT.emptyState(), 'target: Persist done').state;
  s = AT.closeTargetAside(s, s.threads[0].id);
  const again = AT.deserialize(AT.serialize(s));
  assert.strictEqual(again.threads[0].status, 'done');
  assert.strictEqual(AT.stack(again).length, 0);
  assert.strictEqual(AT.archive(again).length, 1);
  // Legacy "archived" alias normalizes to done.
  const legacy = AT.deserialize(JSON.stringify({
    focusId: 'main',
    threads: [{ id: 'att-x', title: 'X', body: 'X', status: 'archived', purpose: '', createdAt: 1, updatedAt: 1 }],
  }));
  assert.strictEqual(legacy.threads[0].status, 'done');
});

test('load migrates legacy parked file-target → done (not manual park)', function () {
  // Pre-T95.1 localStorage: auto-close parked the filing aside.
  const raw = JSON.stringify({
    focusId: AT.MAIN_ID,
    threads: [
      {
        id: 'att-file-old',
        title: 'T113 filing',
        body: 'target: something',
        status: 'parked',
        purpose: 'file-target',
        createdAt: 1,
        updatedAt: 2,
      },
      {
        id: 'att-work',
        title: 'real workstream',
        body: 'still parked on purpose',
        status: 'parked',
        purpose: '',
        createdAt: 3,
        updatedAt: 4,
      },
    ],
  });
  const store = {};
  store[AT.STORAGE_KEY || 'jevons-attention-threads-v1'] = raw;
  const storage = {
    getItem: function (k) { return Object.prototype.hasOwnProperty.call(store, k) ? store[k] : null; },
    setItem: function (k, v) { store[k] = String(v); },
  };
  const loaded = AT.load(storage);
  const filing = loaded.threads.find(function (t) { return t.id === 'att-file-old'; });
  const work = loaded.threads.find(function (t) { return t.id === 'att-work'; });
  assert.ok(filing);
  assert.ok(work);
  assert.strictEqual(filing.status, 'done');
  assert.strictEqual(filing.purpose, 'file-target');
  assert.strictEqual(work.status, 'parked');
  // Default chrome: only intentional park remains.
  const st = AT.stack(loaded);
  assert.strictEqual(st.length, 1);
  assert.strictEqual(st[0].id, 'att-work');
  assert.strictEqual(AT.archive(loaded).length, 1);
  assert.strictEqual(AT.archive(loaded)[0].id, 'att-file-old');
  // deserialize alone (same migration path)
  const d = AT.deserialize(raw);
  assert.strictEqual(d.threads.find(function (t) { return t.id === 'att-file-old'; }).status, 'done');
});

test('aside: sends routed wire text and keeps main focus', function () {
  const r = AT.handleComposer(AT.emptyState(), 'aside: billing nit');
  assert.strictEqual(r.kind, 'send');
  assert.ok(r.text.indexOf('[attention:') === 0);
  assert.ok(r.text.indexOf('billing nit') > 0);
  assert.ok(r.text.indexOf('aside:') === -1);
  assert.strictEqual(r.state.focusId, AT.MAIN_ID);
  assert.strictEqual(r.state.threads.length, 1);
  assert.strictEqual(r.state.threads[0].body, 'billing nit');
});

test('capture: is local — no send, main focus, thread tracked', function () {
  const r = AT.handleComposer(AT.emptyState(), 'capture: later idea');
  assert.strictEqual(r.kind, 'local');
  assert.strictEqual(r.text, '');
  assert.strictEqual(r.state.focusId, AT.MAIN_ID);
  assert.strictEqual(r.state.threads.length, 1);
  assert.strictEqual(r.state.threads[0].body, 'later idea');
  assert.strictEqual(r.clearComposer, true);
});

test('park: and pursue: keep both threads tracked', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: First').state;
  s = AT.handleComposer(s, 'capture: Second').state;
  assert.strictEqual(s.threads.length, 2);
  const firstId = s.threads.find(function (t) { return t.body === 'First'; }).id;
  s = AT.pursue(s, firstId);
  assert.strictEqual(s.focusId, firstId);
  s = AT.handleComposer(s, 'park:').state;
  assert.strictEqual(s.focusId, AT.MAIN_ID);
  const first = s.threads.find(function (t) { return t.id === firstId; });
  assert.strictEqual(first.status, 'parked');
  s = AT.handleComposer(s, 'pursue: First').state;
  assert.strictEqual(s.focusId, firstId);
  assert.strictEqual(AT.findThread(s, firstId).status, 'open');
  assert.strictEqual(s.threads.length, 2);
});

test('main: returns focus; main: body sends on main', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: Side').state;
  const id = s.threads[0].id;
  s = AT.pursue(s, id);
  const local = AT.handleComposer(s, 'main:');
  assert.strictEqual(local.kind, 'local');
  assert.strictEqual(local.state.focusId, AT.MAIN_ID);
  s = AT.pursue(local.state, id);
  const send = AT.handleComposer(s, 'main: back to work');
  assert.strictEqual(send.kind, 'send');
  assert.strictEqual(send.text, 'back to work');
  assert.strictEqual(send.state.focusId, AT.MAIN_ID);
  assert.ok(!send.routed);
});

test('pursued focus without prefix routes as aside wire', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: Draft').state;
  const id = s.threads[0].id;
  s = AT.pursue(s, id);
  const r = AT.handleComposer(s, 'Pursued body');
  assert.strictEqual(r.kind, 'send');
  assert.ok(r.routed);
  assert.ok(r.text.indexOf('[attention:' + id + '|') === 0);
  assert.ok(r.text.endsWith('Pursued body'));
});

test('plain main send is unchanged', function () {
  const r = AT.handleComposer(AT.emptyState(), 'Hello main');
  assert.strictEqual(r.kind, 'send');
  assert.strictEqual(r.text, 'Hello main');
  assert.ok(!r.routed);
  assert.strictEqual(r.state.threads.length, 0);
});

test('serialize round-trip drops legacy asideNext', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: Persist me').state;
  const again = AT.deserialize(AT.serialize(s));
  assert.strictEqual(again.threads.length, 1);
  assert.strictEqual(again.threads[0].body, 'Persist me');
  assert.strictEqual(again.asideNext, undefined);
});

test('load/save via mock storage', function () {
  const store = {};
  const storage = {
    getItem: function (k) { return Object.prototype.hasOwnProperty.call(store, k) ? store[k] : null; },
    setItem: function (k, v) { store[k] = String(v); },
  };
  let s = AT.handleComposer(AT.emptyState(), 'capture: Stored').state;
  AT.save(storage, s);
  const loaded = AT.load(storage);
  assert.strictEqual(loaded.threads[0].body, 'Stored');
});

test('composerPlaceholder: main is clean; side uses [aside: title] hint', function () {
  const mainPh = 'Write a message to Jevons. Enter to send, Shift-Enter for a new line.';
  assert.strictEqual(AT.composerPlaceholder(AT.emptyState(), mainPh), mainPh);
  assert.ok(AT.composerPlaceholder(AT.emptyState(), mainPh).indexOf('[main') === -1);

  let s = AT.handleComposer(AT.emptyState(), 'capture: billing nit later').state;
  const id = s.threads[0].id;
  s = AT.pursue(s, id);
  const ph = AT.composerPlaceholder(s, mainPh);
  assert.ok(ph.indexOf('[aside: ') === 0);
  assert.ok(ph.indexOf('] Write a message to Jevons') > 0);
  assert.ok(ph.indexOf('billing') > 0);
  assert.ok(ph.indexOf('[main') === -1);
});

// ── 🎯T134 chrome hygiene + routing honesty ──────────────────────────

test('T134 routeCandidates: open only — never done or parked', function () {
  let s = AT.emptyState();
  s = AT.handleComposer(s, 'capture: restic backup work').state;
  const openId = s.threads[0].id;
  s = AT.handleComposer(s, 'capture: parked later').state;
  const parkId = s.threads[0].id;
  s = AT.park(s, parkId);
  s = AT.handleComposer(s, 'aside: done ghost title').state;
  const doneId = s.threads[0].id;
  s = AT.dismiss(s, doneId);

  const cands = AT.routeCandidates(s);
  assert.strictEqual(cands.length, 1);
  assert.strictEqual(cands[0].id, openId);
  assert.strictEqual(cands[0].status, 'open');
  assert.ok(cands.every(function (t) { return t.status === 'open'; }));
  // Done/parked never appear even if titles would match auto-route.
  assert.ok(!cands.find(function (t) { return t.id === doneId; }));
  assert.ok(!cands.find(function (t) { return t.id === parkId; }));
});

test('T134 stack excludes done; filing close still leaves bar clean', function () {
  let s = AT.handleComposer(AT.emptyState(), 'target: File me please').state;
  assert.strictEqual(AT.stack(s).length, 1);
  s = AT.closeTargetAside(s);
  assert.strictEqual(AT.stack(s).length, 0);
  assert.strictEqual(AT.routeCandidates(s).length, 0);
  assert.strictEqual(AT.archive(s).length, 1);
});

test('T134 model stack still accumulates; T136 chrome/visible empty', function () {
  assert.ok(AT.MAX_VISIBLE_CHIPS >= 1);
  let s = AT.emptyState();
  for (let i = 0; i < AT.MAX_VISIBLE_CHIPS + 3; i++) {
    s = AT.handleComposer(s, 'capture: Workstream item ' + i + ' unique').state;
  }
  const full = AT.stack(s);
  assert.strictEqual(full.length, AT.MAX_VISIBLE_CHIPS + 3);
  // 🎯T136: top chrome never shows aside chips (RHS fleet tree owns them).
  assert.strictEqual(AT.chromeStack(s).length, 0);
  const vs = AT.visibleStack(s);
  assert.strictEqual(vs.shown.length, 0);
  assert.strictEqual(vs.overflowCount, 0);
});

// ── 🎯T136 asides live only in RHS fleet tree ─────────────────────────

test('T136 chromeStack always empty even with open and parked asides', function () {
  let s = AT.handleComposer(AT.emptyState(), 'aside: billing nit').state;
  s = AT.handleComposer(s, 'capture: parked later').state;
  const parkId = s.threads[0].id;
  s = AT.park(s, parkId);
  assert.ok(AT.stack(s).length >= 2, 'model stack still tracks asides');
  assert.strictEqual(AT.chromeStack(s).length, 0);
  assert.strictEqual(AT.visibleStack(s).shown.length, 0);
  // Route candidates still open-only for T99/T135 (no silent steal elsewhere).
  assert.ok(AT.routeCandidates(s).length >= 1);
});

test('T136 index.html: attention-bar not used for aside wall; fleet register path', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('ensureFleetAside') >= 0, 'create-aside dual-writes fleet');
  assert.ok(html.indexOf('/api/asides') >= 0, 'POST /api/asides');
  // renderAttention must not show chip wall for asides.
  assert.ok(/function renderAttention\(\)\s*\{[\s\S]{0,600}?classList\.remove\(['"]visible['"]\)/.test(html) ||
    html.indexOf("attentionBar.classList.remove('visible')") >= 0,
    'renderAttention never shows attention-bar');
  assert.ok(html.indexOf('aria-label="Attention asides"') >= 0 ||
    html.indexOf("aria-label='Attention asides'") >= 0,
    'owner-visible vocabulary: asides not threads');
  // Must not leave a chip loop as the default chrome path for open stack.
  assert.ok(html.indexOf('appendThreadChip') === -1,
    'no appendThreadChip wall in index');
});

test('T134 clearDone / dismissAllParked / clearChromeNoise', function () {
  let s = AT.emptyState();
  s = AT.handleComposer(s, 'capture: Keep open').state;
  const openId = s.threads[0].id;
  s = AT.handleComposer(s, 'capture: Park me').state;
  const parkId = s.threads[0].id;
  s = AT.park(s, parkId);
  s = AT.handleComposer(s, 'capture: Done me').state;
  const doneId = s.threads[0].id;
  s = AT.dismiss(s, doneId);

  assert.strictEqual(AT.stack(s).length, 2); // open + parked
  assert.strictEqual(AT.archive(s).length, 1);

  // clearDone purges archive only.
  let s2 = AT.clearDone(s);
  assert.strictEqual(AT.archive(s2).length, 0);
  assert.ok(AT.findThread(s2, openId));
  assert.ok(AT.findThread(s2, parkId));
  assert.ok(!AT.findThread(s2, doneId));

  // dismissAllParked leaves open.
  s2 = AT.dismissAllParked(s2);
  assert.strictEqual(AT.findThread(s2, parkId).status, 'done');
  assert.strictEqual(AT.findThread(s2, openId).status, 'open');
  assert.strictEqual(AT.stack(s2).length, 1);

  // clearChromeNoise: full bar reset (open+parked dismissed + archive purged).
  s = AT.clearChromeNoise(s);
  assert.strictEqual(AT.stack(s).length, 0);
  assert.strictEqual(AT.archive(s).length, 0);
  assert.strictEqual(s.focusId, AT.MAIN_ID);
  assert.ok(!AT.findThread(s, openId));
  assert.ok(!AT.findThread(s, parkId));
  assert.ok(!AT.findThread(s, doneId));
});

test('T134 capture dedupes same fingerprint open thread', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: restic backup status').state;
  assert.strictEqual(s.threads.length, 1);
  const id = s.threads[0].id;
  s = AT.handleComposer(s, 'capture: restic backup status').state;
  assert.strictEqual(s.threads.length, 1);
  assert.strictEqual(s.threads[0].id, id);
  // Near-dup first line (whitespace/case) merges.
  s = AT.handleComposer(s, 'capture:   RESTIC backup status  ').state;
  assert.strictEqual(s.threads.length, 1);
  assert.strictEqual(s.threads[0].id, id);
  // Different workstream stacks separately.
  s = AT.handleComposer(s, 'capture: billing nit later').state;
  assert.strictEqual(s.threads.length, 2);
  // Done ghost does not block a new open capture with same title.
  s = AT.dismiss(s, id);
  s = AT.handleComposer(s, 'capture: restic backup status').state;
  assert.strictEqual(AT.stack(s).filter(function (t) {
    return (t.body || '').toLowerCase().indexOf('restic') !== -1;
  }).length, 1);
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nall passed');
