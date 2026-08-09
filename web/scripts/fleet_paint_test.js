// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic thrash oracle for fleet-tree paint (🎯T289).
//
//   node web/scripts/fleet_paint_test.js
//
// These tests are written to FAIL against the pre-T289 hot path (one full
// teardown+rebuild per agents_changed push) and pass only when bursts
// coalesce and unchanged rows are left alone.

'use strict';

const assert = require('assert');
const FP = require('./fleet_paint.js');

function test(name, fn) {
  try {
    fn();
    console.log('  ok -', name);
  } catch (e) {
    console.error('  FAIL -', name);
    console.error('   ', e && e.stack ? e.stack.split('\n').slice(0, 6).join('\n    ') : e);
    process.exitCode = 1;
  }
}

console.log('fleet_paint_test (🎯T289 stutter + CPU)');

// Fake timer harness so coalescing is tested deterministically.
function fakeClock() {
  let t = 0;
  let seq = 1;
  const timers = new Map();
  return {
    now: function () { return t; },
    setTimer: function (fn, ms) {
      const id = seq++;
      timers.set(id, { at: t + ms, fn: fn });
      return id;
    },
    clearTimer: function (id) { timers.delete(id); },
    advance: function (ms) {
      const target = t + ms;
      for (;;) {
        let nextId = 0;
        let nextAt = Infinity;
        timers.forEach(function (v, k) {
          if (v.at <= target && v.at < nextAt) { nextAt = v.at; nextId = k; }
        });
        if (!nextId) break;
        const entry = timers.get(nextId);
        timers.delete(nextId);
        t = entry.at;
        entry.fn();
      }
      t = target;
    },
  };
}

function coalescer(clock, opts) {
  const runs = [];
  const c = FP.makeCoalescer(function () { runs.push(clock.now()); }, Object.assign({
    now: clock.now,
    setTimer: clock.setTimer,
    clearTimer: clock.clearTimer,
  }, opts || {}));
  c.runs = runs;
  return c;
}

// ── Coalescing ───────────────────────────────────────────────────────

test('burst of pushes collapses to a single repaint', function () {
  const clock = fakeClock();
  const c = coalescer(clock, { waitMs: 120 });
  // 20 agents_changed pushes inside one burst window — the pre-T289 path
  // repainted the whole tree 20 times.
  for (let i = 0; i < 20; i++) {
    c.schedule();
    clock.advance(5);
  }
  clock.advance(200);
  assert.strictEqual(c.runs.length, 1, 'expected exactly one coalesced repaint');
  assert.strictEqual(c.stats.scheduled, 20);
  assert.strictEqual(c.stats.ran, 1);
});

test('sustained stream still repaints — no starvation', function () {
  const clock = fakeClock();
  const c = coalescer(clock, { waitMs: 120, maxWaitMs: 600 });
  // A push every 50ms forever would reset a naive debounce indefinitely.
  for (let i = 0; i < 40; i++) {
    c.schedule();
    clock.advance(50);
  }
  clock.advance(200);
  assert.ok(c.runs.length >= 3, 'steady stream must still paint, got ' + c.runs.length);
  // ...but nowhere near one paint per push.
  assert.ok(c.runs.length < 10, 'steady stream over-painted: ' + c.runs.length);
});

test('separated pushes each paint (no lost update)', function () {
  const clock = fakeClock();
  const c = coalescer(clock, { waitMs: 120 });
  c.schedule();
  clock.advance(500);
  c.schedule();
  clock.advance(500);
  assert.strictEqual(c.runs.length, 2);
});

test('flush paints immediately and clears pending', function () {
  const clock = fakeClock();
  const c = coalescer(clock, { waitMs: 120 });
  c.schedule();
  assert.strictEqual(c.pending(), true);
  assert.strictEqual(c.flush(), true);
  assert.strictEqual(c.runs.length, 1);
  assert.strictEqual(c.pending(), false);
  assert.strictEqual(c.flush(), false, 'flush with nothing pending is a no-op');
});

test('cancel drops the pending paint', function () {
  const clock = fakeClock();
  const c = coalescer(clock, { waitMs: 120 });
  c.schedule();
  assert.strictEqual(c.cancel(), true);
  clock.advance(1000);
  assert.strictEqual(c.runs.length, 0);
});

// ── Diffing ──────────────────────────────────────────────────────────

function row(key, parentKey, html, extra) {
  return Object.assign({
    key: key,
    parentKey: parentKey,
    className: 'agent-node',
    html: html,
    dataset: { agent: key, parent: parentKey },
  }, extra || {});
}

function fleet(progressFor, progressText) {
  // Overseer → PO → three workers: the ordinary shape the owner sees.
  return [
    row('jevons', '', 'jevons'),
    row('jevons-po', 'jevons', 'jevons-po'),
    row('jv-a', 'jevons-po', 'jv-a ' + (progressFor === 'jv-a' ? progressText : 'idle')),
    row('jv-b', 'jevons-po', 'jv-b ' + (progressFor === 'jv-b' ? progressText : 'idle')),
    row('jv-c', 'jevons-po', 'jv-c ' + (progressFor === 'jv-c' ? progressText : 'idle')),
  ];
}

test('first paint rebuilds', function () {
  const plan = FP.diffPlan(null, fleet());
  assert.strictEqual(plan.mode, 'rebuild');
});

test('identical snapshot is a noop — zero DOM writes', function () {
  const plan = FP.diffPlan(fleet(), fleet());
  assert.strictEqual(plan.mode, 'noop');
  assert.strictEqual(plan.changed, 0);
  assert.strictEqual(plan.patches.length, 0);
});

