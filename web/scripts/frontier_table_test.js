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

test('formatStatus abbr + statusTitle (🎯T173)', function () {
  assert.strictEqual(FT.formatStatus('Converging'), 'Cv');
  assert.strictEqual(FT.formatStatus('converging'), 'Cv');
  assert.strictEqual(FT.formatStatus('Identified'), 'Id');
  assert.strictEqual(FT.formatStatus('identified'), 'Id');
  assert.strictEqual(FT.formatStatus('set_aside'), 'Sa');
  assert.strictEqual(FT.formatStatus('SetAside'), 'Sa');
  assert.strictEqual(FT.formatStatus(''), '—');
  assert.strictEqual(FT.statusTitle('Converging'), 'Converging');
  assert.strictEqual(FT.statusTitle('identified'), 'Identified');
  assert.strictEqual(FT.statusTitle('set_aside'), 'Set aside');
  assert.strictEqual(FT.statusTitle(''), '');
  assert.ok(FT.shortName('x'.repeat(60), 20).length <= 20);
  assert.strictEqual(FT.shortName('short', 48), 'short');
});

test('formatFanout N᚛ + title; hide when 0 (🎯T173)', function () {
  assert.strictEqual(FT.FANOUT_MARK, '\u169B');
  const z = FT.formatFanout(0, 'T10.2');
  assert.strictEqual(z.visible, false);
  assert.strictEqual(z.text, '');
  assert.strictEqual(z.title, '');
  const one = FT.formatFanout(1, 'T10.2');
  assert.strictEqual(one.visible, true);
  assert.strictEqual(one.text, '1\u169B');
  assert.strictEqual(one.title, '1 target depends on T10.2');
  const many = FT.formatFanout(4, 'T10.2');
  assert.strictEqual(many.visible, true);
  assert.strictEqual(many.text, '4\u169B');
  assert.strictEqual(many.title, '4 targets depend on T10.2');
  // Real id passthrough
  assert.ok(FT.formatFanout(3, 'T173').title.indexOf('T173') >= 0);
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

test('T173 headerless table + abbr/fanout wiring in index.html', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  // No column headers: do not create thead or hard-code ID/NAME/STATUS/FAN labels.
  assert.ok(html.indexOf('createElement(\'thead\')') === -1, 'no thead element');
  assert.ok(html.indexOf('createElement("thead")') === -1, 'no thead element (dbl)');
  assert.ok(!/<th>\s*Id\s*<\/th>/i.test(html), 'no Id header cell');
  assert.ok(!/<th>\s*Name\s*<\/th>/i.test(html), 'no Name header cell');
  assert.ok(!/<th[^>]*>\s*Status\s*<\/th>/i.test(html), 'no Status header cell');
  assert.ok(!/>Fan<\/th>/i.test(html), 'no Fan header cell');
  // Status abbr + title via pure helpers
  assert.ok(html.indexOf('FrontierTable.formatStatus') >= 0, 'formatStatus used');
  assert.ok(html.indexOf('FrontierTable.statusTitle') >= 0, 'statusTitle used');
  // Fanout N᚛ path
  assert.ok(html.indexOf('FrontierTable.formatFanout') >= 0, 'formatFanout used');
  assert.ok(html.indexOf('ft-fanout-empty') >= 0, 'empty fanout class for hide-0');
});

test('T175 frontier cells use InstantTip not native title=', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/instant_tip.js') >= 0, 'instant_tip script');
  const start = html.indexOf('// 🎯T173: headerless table');
  assert.ok(start >= 0);
  const end = html.indexOf('function loadFrontier', start);
  const region = html.slice(start, end);
  assert.ok(region.indexOf('status.title') < 0, 'no status.title');
  assert.ok(region.indexOf('fan.title') < 0, 'no fan.title');
  assert.ok(region.indexOf('name.title') < 0, 'no name.title');
  assert.ok(region.indexOf('InstantTip.attach') >= 0, 'InstantTip.attach in render');
  assert.ok(/\.instant-tip\s*\{/.test(html), 'instant-tip CSS');
});

test('T174 frontier table width constrained to RHS container', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  // Table must not grow past #frontier-body: fixed layout + max-width 100%.
  assert.ok(/#frontier-table\s*\{[^}]*max-width:\s*100%/.test(html), 'table max-width 100%');
  assert.ok(/#frontier-table\s*\{[^}]*table-layout:\s*fixed/.test(html), 'table-layout fixed');
  assert.ok(!/#frontier-table\s*\{[^}]*table-layout:\s*auto/.test(html), 'not table-layout auto');
  // Name cell ellipsizes within remaining space (overflow chain).
  assert.ok(/#frontier-table\s+\.ft-name\s*\{[^}]*overflow:\s*hidden/.test(html), 'name overflow hidden');
  assert.ok(/#frontier-table\s+\.ft-name\s*\{[^}]*text-overflow:\s*ellipsis/.test(html), 'name ellipsis');
  assert.ok(/#frontier-table\s+\.ft-name\s*\{[^}]*white-space:\s*nowrap/.test(html), 'name nowrap');
  assert.ok(/#frontier-table\s+\.ft-name\s*\{[^}]*min-width:\s*0/.test(html), 'name min-width 0');
  // Flex/table parent chain can shrink (min-width:0) so table cannot force pane wider.
  assert.ok(/#frontier-body\s*\{[^}]*min-width:\s*0/.test(html), 'frontier-body min-width 0');
  assert.ok(/#frontier-pane\s*\{[^}]*min-width:\s*0/.test(html), 'frontier-pane min-width 0');
  assert.ok(/#rhs-bottom\s*\{[^}]*min-width:\s*0/.test(html), 'rhs-bottom min-width 0');
  // No min-width on the table that forces pane overflow (px/ch/em floors).
  const tableBlock = html.match(/#frontier-table\s*\{[^}]*\}/);
  assert.ok(tableBlock, 'frontier-table rule present');
  assert.ok(!/min-width:\s*\d+(px|rem|em|ch)/.test(tableBlock[0]), 'table has no forcing min-width');
});

test('T177 chrome cols use explicit rem widths — not 1%/99% under fixed layout', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  // Explicit rem/ch widths for chrome; name flexes (no width:99%).
  assert.ok(/#frontier-table\s+\.ft-id\s*\{[^}]*width:\s*[\d.]+(rem|ch)/.test(html), 'id explicit rem/ch');
  assert.ok(/#frontier-table\s+\.ft-status\s*\{[^}]*width:\s*[\d.]+(rem|ch)/.test(html), 'status explicit rem/ch');
  assert.ok(/#frontier-table\s+\.ft-fanout\s*\{[^}]*width:\s*[\d.]+(rem|ch)/.test(html), 'fanout explicit rem/ch');
  // Forbid collapsed chrome under table-layout:fixed (owner fail: ID paints mid-name).
  assert.ok(!/#frontier-table\s+\.ft-id\s*\{[^}]*width:\s*1%/.test(html), 'id rejects width:1%');
  assert.ok(!/#frontier-table\s+\.ft-status\s*\{[^}]*width:\s*1%/.test(html), 'status rejects width:1%');
  assert.ok(!/#frontier-table\s+\.ft-fanout\s*\{[^}]*width:\s*1%/.test(html), 'fanout rejects width:1%');
  assert.ok(!/#frontier-table\s+\.ft-name\s*\{[^}]*width:\s*99%/.test(html), 'name rejects width:99%');
  // Name takes remaining space without a width claim.
  assert.ok(!/#frontier-table\s+\.ft-name\s*\{[^}]*width:\s*[\d.]+%/.test(html), 'name has no percent width');
  // Id ellipsizes long ids (residual: very long target ids).
  assert.ok(/#frontier-table\s+\.ft-id\s*\{[^}]*overflow:\s*hidden/.test(html), 'id overflow hidden');
  assert.ok(/#frontier-table\s+\.ft-id\s*\{[^}]*text-overflow:\s*ellipsis/.test(html), 'id ellipsis');
  // DOM order still id|name|status|fan (class sequence in row builder).
  const rowBuild = html.indexOf("id.className = 'ft-id'");
  const nameBuild = html.indexOf("name.className = 'ft-name'");
  const statusBuild = html.indexOf("status.className = 'ft-status'");
  const fanBuild = html.indexOf("fan.className = 'ft-fanout'");
  assert.ok(rowBuild >= 0 && nameBuild > rowBuild && statusBuild > nameBuild && fanBuild > statusBuild,
    'DOM order id|name|status|fan');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('All frontier_table tests passed');
