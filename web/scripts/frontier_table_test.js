// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const FT = require('./frontier_table.js');

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

test('nextBottomTab and tabAfterAgentSelect', function () {
  assert.strictEqual(FT.nextBottomTab('frontier', 'transcript'), 'transcript');
  assert.strictEqual(FT.nextBottomTab('transcript', 'frontier'), 'frontier');
  assert.strictEqual(FT.nextBottomTab('frontier', 'nope'), 'frontier');
  assert.strictEqual(FT.tabAfterAgentSelect(true), 'transcript');
  assert.strictEqual(FT.tabAfterAgentSelect(false), 'frontier');
});

test('normalizePayload maps API rows; empty/error calm', function () {
  const m = FT.normalizePayload({
    available: true,
    ledger: '/resolved/by/bullseye/bullseye.yaml',
    cwd: '/proj',
    targets: [
      { id: 'T131', name: 'RHS frontier table', status: 'Converging', fanout: 0 },
      { id: 'T27.1', name: 'Provider contract', status: 'Converging', fanout: 3, value: 8 },
    ],
    updated_at: '2026-08-03T00:00:00Z',
  });
  assert.strictEqual(m.available, true);
  assert.strictEqual(m.rows.length, 2);
  assert.strictEqual(m.rows[0].id, 'T131');
  assert.strictEqual(m.rows[1].fanout, 3);
  assert.strictEqual(m.empty, false);
  assert.ok(m.ledger.indexOf('bullseye.yaml') >= 0);

  const empty = FT.normalizePayload({ available: true, targets: [] });
  assert.strictEqual(empty.empty, true);
  assert.ok(FT.emptyMessage(empty).indexOf('empty') >= 0);

  const unavail = FT.normalizePayload({
    available: false,
    targets: [],
    error: 'no bullseye ledger for this workdir',
  });
  assert.strictEqual(unavail.available, false);
  assert.ok(FT.emptyMessage(unavail).indexOf('ledger') >= 0);

  const err = FT.normalizePayload(null, new Error('network'));
  assert.ok(err.error.indexOf('network') >= 0);
  assert.strictEqual(err.empty, true);
});

test('formatStatus and shortName', function () {
  assert.strictEqual(FT.formatStatus('Converging'), 'Converging');
  assert.strictEqual(FT.formatStatus('identified'), 'Identified');
  assert.strictEqual(FT.formatStatus(''), '—');
  assert.ok(FT.shortName('x'.repeat(60), 20).length <= 20);
  assert.strictEqual(FT.shortName('short', 48), 'short');
});

test('client uses API path — does not hard-code ledger discovery path', function () {
  assert.strictEqual(FT.API_PATH, '/api/frontier');
  // Pure module must not invent in-repo path strings for discovery.
  const src = fs.readFileSync(path.join(__dirname, 'frontier_table.js'), 'utf8');
  assert.ok(src.indexOf('filepath.Join') === -1);
  // Must not embed a default absolute or home-shadow ledger path.
  assert.ok(!/\/Users\/.*\/bullseye\.yaml/.test(src));
  assert.ok(src.indexOf('.local/share/bullseye') === -1);
});

test('index.html wires frontier tab + API + no hard-coded ledger path', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/frontier_table.js') >= 0, 'script tag');
  assert.ok(html.indexOf('id="rhs-bottom"') >= 0 || html.indexOf("id='rhs-bottom'") >= 0 ||
    html.indexOf('id="frontier-pane"') >= 0, 'frontier pane host');
  assert.ok(html.indexOf('/api/frontier') >= 0, 'fetches frontier API');
  assert.ok(html.indexOf('frontier_changed') >= 0 || html.indexOf('loadFrontier') >= 0,
    'live refresh path');
  // Tabs coexist with T124 transcript inspect.
  assert.ok(html.indexOf('agent-inspect') >= 0, 'transcript pane still present');
  assert.ok(/Transcript/i.test(html) && /Frontier/i.test(html), 'tab labels');
  // Client must not hard-code discovery to a fixed repo-relative path string
  // as the ledger location (server discovers via bullseye).
  assert.ok(!/const\s+BULLSEYE_PATH\s*=\s*['"]bullseye\.yaml['"]/.test(html));
  assert.ok(!/ledger\s*=\s*['"][^'"]*bullseye\.yaml['"]/.test(html));
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('All frontier_table tests passed');