test('one working agent patches exactly one row (not the whole tree)', function () {
  // The dominant case: agents_changed because ONE agent's progress line moved.
  const plan = FP.diffPlan(fleet('jv-b', 'idle'), fleet('jv-b', 'running tests'));
  assert.strictEqual(plan.mode, 'patch');
  assert.strictEqual(plan.changed, 1, 'only the changed row may repaint');
  assert.strictEqual(plan.total, 5);
  assert.strictEqual(plan.patches[0].index, 3);
  assert.strictEqual(plan.patches[0].descriptor.key, 'jv-b');
});

test('class-only change (selection move) patches, does not rebuild', function () {
  const a = fleet();
  const b = fleet();
  b[3] = Object.assign({}, b[3], { className: 'agent-node selected' });
  const plan = FP.diffPlan(a, b);
  assert.strictEqual(plan.mode, 'patch');
  assert.strictEqual(plan.changed, 1);
});

test('dataset-only change is detected', function () {
  const a = fleet();
  const b = fleet();
  b[2] = Object.assign({}, b[2], { dataset: { agent: 'jv-a', parent: 'jevons-po', secondary: 'progress' } });
  const plan = FP.diffPlan(a, b);
  assert.strictEqual(plan.mode, 'patch');
  assert.strictEqual(plan.changed, 1);
});

test('spawn changes shape → rebuild', function () {
  const next = fleet().concat([row('jv-d', 'jevons-po', 'jv-d idle')]);
  assert.strictEqual(FP.diffPlan(fleet(), next).mode, 'rebuild');
});

test('reap changes shape → rebuild', function () {
  const next = fleet().filter(function (r) { return r.key !== 'jv-c'; });
  assert.strictEqual(FP.diffPlan(fleet(), next).mode, 'rebuild');
});

test('reparent changes shape → rebuild', function () {
  const next = fleet();
  next[4] = row('jv-c', 'jv-a', 'jv-c idle');
  assert.strictEqual(FP.diffPlan(fleet(), next).mode, 'rebuild');
});

test('sibling reorder changes shape → rebuild', function () {
  const next = fleet();
  const tmp = next[2];
  next[2] = next[3];
  next[3] = tmp;
  assert.strictEqual(FP.diffPlan(fleet(), next).mode, 'rebuild');
});

test('key/parent separator cannot be forged by row content', function () {
  // Guards against a name containing the separator colliding two shapes.
  const a = [row('a', 'b|c', 'x')];
  const b = [row('a|b', 'c', 'x')];
  assert.notStrictEqual(FP.structureKey(a), FP.structureKey(b));
});

// ── Thrash budget (the actual oracle) ────────────────────────────────

test('busy fleet burst: bounded paints and bounded row writes', function () {
  const clock = fakeClock();
  let paints = 0;
  let rowWrites = 0;
  let prev = null;
  let live = fleet();

  const c = FP.makeCoalescer(function () {
    paints++;
    const plan = FP.diffPlan(prev, live);
    if (plan.mode === 'rebuild') rowWrites += plan.total;
    else rowWrites += plan.changed;
    prev = live;
  }, { now: clock.now, setTimer: clock.setTimer, clearTimer: clock.clearTimer, waitMs: 120, maxWaitMs: 600 });

  // 3 agents each emitting a progress change every ~40ms for 4 seconds —
  // an ordinary busy fleet, not a storm. 300 pushes.
  const names = ['jv-a', 'jv-b', 'jv-c'];
  for (let i = 0; i < 300; i++) {
    live = fleet(names[i % 3], 'step ' + i);
    c.schedule();
    clock.advance(13);
  }
  clock.advance(500);

  // Pre-T289: 300 paints x 5 rows = 1500 row writes plus 300 full teardowns.
  assert.strictEqual(c.stats.scheduled, 300);
  assert.ok(paints <= 12, 'paint budget blown: ' + paints + ' paints for 300 pushes');
  assert.ok(rowWrites <= 40, 'row-write budget blown: ' + rowWrites);
  // The first paint is the only rebuild; the rest patch a single row.
  assert.ok(rowWrites < 300, 'row writes must be far below push count');
  console.log('    300 pushes → ' + paints + ' paints, ' + rowWrites + ' row writes');
});

// ── Poll pacing ──────────────────────────────────────────────────────

test('visible tab polls at base rate', function () {
  const p = FP.pollPlan(3000, { hidden: false });
  assert.strictEqual(p.run, true);
  assert.strictEqual(p.delayMs, 3000);
});

test('hidden tab skips work and backs off', function () {
  const p = FP.pollPlan(3000, { hidden: true });
  assert.strictEqual(p.run, false);
  assert.ok(p.delayMs >= 60000, 'hidden poll must back off, got ' + p.delayMs);
});

test('hidden never polls faster than visible', function () {
  [1000, 3000, 10000, 30000, 120000].forEach(function (base) {
    const vis = FP.pollPlan(base, { hidden: false });
    const hid = FP.pollPlan(base, { hidden: true });
    assert.ok(hid.delayMs >= vis.delayMs, 'base ' + base);
  });
});

test('cost ticker in a background tab does ~zero work over an hour', function () {
  // 3s poll x 3600s = 1200 fetches pre-T289, every one of them pure burn.
  let fetches = 0;
  let t = 0;
  while (t < 3600000) {
    const p = FP.pollPlan(3000, { hidden: true });
    if (p.run) fetches++;
    t += p.delayMs;
  }
  assert.strictEqual(fetches, 0, 'hidden tab must not fetch at all');
});
