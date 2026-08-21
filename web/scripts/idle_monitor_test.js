// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('assert');
const IM = require('./idle_monitor.js');
const fs = require('fs');
const path = require('path');

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

test('windowedRate is delta over wall, not lifetime', function () {
  assert.strictEqual(IM.windowedRate(236, 2000), 118);
  assert.strictEqual(IM.windowedRate(0, 2000), 0);
  assert.strictEqual(IM.windowedRate(10, 0), 0);
});

test('classifyIdleStorm ignores hydrate/stream and flags T532-rate snaps', function () {
  assert.strictEqual(IM.classifyIdleStorm({ snapsPerS: 118, streaming: false, replaying: false }).warn, true);
  assert.strictEqual(IM.classifyIdleStorm({ snapsPerS: 118, streaming: false, replaying: false }).reason, 'snap');
  assert.strictEqual(IM.classifyIdleStorm({ snapsPerS: 118, streaming: true }).warn, false);
  assert.strictEqual(IM.classifyIdleStorm({ snapsPerS: 118, replaying: true }).warn, false);
  assert.strictEqual(IM.classifyIdleStorm({ snapsPerS: 0.5, streaming: false }).warn, false);
  assert.strictEqual(IM.classifyIdleStorm({ snapsPerS: 0, longtasks: 5, streaming: false }).warn, true);
  assert.strictEqual(IM.classifyIdleStorm({ snapsPerS: 0, rafPerS: 118, streaming: false }).warn, true);
  assert.strictEqual(IM.classifyIdleStorm({ snapsPerS: 0, rafPerS: 118, streaming: false }).reason, 'raf');
});

test('stormHysteresis needs two consecutive windows', function () {
  const a = IM.stormHysteresis(0, { warn: true, reason: 'snap' });
  assert.strictEqual(a.warn, false);
  assert.strictEqual(a.streak, 1);
  const b = IM.stormHysteresis(a.streak, { warn: true, reason: 'snap' });
  assert.strictEqual(b.warn, true);
  const c = IM.stormHysteresis(b.streak, { warn: false });
  assert.strictEqual(c.warn, false);
  assert.strictEqual(c.streak, 0);
});

test('bannerText names the snap loop', function () {
  const t = IM.bannerText({ warn: true, reason: 'snap', snapsPerS: 118 });
  assert.ok(/snap-looping/.test(t));
  assert.ok(/118/.test(t));
  assert.strictEqual(IM.bannerText({ warn: false }), '');
});

test('tickWindow reports the T532 118/s burst', function () {
  const w = IM.tickWindow({ count: 0, ms: 0 }, 236, 2000);
  assert.strictEqual(w.rate, 118);
  assert.strictEqual(w.next.count, 236);
});

test('index.html wires IdleMonitor and the storm banner', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(/scripts\/idle_monitor\.js/.test(html), 'idle_monitor.js loaded');
  assert.ok(/id="idle-storm-banner"/.test(html), 'banner node');
  assert.ok(/IdleMonitor\.classifyIdleStorm/.test(html), 'classifier used');
  assert.ok(/PerformanceObserver/.test(html) && /longtask/.test(html),
    'longtask observer wired');
});

if (process.exitCode) {
  console.error('FAIL idle_monitor_test');
} else {
  console.log('PASS idle_monitor_test');
}
