// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for owner-chat send queue (🎯T113).
// Run: node web/scripts/send_queue_test.js
// NOT Playwright — pure-state policy + index.html wiring greps.

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const SQ = require('./send_queue.js');

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

// ── decideSend / interrupt policy ────────────────────────────────

test('plain send while busy enqueues (no interrupt)', function () {
  const d = SQ.decideSend({ busy: true, interrupt: false, text: 'follow up' });
  assert.strictEqual(d.action, 'enqueue');
  assert.strictEqual(d.text, 'follow up');
  assert.strictEqual(SQ.shouldEnqueue(true, false), true);
  assert.strictEqual(SQ.shouldInterrupt(true, false), false);
});

test('Control+Enter while busy interrupts and sends', function () {
  const d = SQ.decideSend({ busy: true, interrupt: true, text: 'interject now' });
  assert.strictEqual(d.action, 'send');
  assert.strictEqual(d.interrupt, true);
  assert.strictEqual(SQ.shouldInterrupt(true, true), true);
  assert.strictEqual(SQ.shouldEnqueue(true, true), false);
});

test('idle send submits without interrupt', function () {
  const d = SQ.decideSend({ busy: false, interrupt: false, text: 'hello' });
  assert.strictEqual(d.action, 'send');
  assert.strictEqual(d.interrupt, false);
});

test('empty draft is noop even when busy', function () {
  assert.strictEqual(SQ.decideSend({ busy: true, interrupt: false, text: '  ' }).action, 'noop');
  assert.strictEqual(SQ.decideSend({ busy: true, interrupt: true, text: '' }).action, 'noop');
});

test('images-only payload can enqueue while busy', function () {
  const d = SQ.decideSend({ busy: true, interrupt: false, text: '', hasImages: true });
  assert.strictEqual(d.action, 'enqueue');
});

// ── queue mutate / drain ─────────────────────────────────────────

test('enqueue then shiftNext drains FIFO', function () {
  let s = SQ.emptyState();
  s = SQ.enqueue(s, 'first');
  s = SQ.enqueue(s, 'second');
  assert.strictEqual(s.items.length, 2);
  let r = SQ.shiftNext(s);
  assert.strictEqual(r.item.text, 'first');
  r = SQ.shiftNext(r.state);
  assert.strictEqual(r.item.text, 'second');
  r = SQ.shiftNext(r.state);
  assert.strictEqual(r.item, null);
  assert.strictEqual(r.state.items.length, 0);
});

test('cancel removes by id; takeById for send-now', function () {
  let s = SQ.emptyState();
  s = SQ.enqueue(s, 'a');
  s = SQ.enqueue(s, 'b');
  const idA = s.items[0].id;
  const idB = s.items[1].id;
  s = SQ.cancel(s, idA);
  assert.strictEqual(s.items.length, 1);
  assert.strictEqual(s.items[0].id, idB);
  const t = SQ.takeById(s, idB);
  assert.strictEqual(t.item.text, 'b');
  assert.strictEqual(t.state.items.length, 0);
});

test('updateItem edits queued text', function () {
  let s = SQ.enqueue(SQ.emptyState(), 'draft');
  const id = s.items[0].id;
  s = SQ.updateItem(s, id, 'edited');
  assert.strictEqual(s.items[0].text, 'edited');
});

// ── Alt+Up order: queue before history ───────────────────────────

test('Alt+Up from live hits newest queue before history', function () {
  const r1 = SQ.cycleNav({ zone: 'live' }, -1, 2, 3);
  assert.ok(r1.handled);
  assert.deepStrictEqual(r1.focus, { zone: 'queue', index: 1 });

  const r2 = SQ.cycleNav(r1.focus, -1, 2, 3);
  assert.deepStrictEqual(r2.focus, { zone: 'queue', index: 0 });

  const r3 = SQ.cycleNav(r2.focus, -1, 2, 3);
  assert.deepStrictEqual(r3.focus, { zone: 'history', index: 2 });

  const r4 = SQ.cycleNav(r3.focus, -1, 2, 3);
  assert.deepStrictEqual(r4.focus, { zone: 'history', index: 1 });
});

test('Alt+Down reverses queue-then-history into live', function () {
  let f = { zone: 'history', index: 0 };
  f = SQ.cycleNav(f, +1, 2, 2).focus; // history 1
  assert.deepStrictEqual(f, { zone: 'history', index: 1 });
  f = SQ.cycleNav(f, +1, 2, 2).focus; // oldest queue
  assert.deepStrictEqual(f, { zone: 'queue', index: 0 });
  f = SQ.cycleNav(f, +1, 2, 2).focus;
  assert.deepStrictEqual(f, { zone: 'queue', index: 1 });
  f = SQ.cycleNav(f, +1, 2, 2).focus;
  assert.deepStrictEqual(f, { zone: 'live' });
});

test('Alt+Up with empty queue falls into history (legacy path)', function () {
  const r = SQ.cycleNav({ zone: 'live' }, -1, 0, 2);
  assert.ok(r.handled);
  assert.deepStrictEqual(r.focus, { zone: 'history', index: 1 });
});

test('Alt+Up with empty queue and empty history is unhandled', function () {
  const r = SQ.cycleNav({ zone: 'live' }, -1, 0, 0);
  assert.strictEqual(r.handled, false);
  assert.strictEqual(r.focus.zone, 'live');
});

test('textForFocus resolves queue then history', function () {
  const items = [{ id: 'q1', text: 'queued A' }, { id: 'q2', text: 'queued B' }];
  const hist = ['old', 'newer'];
  assert.strictEqual(
    SQ.textForFocus({ zone: 'queue', index: 1 }, items, hist, 'live'),
    'queued B'
  );
  assert.strictEqual(
    SQ.textForFocus({ zone: 'history', index: 0 }, items, hist, 'live'),
    'old'
  );
  assert.strictEqual(
    SQ.textForFocus({ zone: 'live' }, items, hist, 'draft'),
    'draft'
  );
});

// ── index.html wiring ────────────────────────────────────────────

test('index.html loads SendQueue and does not interrupt on plain busy send', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/send_queue.js'), 'must load send_queue.js');
  assert.ok(html.includes('SendQueue'), 'must reference SendQueue');
  assert.ok(html.includes('id="send-queue"') || html.includes("id='send-queue'"),
    'queue strip #send-queue must be present');
  // Root-cause fix: the old unconditional interrupt on send while busy is gone.
  assert.ok(
    !/if \(workingEl\) transport\.send\('\{"type":"interrupt"\}'\);/.test(html),
    'must not unconditionally interrupt when workingEl is set in send()'
  );
  assert.ok(
    html.includes('Ctrl+Enter') || html.includes('Control+Enter'),
    'placeholder or UI must document Ctrl+Enter interject'
  );
  assert.ok(html.includes('decideSend') || html.includes('shouldEnqueue'),
    'send path must consult queue policy');
});

test('Grok mismatches are documented on the module', function () {
  assert.ok(Array.isArray(SQ.GROK_MISMATCHES));
  assert.ok(SQ.GROK_MISMATCHES.length >= 1);
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('PASS send_queue_test');
