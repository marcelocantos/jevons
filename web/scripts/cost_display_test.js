// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const CD = require('./cost_display.js');

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

test('disabled=true → ticker hidden', function () {
  const r = CD.formatCostTicker({
    disabled: true,
    accounting: 'disabled',
    billable: false,
    global_usd_per_hour: 50
  });
  assert.strictEqual(r.visible, false);
});

test('unset / error → ticker hidden', function () {
  assert.strictEqual(CD.formatCostTicker(null).visible, false);
  assert.strictEqual(CD.formatCostTicker({ error: 'nope' }).visible, false);
  assert.strictEqual(CD.formatCostTicker({ accounting: 'disabled' }).visible, false);
});

test('subscription → visible estimate, not billed, no kill chrome', function () {
  const r = CD.formatCostTicker({
    accounting: 'subscription',
    billable: false,
    currency_note: 'API-equivalent USD estimate — not billed',
    global_usd_per_hour: 50.5,
    fleet_usd_per_hour: 12,
    alerts: [{ level: 'warn', kind: 'fleet-rate', detail: 'est high' }]
  });
  assert.strictEqual(r.visible, true);
  assert.ok(r.text.indexOf('est $') === 0 || r.text.indexOf('est $') >= 0);
  assert.ok(r.text.indexOf('not billed') >= 0);
  assert.ok(r.text.indexOf('50.50') >= 0);
  assert.strictEqual(r.className, 'cost-warn');
  assert.ok(r.className !== 'cost-kill');
});

test('list_price → billable fire emoji $ and kill class on kill alert', function () {
  const r = CD.formatCostTicker({
    accounting: 'list_price',
    billable: true,
    global_usd_per_hour: 10,
    fleet_usd_per_hour: 5,
    alerts: [{ level: 'kill', kind: 'fleet-rate', detail: 'hot' }]
  });
  assert.strictEqual(r.visible, true);
  assert.ok(r.text.indexOf('🔥') >= 0);
  assert.ok(r.text.indexOf('not billed') < 0);
  assert.strictEqual(r.className, 'cost-kill');
});

test('index.html loads cost_display and uses formatCostTicker', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('cost_display.js') >= 0, 'must script-src cost_display.js');
  assert.ok(html.indexOf('formatCostTicker') >= 0, 'must call formatCostTicker');
});
