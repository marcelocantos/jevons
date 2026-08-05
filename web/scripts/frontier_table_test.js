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
      { id: 'T131', name: 'RHS frontier table', status: 'Converging', fanout: 0, dependents: [] },
      {
        id: 'T27.1',
        name: 'Provider contract',
        status: 'Converging',
        fanout: 3,
        value: 8,
        acceptance: ['Criterion A holds', 'Criterion B holds'],
        context: 'Why this target exists.',
        tags: ['providers', 'api'],
        dependents: [
          { id: 'T27.2', name: 'Child A' },
          { id: 'T27.3', name: 'Child B' },
          { id: 'T27.4', name: 'Child C' },
        ],
      },
    ],
    updated_at: '2026-08-03T00:00:00Z',
  });
  assert.strictEqual(m.available, true);
  assert.strictEqual(m.rows.length, 2);
  assert.strictEqual(m.rows[0].id, 'T131');
  assert.strictEqual(m.rows[1].fanout, 3);
  assert.strictEqual(m.rows[1].dependents.length, 3);
  assert.strictEqual(m.rows[1].dependents[0].id, 'T27.2');
  assert.strictEqual(m.rows[1].dependents[0].name, 'Child A');
  // 🎯T181: acceptance/context/tags pass through normalizePayload.
  assert.deepStrictEqual(m.rows[1].acceptance, ['Criterion A holds', 'Criterion B holds']);
  assert.strictEqual(m.rows[1].context, 'Why this target exists.');
  assert.deepStrictEqual(m.rows[1].tags, ['providers', 'api']);
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

// 🎯T179: lead count line + "• TID Name" bullets (space, not em-dash) from dependents[{id,name}].
test('formatFanout InstantTip lists dependents with id + name (🎯T179)', function () {
  const deps = [
    { id: 'T10.3', name: 'Client requests table drives server actions' },
    { id: 'T10.4', name: 'Reconnect uses diff sync only' },
    { id: 'T10.5', name: 'Short' },
    { id: 'T10.6', name: 'Also blocked' },
  ];
  const many = FT.formatFanout(4, 'T10.2', deps);
  assert.strictEqual(many.visible, true);
  assert.strictEqual(many.text, '4\u169B');
  // Lead line unchanged.
  assert.ok(many.title.indexOf('4 targets depend on T10.2') === 0, 'lead line first: ' + many.title);
  // Bullets: id + space + name (owner pin: not em-dash-only).
  assert.ok(many.title.indexOf('• T10.3 Client requests table drives server actions') >= 0, many.title);
  assert.ok(many.title.indexOf('• T10.4 Reconnect uses diff sync only') >= 0, many.title);
  assert.ok(many.title.indexOf('• T10.5 Short') >= 0, many.title);
  assert.ok(many.title.indexOf('• T10.6 Also blocked') >= 0, many.title);
  assert.ok(many.title.indexOf('—') < 0, 'no em-dash in fanout tip: ' + many.title);
  // Multi-line (lead + 4 bullets).
  assert.strictEqual(many.title.split('\n').length, 5, many.title);

  const one = FT.formatFanout(1, 'T2', [{ id: 'T3', name: 'Blocked' }]);
  assert.strictEqual(one.title, '1 target depends on T2\n• T3 Blocked');

  // Hide when empty dependents / zero.
  const z = FT.formatFanout(0, 'T9', []);
  assert.strictEqual(z.visible, false);
  assert.strictEqual(z.title, '');

  // Bare string dependents still list ids (name optional).
  const bare = FT.formatFanout(2, 'T1', ['T2', 'T3']);
  assert.ok(bare.title.indexOf('2 targets depend on T1') === 0);
  assert.ok(bare.title.indexOf('• T2') >= 0);
  assert.ok(bare.title.indexOf('• T3') >= 0);
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

// 🎯T179: no small-caps status; tighter id/fanout than T177 (4.5/2.75); dependents tip wiring.
test('T179 status normal case + tight widths + dependents tip wiring', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const statusBlock = html.match(/#frontier-table\s+\.ft-status\s*\{[^}]*\}/);
  assert.ok(statusBlock, 'ft-status rule');
  assert.ok(!/font-variant\s*:\s*small-caps/.test(statusBlock[0]), 'no small-caps on .ft-status');
  assert.ok(!/#frontier-table\s+\.ft-status\s*\{[^}]*font-variant\s*:\s*small-caps/.test(html),
    'no small-caps anywhere on ft-status');

  const idW = html.match(/#frontier-table\s+\.ft-id\s*\{[^}]*width:\s*([\d.]+)(rem|ch)/);
  const fanW = html.match(/#frontier-table\s+\.ft-fanout\s*\{[^}]*width:\s*([\d.]+)(rem|ch)/);
  const stW = html.match(/#frontier-table\s+\.ft-status\s*\{[^}]*width:\s*([\d.]+)(rem|ch)/);
  assert.ok(idW, 'id width');
  assert.ok(fanW, 'fanout width');
  assert.ok(stW, 'status width');
  // Tighter than T177: id < 4.5rem, fanout < 2.75rem; status ~2rem.
  if (idW[2] === 'rem') {
    assert.ok(parseFloat(idW[1]) < 4.5, 'id tighter than 4.5rem, got ' + idW[1]);
  }
  if (fanW[2] === 'rem') {
    assert.ok(parseFloat(fanW[1]) < 2.75, 'fanout tighter than 2.75rem, got ' + fanW[1]);
  }
  if (stW[2] === 'rem') {
    assert.ok(Math.abs(parseFloat(stW[1]) - 2) < 0.5, 'status ~2rem, got ' + stW[1]);
  }

  // Render passes dependents into formatFanout (not fanout-only).
  assert.ok(html.indexOf('formatFanout(row.fanout, row.id, row.dependents)') >= 0 ||
    /formatFanout\s*\(\s*row\.fanout\s*,\s*row\.id\s*,\s*row\.dependents\s*\)/.test(html),
    'formatFanout receives row.dependents');
});

// 🎯T181: rich markdown target card includes acceptance + multi-section markers.
test('formatTargetCardMarkdown includes id name status acceptance context tags (🎯T181)', function () {
  const md = FT.formatTargetCardMarkdown({
    id: 'T181',
    name: 'Rich target hover on Frontier',
    status: 'Converging',
    acceptance: [
      'Hovering a frontier target ID shows InstantTip with full target',
      'Hermetic tip includes acceptance text',
    ],
    context: 'Owner wants fully expanded target in rich markdown.',
    tags: ['ui', 'frontier'],
    dependents: [{ id: 'T999', name: 'Downstream' }],
  });
  assert.ok(md.indexOf('🎯T181') >= 0, 'id: ' + md);
  assert.ok(md.indexOf('Rich target hover on Frontier') >= 0, 'name');
  assert.ok(md.indexOf('**Status:**') >= 0 || md.indexOf('Converging') >= 0, 'status section');
  assert.ok(md.indexOf('**Acceptance**') >= 0, 'acceptance heading');
  assert.ok(md.indexOf('Hovering a frontier target ID shows InstantTip with full target') >= 0,
    'acceptance criterion text');
  assert.ok(md.indexOf('Hermetic tip includes acceptance text') >= 0, 'second acceptance');
  assert.ok(md.indexOf('**Context**') >= 0, 'context heading');
  assert.ok(md.indexOf('Owner wants fully expanded') >= 0, 'context body');
  assert.ok(md.indexOf('**Tags:**') >= 0 && md.indexOf('ui') >= 0, 'tags');
  assert.ok(md.indexOf('**Dependents**') >= 0 && md.indexOf('T999') >= 0, 'dependents');
  // Multi-section markdown (not a single name string).
  assert.ok(md.split('\n').length >= 6, 'multi-line card: ' + md);

  const plain = FT.formatTargetCardPlain({
    id: 'T181',
    name: 'Rich target hover',
    status: 'converging',
    acceptance: ['A holds'],
  });
  assert.ok(plain.indexOf('🎯T181') >= 0);
  assert.ok(plain.indexOf('A holds') >= 0);
  assert.strictEqual(FT.formatTargetCardMarkdown(null), '');
  assert.strictEqual(FT.formatTargetCardMarkdown({}), '');
});

// 🎯T184: full semantic card — common fields, extra kv, mermaid with focus id.
test('formatTargetCardMarkdown semantic fields + mermaid minigraph (🎯T184)', function () {
  const row = {
    id: 'T184',
    name: 'Full semantic frontier card',
    status: 'Converging',
    value: 8,
    cost: 3,
    acceptance: [
      'Card shows acceptance list',
      'Mermaid minigraph includes focus id',
    ],
    context: 'Owner wants full target view with deps graph.',
    tags: ['ui', 'frontier'],
    depends_on: [{ id: 'T181', name: 'Rich hover' }],
    dependents: [{ id: 'T999', name: 'Downstream consumer' }],
    attestation: 'SHA abc1234 + green oracles',
    origin: 'manual',
    discovered: '2026-08-03',
    extra: { owner: 'jevons-po', custom_flag: 'yes' },
  };
  const md = FT.formatTargetCardMarkdown(row);
  assert.ok(md.indexOf('🎯T184') >= 0, 'focus id in card');
  assert.ok(md.indexOf('**Status:**') >= 0 && md.indexOf('Converging') >= 0, 'status');
  assert.ok(md.indexOf('**Value / cost:**') >= 0, 'value/cost section');
  assert.ok(md.indexOf('value 8') >= 0 && md.indexOf('cost 3') >= 0, 'metrics: ' + md);
  assert.ok(md.indexOf('**Tags:**') >= 0 && md.indexOf('ui') >= 0, 'tags');
  assert.ok(md.indexOf('**Depends on**') >= 0 && md.indexOf('T181') >= 0, 'depends_on');
  assert.ok(md.indexOf('**Dependents**') >= 0 && md.indexOf('T999') >= 0, 'dependents');
  assert.ok(md.indexOf('**Acceptance**') >= 0, 'acceptance heading');
  assert.ok(md.indexOf('Card shows acceptance list') >= 0, 'acceptance text');
  assert.ok(md.indexOf('**Context**') >= 0 && md.indexOf('full target view') >= 0, 'context');
  assert.ok(md.indexOf('**Attestation**') >= 0 && md.indexOf('SHA abc1234') >= 0, 'attestation');
  assert.ok(md.indexOf('**Other fields**') >= 0, 'extra heading');
  assert.ok(md.indexOf('owner') >= 0 && md.indexOf('jevons-po') >= 0, 'extra owner');
  assert.ok(md.indexOf('custom_flag') >= 0, 'extra custom');
  // Hermetic: mermaid fragment with focus id.
  assert.ok(md.indexOf('```mermaid') >= 0, 'mermaid fence: ' + md);
  assert.ok(md.indexOf('graph LR') >= 0, 'graph LR');
  assert.ok(/T184/.test(md), 'focus id appears in graph body');
  const graph = FT.formatDepMinigraph(row);
  assert.ok(graph.indexOf('```mermaid') === 0 || graph.indexOf('```mermaid') >= 0, 'graph fence');
  assert.ok(graph.indexOf(FT.mermaidNodeId('T184')) >= 0, 'safe focus node');
  assert.ok(graph.indexOf('-->') >= 0, 'edges present');
  // One-sided still draws focus node.
  const oneSide = FT.formatDepMinigraph({ id: 'T50', name: 'Solo', depends_on: [], dependents: [] });
  assert.ok(oneSide.indexOf('```mermaid') >= 0 && oneSide.indexOf('T50') >= 0, 'solo graph');

  // normalizePayload passes through T184 fields.
  const m = FT.normalizePayload({
    available: true,
    targets: [{
      id: 'T184',
      name: 'Full card',
      status: 'Converging',
      fanout: 1,
      value: 2,
      cost: 1,
      acceptance: ['A holds'],
      context: 'ctx',
      tags: ['t'],
      depends_on: [{ id: 'T1', name: 'Base' }],
      dependents: [{ id: 'T2', name: 'Child' }],
      attestation: 'ok',
      origin: 'manual',
      discovered: '2026-08-03',
      extra: { owner: 'x' },
    }],
  });
  assert.strictEqual(m.rows[0].depends_on[0].id, 'T1');
  assert.strictEqual(m.rows[0].cost, 1);
  assert.strictEqual(m.rows[0].attestation, 'ok');
  assert.strictEqual(m.rows[0].extra.owner, 'x');
  assert.deepStrictEqual(m.rows[0].acceptance, ['A holds']);
});

// 🎯T181: index wires rich card on .ft-id and .ft-name with left-of-pointer + html.
test('T181 index.html rich card tip on id/name + InstantTip placement', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const start = html.indexOf('// 🎯T173: headerless table');
  assert.ok(start >= 0);
  const end = html.indexOf('function loadFrontier', start);
  const region = html.slice(start, end);
  assert.ok(region.indexOf('formatTargetCardMarkdown') >= 0, 'card markdown builder');
  assert.ok(region.indexOf('InstantTip.attach(id') >= 0 || /InstantTip\.attach\(\s*id/.test(region),
    'attach on id cell');
  // 🎯T231: single attach with groupHosts [id, name] — not dual attach on name.
  assert.ok(region.indexOf('groupHosts') >= 0, 'groupHosts for id+name hit rect');
  assert.ok(region.indexOf('hitGroup') >= 0, 'hitGroup single-rect model');
  assert.ok(region.indexOf('html: true') >= 0 || region.indexOf('html:true') >= 0, 'html tip content');
  assert.ok(region.indexOf('left-of-pointer') >= 0 || region.indexOf('PLACE_LEFT_OF_POINTER') >= 0,
    'left-of-pointer placement');
  assert.ok(region.indexOf('instant-tip-card') >= 0 || region.indexOf('CARD_CLASS') >= 0,
    'card class');
  assert.ok(region.indexOf('id.title') < 0, 'no id.title native');
  assert.ok(/\.instant-tip-card\s*\{/.test(html) || /\.instant-tip\.instant-tip-card/.test(html),
    'card CSS');
  assert.ok(html.indexOf('parseAssistantMarkdown') >= 0, 'markdown render path available');
});

// 🎯T184: index wires mermaid render on card tips + mermaid CSS in tip.
test('T184 index.html mermaid on target card + semantic payload fields', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const start = html.indexOf('// 🎯T173: headerless table');
  assert.ok(start >= 0);
  const end = html.indexOf('function loadFrontier', start);
  const region = html.slice(start, end > start ? end : start + 9000);
  assert.ok(region.indexOf('renderMermaidIn') >= 0, 'renderMermaidIn on tip: ' + region.slice(0, 200));
  assert.ok(region.indexOf('parseAssistantMarkdown') >= 0, 'markdown path for card');
  assert.ok(region.indexOf('left-of-pointer') >= 0 || region.indexOf('PLACE_LEFT_OF_POINTER') >= 0,
    'placement retained');
  assert.ok(/\.instant-tip\.instant-tip-card\s+\.mermaid-diagram/.test(html) ||
    html.indexOf('instant-tip-card .mermaid-diagram') >= 0,
    'card mermaid CSS');
  // Script still exports minigraph helper.
  const src = fs.readFileSync(path.join(__dirname, 'frontier_table.js'), 'utf8');
  assert.ok(src.indexOf('formatDepMinigraph') >= 0, 'formatDepMinigraph exported');
  assert.ok(src.indexOf('depends_on') >= 0, 'depends_on in client');
});

// 🎯T182: pure kickoff request → jevons-po send path with target id/name brief.
test('playKickoffRequest messages jevons-po with full brief (🎯T182)', function () {
  assert.strictEqual(FT.DEFAULT_PLAY_PO, 'jevons-po');
  assert.strictEqual(FT.PLAY_GLYPH, '\u25B6');
  assert.strictEqual(FT.resolvePlayPO(), 'jevons-po');
  assert.strictEqual(FT.agentSendPath('jevons-po'), '/api/agents/jevons-po/send');
  assert.strictEqual(FT.agentSendPath('my-po'), '/api/agents/my-po/send');

  const req = FT.playKickoffRequest({
    id: 'T182',
    name: 'Frontier play + tight status/fan',
    status: 'Converging',
    acceptance: ['Play sends to PO', 'Tight CSS'],
  });
  assert.strictEqual(req.method, 'POST');
  assert.strictEqual(req.po, 'jevons-po');
  assert.strictEqual(req.url, '/api/agents/jevons-po/send');
  assert.ok(req.body && req.body.text, 'body.text present');
  const text = req.body.text;
  assert.ok(text.indexOf('T182') >= 0, 'target id: ' + text);
  assert.ok(text.indexOf('Frontier play') >= 0, 'name: ' + text);
  assert.ok(text.indexOf('parent=jevons-po') >= 0, 'parent lineage: ' + text);
  assert.ok(/spawn|brief|Kick off/i.test(text), 'kick off language: ' + text);
  assert.ok(text.indexOf('Play sends to PO') >= 0, 'acceptance in brief');
  // 🎯T197: kickoff brief teaches literal-dot hierarchical worker names.
  assert.ok(text.indexOf('jv-t27.2-config') >= 0, 'T197 dotted example: ' + text);
  assert.ok(text.indexOf('jv-t272-config') >= 0, 'T197 anti-squash example: ' + text);
  assert.ok(text.indexOf('jv-t159-seal') >= 0, 'T197 flat residual: ' + text);
  assert.ok(/literal dots/i.test(text), 'T197 policy phrase: ' + text);
  assert.strictEqual(FT.buildPlayKickoffText(null), '');
  assert.strictEqual(FT.buildPlayKickoffText({}), '');
});

// 🎯T255: resolvePlayPO binds to selected product PO (not hard-coded jevons-po).
test('T255 resolvePlayPO selected PO / worker parent / residual default', function () {
  assert.strictEqual(typeof FT.resolvePlayPO, 'function');
  assert.strictEqual(typeof FT.isProductOwnerName, 'function');
  assert.strictEqual(typeof FT.playKickoffTitle, 'function');
  assert.strictEqual(FT.isProductOwnerName('yourworld2-po'), true);
  assert.strictEqual(FT.isProductOwnerName('jevons-po'), true);
  assert.strictEqual(FT.isProductOwnerName('jv-t44.1-worker'), false);
  assert.strictEqual(FT.isProductOwnerName('jevons'), false);
  assert.strictEqual(FT.isProductOwnerName(''), false);

  const agents = [
    { name: 'jevons', purpose: 'overseer', workdir: '/Users/x/.jevons/jevons' },
    {
      name: 'jevons-po',
      purpose: 'work',
      parent: 'jevons',
      workdir: '/Users/x/work/github.com/marcelocantos/jevons',
    },
    {
      name: 'yourworld2-po',
      purpose: 'work',
      parent: 'jevons',
      workdir: '/Users/x/work/github.com/marcelocantos/yourworld2',
    },
    {
      name: 'yw2-worker',
      purpose: 'work',
      parent: 'yourworld2-po',
      workdir: '/Users/x/work/github.com/marcelocantos/yourworld2',
    },
    {
      name: 'jv-worker',
      purpose: 'work',
      parent: 'jevons-po',
      workdir: '/Users/x/work/github.com/marcelocantos/jevons',
    },
    {
      name: 'yw2-boss',
      purpose: 'work',
      parent: 'yourworld2-po',
      workdir: '/Users/x/work/github.com/marcelocantos/yourworld2',
    },
    {
      name: 'yw2-leaf',
      purpose: 'work',
      parent: 'yw2-boss',
      workdir: '/Users/x/work/github.com/marcelocantos/yourworld2',
    },
  ];

  // No selection / overseer → residual default jevons-po.
  assert.strictEqual(FT.resolvePlayPO(), 'jevons-po');
  assert.strictEqual(FT.resolvePlayPO({}), 'jevons-po');
  assert.strictEqual(FT.resolvePlayPO({ selectedAgent: null, agents: agents }), 'jevons-po');
  assert.strictEqual(FT.resolvePlayPO({ selectedAgent: '', agents: agents }), 'jevons-po');
  assert.strictEqual(FT.resolvePlayPO({ selectedAgent: 'jevons', agents: agents }), 'jevons-po');

  // Selected is product owner → that PO.
  assert.strictEqual(
    FT.resolvePlayPO({ selectedAgent: 'yourworld2-po', agents: agents }),
    'yourworld2-po');
  assert.strictEqual(
    FT.resolvePlayPO({ selectedAgent: 'jevons-po', agents: agents }),
    'jevons-po');

  // Selected worker → parent PO.
  assert.strictEqual(
    FT.resolvePlayPO({ selectedAgent: 'yw2-worker', agents: agents }),
    'yourworld2-po');
  assert.strictEqual(
    FT.resolvePlayPO({ selectedAgent: 'jv-worker', agents: agents }),
    'jevons-po');

  // Walk parent chain (leaf → boss → PO).
  assert.strictEqual(
    FT.resolvePlayPO({ selectedAgent: 'yw2-leaf', agents: agents }),
    'yourworld2-po');

  // Explicit po override wins.
  assert.strictEqual(
    FT.resolvePlayPO({
      po: 'explicit-po',
      selectedAgent: 'yourworld2-po',
      agents: agents,
    }),
    'explicit-po');

  // Dual-agent fixture: playKickoffRequest hits selected product PO, not jevons-po.
  const ywReq = FT.playKickoffRequest(
    { id: 'T44.1', name: 'Externalize overseer prompt', status: 'Converging' },
    { selectedAgent: 'yourworld2-po', agents: agents }
  );
  assert.strictEqual(ywReq.blocked, false);
  assert.strictEqual(ywReq.po, 'yourworld2-po');
  assert.strictEqual(ywReq.url, '/api/agents/yourworld2-po/send');
  assert.ok(ywReq.body.text.indexOf('parent=yourworld2-po') >= 0, ywReq.body.text);
  assert.ok(ywReq.body.text.indexOf('T44.1') >= 0);

  const jvReq = FT.playKickoffRequest(
    { id: 'T255', name: 'Play PO routing', status: 'Converging' },
    { selectedAgent: 'jevons-po', agents: agents }
  );
  assert.strictEqual(jvReq.po, 'jevons-po');
  assert.strictEqual(jvReq.url, '/api/agents/jevons-po/send');
  assert.ok(jvReq.body.text.indexOf('parent=jevons-po') >= 0);

  // Worker selection routes kickoff to parent PO send path.
  const workerReq = FT.playKickoffRequest(
    { id: 'T99', name: 'Worker selection', status: 'Converging' },
    { selectedAgent: 'yw2-worker', agents: agents }
  );
  assert.strictEqual(workerReq.po, 'yourworld2-po');
  assert.strictEqual(workerReq.url, '/api/agents/yourworld2-po/send');

  // Tooltip title shows real recipient.
  assert.strictEqual(FT.playKickoffTitle('yourworld2-po'), 'Start work via yourworld2-po');
  assert.strictEqual(FT.playKickoffTitle('jevons-po'), 'Start work via jevons-po');
  assert.strictEqual(FT.playKickoffTitle(''), 'Start work via jevons-po');
  assert.strictEqual(FT.playKickoffTitle(null), 'Start work via jevons-po');

  // Name-only (not in list) still honors *-po shape.
  assert.strictEqual(
    FT.resolvePlayPO({ selectedAgent: 'other-product-po', agents: [] }),
    'other-product-po');
});

// 🎯T255: index.html wires selectedAgent into playKickoff + real tooltip recipient.
test('T255 index.html play path uses resolvePlayPO + selectedAgent', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');

  // Tooltip not hard-coded to jevons-po alone.
  assert.ok(html.indexOf("playBtn.title = 'Start work via jevons-po'") < 0,
    'must not hard-code play tooltip to jevons-po');
  assert.ok(html.indexOf('playKickoffTitle') >= 0 || html.indexOf('resolvePlayPO') >= 0,
    'play title uses resolvePlayPO / playKickoffTitle');
  assert.ok(html.indexOf('data-play-po') >= 0, 'data-play-po on button for probe');

  // playFrontierTarget passes selection + agents into playKickoffRequest.
  const start = html.indexOf('function playFrontierTarget');
  assert.ok(start >= 0, 'playFrontierTarget defined');
  // Include preceding T255 comment block.
  const regionStart = Math.max(0, start - 200);
  const end = html.indexOf('\nfunction ', start + 10);
  const body = html.slice(regionStart, end > start ? end : start + 3500);
  assert.ok(body.indexOf('selectedAgent') >= 0, 'playFrontierTarget reads selectedAgent');
  assert.ok(body.indexOf('playKickoffRequest(row, playOpts)') >= 0 ||
    /playKickoffRequest\s*\(\s*row\s*,/.test(body),
    'playKickoffRequest receives opts with selection');
  assert.ok(body.indexOf('T255') >= 0, 'T255 marked on playFrontierTarget');
  assert.ok(body.indexOf('frontierAgentsCache') >= 0 || body.indexOf('lastFleetAgents') >= 0,
    'agents list passed for parent-PO walk');

  // Render path sets title from resolvePlayPO.
  const renderStart = html.indexOf('function renderFrontierTable');
  assert.ok(renderStart >= 0);
  const renderEnd = html.indexOf('function loadFrontier', renderStart);
  const region = html.slice(renderStart, renderEnd > renderStart ? renderEnd : renderStart + 9000);
  assert.ok(region.indexOf('resolvePlayPO') >= 0, 'render resolves play PO');
  assert.ok(region.indexOf('selectedAgent') >= 0, 'render uses selectedAgent for play title');
});

// 🎯T182: CSS tight status/fan + play column + wiring (mockable send path).
test('T182 tight status/fan CSS + play cell + send path wiring', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');

  const stBlock = html.match(/#frontier-table\s+\.ft-status\s*\{[^}]*\}/);
  const fanBlock = html.match(/#frontier-table\s+\.ft-fanout\s*\{[^}]*\}/);
  assert.ok(stBlock, 'ft-status rule');
  assert.ok(fanBlock, 'ft-fanout rule');
  // Near-zero pad between status and fanout.
  assert.ok(/padding-right:\s*0/.test(stBlock[0]), 'status padding-right 0: ' + stBlock[0]);
  assert.ok(/padding-left:\s*0/.test(fanBlock[0]), 'fanout padding-left 0: ' + fanBlock[0]);

  assert.ok(/#frontier-table\s+\.ft-play\s*\{/.test(html), 'ft-play column CSS');
  assert.ok(/#frontier-table\s+\.ft-play-btn\s*\{/.test(html) || html.indexOf('ft-play-btn') >= 0,
    'play button class');
  // Play glyph uses medium product green (not accent red/purple).
  const playBtnBlock = html.match(/#frontier-table\s+\.ft-play-btn\s*\{[^}]*\}/);
  assert.ok(playBtnBlock && /color:\s*var\(--green\)/.test(playBtnBlock[0]),
    'play btn medium green: ' + (playBtnBlock ? playBtnBlock[0] : 'missing'));

  const start = html.indexOf('// 🎯T173: headerless table');
  assert.ok(start >= 0);
  // Include playFrontierTarget (defined after render) in region end.
  const end = html.indexOf('function loadFrontier', start);
  const region = html.slice(start, end > start ? end : start + 8000);
  assert.ok(region.indexOf("play.className = 'ft-play'") >= 0 || region.indexOf('ft-play') >= 0,
    'play cell in row builder');
  assert.ok(region.indexOf('ft-play-btn') >= 0, 'play button in row builder');
  assert.ok(region.indexOf('playFrontierTarget') >= 0, 'playFrontierTarget called');
  assert.ok(html.indexOf('function playFrontierTarget') >= 0, 'playFrontierTarget defined');
  assert.ok(html.indexOf('playKickoffRequest') >= 0, 'uses pure kickoff request');
  assert.ok(html.indexOf('__frontierAgentSend') >= 0, 'mockable send seam');
  assert.ok(html.indexOf('/api/agents/') >= 0 && /\/api\/agents\/.*send/.test(html) ||
    html.indexOf("'/send'") >= 0 || html.indexOf('/send') >= 0,
    'agent send path present');

  // DOM order id|name|status|fan|play
  const idB = html.indexOf("id.className = 'ft-id'");
  const nameB = html.indexOf("name.className = 'ft-name'");
  const stB = html.indexOf("status.className = 'ft-status'");
  const fanB = html.indexOf("fan.className = 'ft-fanout'");
  const playB = html.indexOf("play.className = 'ft-play'");
  assert.ok(idB >= 0 && nameB > idB && stB > nameB && fanB > stB && playB > fanB,
    'DOM order id|name|status|fan|play');

  // Does not clobber T181 rich hover wiring.
  assert.ok(html.indexOf('formatTargetCardMarkdown') >= 0, 'T181 card still present');
  assert.ok(html.indexOf('left-of-pointer') >= 0 || html.indexOf('PLACE_LEFT_OF_POINTER') >= 0,
    'T181 placement still present');
});

// 🎯T185: pure unachieved graph builder + Graph control + large panel wiring.
// 🎯T190: multi-diagram pack (component + orphans), wrap-grid — not mega subgraph strip.
test('buildActiveDependencyDiagrams multi-diagram pack (🎯T185/T190)', function () {
  assert.strictEqual(FT.GRAPH_API_PATH, '/api/frontier/graph');
  assert.strictEqual(FT.FRONTIER_GRAPH_PACK, 'wrap-grid');
  const pack = FT.buildActiveDependencyDiagrams([
    { id: 'T2', name: 'Ready leaf', depends_on: [{ id: 'T1', name: 'Done' }] },
    { id: 'T3', name: 'Blocked', depends_on: [{ id: 'T2' }] },
    { id: 'T3.1', name: 'Nested', depends_on: ['T3', 'T1'] },
    { id: 'T4', name: 'Orphan', depends_on: [] },
  ]);
  assert.strictEqual(pack.pack, 'wrap-grid');
  assert.strictEqual(pack.nodeCount, 4);
  assert.strictEqual(pack.edgeCount, 2);
  // Component {T2,T3,T3.1} + orphans {T4}.
  assert.strictEqual(pack.diagrams.length, 2, JSON.stringify(pack.diagrams.map(function (d) {
    return { id: d.id, kind: d.kind, n: d.nodeCount };
  })));
  const comp = pack.diagrams.find(function (d) { return d.kind === 'component'; });
  const orph = pack.diagrams.find(function (d) { return d.kind === 'orphans'; });
  assert.ok(comp, 'component block');
  assert.ok(orph, 'orphans block');
  assert.strictEqual(comp.nodeCount, 3);
  assert.strictEqual(comp.edgeCount, 2);
  assert.strictEqual(orph.nodeCount, 1);
  // Layout directives inside each diagram.
  [comp, orph].forEach(function (d) {
    assert.ok(d.mermaid.indexOf('%%{init:') >= 0, 'init in ' + d.id);
    assert.ok(/useMaxWidth['"]?\s*:\s*true/.test(d.mermaid), 'useMaxWidth');
    assert.ok(/nodeSpacing['"]?\s*:\s*\d+/.test(d.mermaid), 'nodeSpacing');
    assert.ok(/rankSpacing['"]?\s*:\s*\d+/.test(d.mermaid), 'rankSpacing');
    assert.ok(/wrappingWidth['"]?\s*:\s*\d+/.test(d.mermaid), 'wrappingWidth');
    assert.ok(d.mermaid.indexOf('flowchart TB') >= 0, 'flowchart TB');
    assert.ok(d.mermaid.indexOf('subgraph island_') < 0, 'no subgraph island packing');
  });
  assert.ok(comp.mermaid.indexOf('T2[') >= 0 && comp.mermaid.indexOf('T3[') >= 0
    && comp.mermaid.indexOf('T3_1[') >= 0, 'component nodes');
  assert.ok(orph.mermaid.indexOf('T4[') >= 0, 'orphan node');
  assert.ok(comp.mermaid.indexOf('T1[') < 0, 'no T1');
  assert.ok(comp.mermaid.indexOf('|needs| T1') < 0, 'no edge to T1');
  assert.ok(comp.mermaid.indexOf('T3 -.->|needs| T2') >= 0, 'T3→T2');
  assert.ok(comp.mermaid.indexOf('T3_1 -.->|needs| T3') >= 0, 'T3.1→T3');
  // Joined pin source carries multi-diagram packing directives.
  assert.ok(pack.mermaid.indexOf('jevons-frontier-pack') >= 0, pack.mermaid.slice(0, 120));
  assert.ok(pack.mermaid.indexOf('pack=wrap-grid') >= 0, 'pack directive');
  assert.ok(pack.mermaid.indexOf('kind=component') >= 0 && pack.mermaid.indexOf('kind=orphans') >= 0);
  // Legacy builder returns joined multi-diagram source.
  const src = FT.buildActiveDependencyMermaid([
    { id: 'T2', depends_on: ['T3'] },
    { id: 'T3' },
  ]);
  assert.ok(src.indexOf('jevons-frontier-pack') >= 0, src.slice(0, 80));
  // Empty set still valid pack.
  const empty = FT.buildActiveDependencyDiagrams([]);
  assert.strictEqual(empty.pack, 'wrap-grid');
  assert.ok(empty.diagrams.length >= 1);
  assert.ok(empty.diagrams[0].mermaid.indexOf('flowchart TB') >= 0);
});

test('packActiveGraphIslands + splitOrphanComponents (🎯T190)', function () {
  // Two islands: A-B connected, C alone, D-E connected.
  const islands = FT.packActiveGraphIslands(
    ['B', 'A', 'E', 'C', 'D'],
    [{ from: 'A', to: 'B' }, { from: 'D', to: 'E' }]
  );
  assert.strictEqual(islands.length, 3, JSON.stringify(islands));
  // Ordered by first id after sort within component.
  assert.deepStrictEqual(islands[0], ['A', 'B']);
  assert.deepStrictEqual(islands[1], ['C']);
  assert.deepStrictEqual(islands[2], ['D', 'E']);
  const split = FT.splitOrphanComponents(islands);
  assert.strictEqual(split.connected.length, 2);
  assert.deepStrictEqual(split.orphans, ['C']);
});

// 🎯T199: natural/version order for target ids (not pure lex).
test('targetIDCompare natural sort (🎯T199)', function () {
  assert.strictEqual(typeof FT.targetIDCompare, 'function');
  assert.strictEqual(typeof FT.targetIDLess, 'function');

  const want = ['T1', 'T2', 'T10', 'T10.2', 'T27', 'T27.3', 'T100'];
  const shuffled = ['T100', 'T10', 'T27.3', 'T2', 'T10.2', 'T1', 'T27'];
  shuffled.sort(FT.targetIDCompare);
  assert.deepStrictEqual(shuffled, want);

  assert.ok(FT.targetIDLess('T10.2', 'T10.10'), 'T10.2 < T10.10');
  assert.ok(!FT.targetIDLess('T10.10', 'T10.2'), 'not T10.10 < T10.2');
  assert.ok(FT.targetIDLess('T1', 'T1.1'));
  assert.ok(FT.targetIDLess('T1.1', 'T2'));
  assert.ok(!FT.targetIDLess('T10', 'T10'));
  assert.ok(FT.targetIDLess('foo2', 'foo10'), 'non-T residual');
  assert.ok(FT.targetIDLess('T1', 'T01'), 'leading zeros: shorter first');
  assert.strictEqual(FT.targetIDCompare('T10', 'T10'), 0);
});

test('packActiveGraphIslands natural target ids (🎯T199)', function () {
  const islands = FT.packActiveGraphIslands(
    ['T10', 'T2', 'T100', 'T10.2'],
    [{ from: 'T2', to: 'T10' }]
  );
  assert.strictEqual(islands.length, 3, JSON.stringify(islands));
  // Natural: {T2,T10}, {T10.2}, {T100} — not lex {T10,T10.2,T100,T2}.
  assert.deepStrictEqual(islands[0], ['T2', 'T10']);
  assert.deepStrictEqual(islands[1], ['T10.2']);
  assert.deepStrictEqual(islands[2], ['T100']);
});

test('buildActiveDependencyDiagrams id order natural (🎯T199)', function () {
  // Nodes natural order within component (T2 before T10); orphans separate.
  const pack = FT.buildActiveDependencyDiagrams([
    { id: 'T10', name: 'Ten' },
    { id: 'T2', name: 'Two', depends_on: [{ id: 'T10' }] },
    { id: 'T100', name: 'Hundred' },
  ]);
  const comp = pack.diagrams.find(function (d) { return d.kind === 'component'; });
  const orph = pack.diagrams.find(function (d) { return d.kind === 'orphans'; });
  assert.ok(comp, pack.mermaid);
  assert.ok(orph, 'T100 orphan');
  const i2 = comp.mermaid.indexOf('T2[');
  const i10 = comp.mermaid.indexOf('T10[');
  assert.ok(i2 >= 0 && i10 >= 0, comp.mermaid);
  assert.ok(i2 < i10, 'T2 before T10 in component: ' + comp.mermaid);
  assert.ok(orph.mermaid.indexOf('T100[') >= 0, orph.mermaid);
});

test('normalizeGraphPayload multi-diagram (🎯T185/T190)', function () {
  const ok = FT.normalizeGraphPayload({
    available: true,
    mermaid: '%% jevons-frontier-pack pack=wrap-grid diagrams=1 %%\nflowchart TB\n  A --> B\n',
    pack: 'wrap-grid',
    diagrams: [{
      id: 'c0',
      kind: 'component',
      title: 'Component (2 nodes)',
      mermaid: 'flowchart TB\n  A --> B\n',
      node_count: 2,
      edge_count: 1,
    }],
    node_count: 2,
    edge_count: 1,
    ledger: '/x/bullseye.yaml',
  });
  assert.strictEqual(ok.available, true);
  assert.strictEqual(ok.pack, 'wrap-grid');
  assert.strictEqual(ok.diagrams.length, 1);
  assert.strictEqual(ok.diagrams[0].id, 'c0');
  assert.strictEqual(ok.diagrams[0].nodeCount, 2);
  assert.ok(ok.mermaid.indexOf('wrap-grid') >= 0);
  assert.strictEqual(ok.nodeCount, 2);
  assert.strictEqual(ok.edgeCount, 1);
  // Single mermaid without diagrams[] → synthetic component block.
  const single = FT.normalizeGraphPayload({
    available: true,
    mermaid: 'flowchart TB\n  X --> Y\n',
    node_count: 2,
  });
  assert.strictEqual(single.diagrams.length, 1);
  assert.strictEqual(single.diagrams[0].kind, 'component');
  const bad = FT.normalizeGraphPayload(null, new Error('boom'));
  assert.strictEqual(bad.available, false);
  assert.ok(/boom/.test(bad.error));
});

test('T185/T190 index.html Graph control + large panel + multi-diagram pack CSS', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  // Graph button next to Refresh; Refresh retained (manual recovery).
  assert.ok(html.indexOf('id="frontier-graph"') >= 0, 'Graph button');
  assert.ok(/>Graph</.test(html) || html.indexOf('>Graph</button>') >= 0, 'Graph label');
  assert.ok(html.indexOf('id="frontier-refresh"') >= 0, 'Refresh retained');
  // ~90% overlay class on mermaid panel.
  assert.ok(/#mermaid-viz-panel\.mvp-large\s*\{/.test(html), 'mvp-large CSS');
  assert.ok(/90vw/.test(html) && /90vh/.test(html), '90vw/90vh sizing');
  // 🎯T190: svg max-width 100% so rendered graph cannot force panel wider.
  assert.ok(/#mermaid-viz-panel\s+\.mvp-body\s+svg\s*\{[^}]*max-width:\s*100%/.test(html)
    || /#mermaid-viz-panel \.mvp-body svg \{[^}]*max-width: 100%/.test(html),
    'mvp-body svg max-width 100%');
  // 🎯T190: multi-diagram wrap-grid pack blocks.
  assert.ok(html.indexOf('mvp-pack') >= 0, 'mvp-pack class');
  assert.ok(html.indexOf('mvp-pack-block') >= 0, 'mvp-pack-block');
  assert.ok(html.indexOf('auto-fill') >= 0, 'wrap-grid columns');
  assert.ok(html.indexOf('function renderMermaidDiagramPackInPanel') >= 0,
    'multi-diagram renderer');
  assert.ok(html.indexOf('renderMermaidDiagramPackInPanel') >= 0
    && html.indexOf('model.diagrams') >= 0, 'openFrontierGraph uses diagrams pack');
  // Open path: fetch graph API + openFrontierGraph + wire button.
  assert.ok(html.indexOf('/api/frontier/graph') >= 0, 'graph API path');
  assert.ok(html.indexOf('function openFrontierGraph') >= 0, 'openFrontierGraph defined');
  assert.ok(html.indexOf('openFrontierGraph') >= 0 && html.indexOf('frontier-graph') >= 0,
    'button wiring present');
  assert.ok(html.indexOf('GRAPH_API_PATH') >= 0 || html.indexOf('/api/frontier/graph') >= 0);
  assert.ok(html.indexOf('mvp-large') >= 0, 'large class used in JS/CSS');
  // 🎯T196: failed fetch → actionable error, not empty paste shell.
  assert.ok(html.indexOf('renderMermaidPanelFetchError') >= 0, 'T196 fetch-error renderer');
  const ofgStart = html.indexOf('function openFrontierGraph');
  const ofgEnd = html.indexOf('\nfunction ', ofgStart + 10);
  const ofg = html.slice(ofgStart, ofgEnd > ofgStart ? ofgEnd : ofgStart + 6000);
  const catchIdx = ofg.indexOf('.catch(');
  assert.ok(catchIdx >= 0, 'openFrontierGraph has catch');
  const catchCode = ofg.slice(catchIdx).replace(/\/\/[^\n]*/g, '');
  assert.ok(!/renderMermaidPanelEmpty\s*\(/.test(catchCode),
    'T196: catch must not use empty paste shell');
  assert.ok(catchCode.indexOf('renderMermaidPanelFetchError') >= 0,
    'T196: catch uses fetch-error path');
  // Pure module exports.
  assert.strictEqual(typeof FT.buildActiveDependencyMermaid, 'function');
  assert.strictEqual(typeof FT.buildActiveDependencyDiagrams, 'function');
  assert.strictEqual(typeof FT.normalizeGraphPayload, 'function');
  assert.strictEqual(typeof FT.packActiveGraphIslands, 'function');
  assert.strictEqual(typeof FT.splitOrphanComponents, 'function');
  assert.strictEqual(typeof FT.mermaidActiveGraphHeader, 'function');
});

// 🎯T198: engagement overlay — target_id equality, sink bottom, stop control.
// No name parsing: agent named T10.2-worker without target_id stays free.
test('applyEngagement by target_id sinks engaged bottom + stop request (🎯T198)', function () {
  assert.strictEqual(FT.STOP_GLYPH, '\u25A0');
  assert.strictEqual(FT.ENGAGEMENT_STOP_PATH, '/api/agents/engagement/stop');
  assert.strictEqual(FT.normalizeTargetID('🎯T10.2'), 'T10.2');
  assert.strictEqual(FT.normalizeTargetID('  T198 '), 'T198');

  const rows = [
    { id: 'T10.2', name: 'Server Peer' },
    { id: 'T1', name: 'Tool surface' },
    { id: 'T200', name: 'Portfolios' },
  ];
  // Name looks like a T-id but has NO target_id — must not engage.
  const agents = [
    { name: 'T10.2-worker', purpose: 'work' },
    { name: 'jv-fixture-worker', purpose: 'work', target_id: 'T10.2' },
    { name: 'jevons', purpose: 'overseer', target_id: 'T10.2' },
  ];
  const out = FT.applyEngagement(rows, agents);
  assert.strictEqual(out.length, 3);
  // Free first (relative order T1, T200), engaged last (T10.2).
  assert.strictEqual(out[0].id, 'T1');
  assert.strictEqual(out[0].engaged, false);
  assert.strictEqual(out[1].id, 'T200');
  assert.strictEqual(out[1].engaged, false);
  assert.strictEqual(out[2].id, 'T10.2');
  assert.strictEqual(out[2].engaged, true);
  assert.deepStrictEqual(out[2].engaged_agents, ['jv-fixture-worker']);

  // Index ignores overseer + name-only matches.
  const idx = FT.engagementIndex(agents);
  assert.deepStrictEqual(idx['T10.2'].agents, ['jv-fixture-worker']);

  const stop = FT.stopEngagementRequest('🎯T10.2');
  assert.strictEqual(stop.method, 'POST');
  assert.strictEqual(stop.url, '/api/agents/engagement/stop');
  assert.deepStrictEqual(stop.body, { target_id: 'T10.2' });

  // Kickoff brief requires target_id= on spawn.
  const text = FT.buildPlayKickoffText({ id: 'T10.2', name: 'Peer' });
  assert.ok(text.indexOf('target_id=T10.2') >= 0, 'target_id in brief: ' + text);
});

test('T198 index.html engaged stop wiring + agents merge', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('applyEngagement') >= 0, 'applyEngagement in render');
  assert.ok(html.indexOf('stopFrontierEngagement') >= 0, 'stopFrontierEngagement');
  assert.ok(html.indexOf('ft-engaged') >= 0, 'engaged row class');
  assert.ok(html.indexOf('ft-stop-btn') >= 0, 'stop button class');
  assert.ok(html.indexOf('STOP_GLYPH') >= 0 || html.indexOf('\\u25A0') >= 0 || html.indexOf('\u25A0') >= 0,
    'stop glyph');
  assert.ok(html.indexOf('__frontierEngagementStop') >= 0, 'mockable stop seam');
  assert.ok(html.indexOf('/api/agents/engagement/stop') >= 0 || html.indexOf('stopEngagementRequest') >= 0,
    'engagement stop path');
  assert.ok(html.indexOf('frontierAgentsCache') >= 0, 'agents cache for merge');
  // agents_changed reloads frontier engagement.
  const ac = html.indexOf("typ === 'agents_changed'");
  assert.ok(ac >= 0);
  const region = html.slice(ac, ac + 400);
  assert.ok(region.indexOf('loadFrontier') >= 0, 'agents_changed → loadFrontier: ' + region);
});

// 🎯T222: play on engaged / set_aside / achieved → blocked (no second agent).
test('playKickoff blocked when engaged or closed (🎯T222)', function () {
  assert.strictEqual(typeof FT.canPlayKickoff, 'function');

  const engaged = FT.playKickoffRequest({
    id: 'T221',
    name: 'Inspect user MD',
    status: 'identified',
    engaged: true,
    engaged_agents: ['jv-t221-inspect-user-md'],
  });
  assert.strictEqual(engaged.blocked, true);
  assert.strictEqual(engaged.reason, 'already_engaged');
  assert.ok(!engaged.body, 'no send body when blocked');
  assert.ok((engaged.message || '').indexOf('jv-t221-inspect-user-md') >= 0);

  const setAside = FT.playKickoffRequest({
    id: 'T220',
    name: 'Dup',
    status: 'set_aside',
  });
  assert.strictEqual(setAside.blocked, true);
  assert.strictEqual(setAside.reason, 'set_aside');

  const achieved = FT.canPlayKickoff({ id: 'T1', status: 'achieved' });
  assert.strictEqual(achieved.ok, false);
  assert.strictEqual(achieved.reason, 'achieved');

  // Free open target still produces kickoff.
  const free = FT.playKickoffRequest({
    id: 'T222',
    name: 'Dedupe filing',
    status: 'identified',
    engaged: false,
  });
  assert.strictEqual(free.blocked, false);
  assert.ok(free.body && free.body.text);
  assert.ok(free.body.text.indexOf('T222') >= 0);
  assert.ok(free.body.text.indexOf('do not spawn a second') >= 0 || free.body.text.indexOf('🎯T222') >= 0);

  // Force override residual.
  const forced = FT.playKickoffRequest({
    id: 'T221', engaged: true, engaged_agents: ['w'],
  }, { force: true });
  assert.strictEqual(forced.blocked, false);
  assert.ok(forced.body && forced.body.text);

  // index.html respects blocked.
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('req.blocked') >= 0 || html.indexOf('frontier_play_blocked') >= 0,
    'UI handles blocked kickoff');
});

// 🎯T253: Frontier tab follows selected agent workdir ledger.
test('T253 resolveFrontierCwd + frontierAPIURL pure', function () {
  assert.strictEqual(typeof FT.resolveFrontierCwd, 'function');
  assert.strictEqual(typeof FT.frontierAPIURL, 'function');

  const agents = [
    { name: 'jevons', purpose: 'overseer', workdir: '/Users/x/.jevons/jevons' },
    { name: 'jevons-po', purpose: 'work', workdir: '/Users/x/work/github.com/marcelocantos/jevons' },
    {
      name: 'yourworld2-po',
      purpose: 'work',
      workdir: '/Users/x/work/github.com/marcelocantos/yourworld2',
    },
    { name: 'no-ledger-po', purpose: 'work', workdir: '' },
    { name: 'orphan-worker', purpose: 'work' },
  ];

  // No selection / overseer / missing → empty cwd (server primary).
  assert.strictEqual(FT.resolveFrontierCwd(null, agents), '');
  assert.strictEqual(FT.resolveFrontierCwd('', agents), '');
  assert.strictEqual(FT.resolveFrontierCwd('jevons', agents), '');
  assert.strictEqual(FT.resolveFrontierCwd('unknown-agent', agents), '');
  assert.strictEqual(FT.resolveFrontierCwd('no-ledger-po', agents), '');
  assert.strictEqual(FT.resolveFrontierCwd('orphan-worker', agents), '');

  // PO / worker with workdir → that path.
  assert.strictEqual(
    FT.resolveFrontierCwd('jevons-po', agents),
    '/Users/x/work/github.com/marcelocantos/jevons');
  assert.strictEqual(
    FT.resolveFrontierCwd('yourworld2-po', agents),
    '/Users/x/work/github.com/marcelocantos/yourworld2');

  // Fixture: two ledgers — selection change switches cwd.
  const cwdA = FT.resolveFrontierCwd('jevons-po', agents);
  const cwdB = FT.resolveFrontierCwd('yourworld2-po', agents);
  assert.notStrictEqual(cwdA, cwdB);
  assert.strictEqual(FT.frontierAPIURL(FT.API_PATH, cwdA),
    '/api/frontier?cwd=' + encodeURIComponent(cwdA));
  assert.strictEqual(FT.frontierAPIURL(FT.API_PATH, cwdB),
    '/api/frontier?cwd=' + encodeURIComponent(cwdB));
  assert.strictEqual(FT.frontierAPIURL(FT.API_PATH, ''), '/api/frontier');
  assert.strictEqual(FT.frontierAPIURL(FT.GRAPH_API_PATH, cwdB),
    '/api/frontier/graph?cwd=' + encodeURIComponent(cwdB));
  // Spaces / special chars encoded.
  assert.strictEqual(
    FT.frontierAPIURL('/api/frontier', '/tmp/My Repo'),
    '/api/frontier?cwd=' + encodeURIComponent('/tmp/My Repo'));
});

test('T253 index.html loadFrontier/graph pass selected agent workdir', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const loadStart = html.indexOf('function loadFrontier');
  assert.ok(loadStart >= 0, 'loadFrontier defined');
  const loadEnd = html.indexOf('\nfunction ', loadStart + 10);
  const loadBody = html.slice(loadStart, loadEnd > loadStart ? loadEnd : loadStart + 2500);
  assert.ok(loadBody.indexOf('resolveFrontierCwd') >= 0, 'loadFrontier resolves cwd');
  assert.ok(loadBody.indexOf('frontierAPIURL') >= 0, 'loadFrontier builds API URL with cwd');
  assert.ok(loadBody.indexOf('selectedAgent') >= 0, 'loadFrontier reads selection');
  assert.ok(loadBody.indexOf('T253') >= 0, 'T253 marked on loadFrontier');

  const graphStart = html.indexOf('function openFrontierGraph');
  assert.ok(graphStart >= 0, 'openFrontierGraph defined');
  const graphEnd = html.indexOf('\nfunction ', graphStart + 10);
  const graphBody = html.slice(graphStart, graphEnd > graphStart ? graphEnd : graphStart + 2000);
  assert.ok(graphBody.indexOf('resolveFrontierCwd') >= 0, 'graph resolves cwd');
  assert.ok(graphBody.indexOf('frontierAPIURL') >= 0, 'graph builds URL with cwd');

  // Selection change rebinds frontier (PO switch / deselect / overseer).
  const selStart = html.indexOf('function selectAgent');
  assert.ok(selStart >= 0);
  const selEnd = html.indexOf('\nfunction ', selStart + 10);
  const selBody = html.slice(selStart, selEnd > selStart ? selEnd : selStart + 3500);
  assert.ok(selBody.indexOf('loadFrontier') >= 0, 'selectAgent reloads frontier');
  // Overseer path and deselect path both reload (primary cwd).
  assert.ok((selBody.match(/loadFrontier\s*\(/g) || []).length >= 2,
    'selectAgent reloads on overseer/deselect and agent select');
});

// 🎯T267: target-ask auto-selects owning PO + highlights Frontier row.
test('T267 extractTargetIDs + detectTargetAsk + planTargetAskFocus', function () {
  assert.strictEqual(typeof FT.extractTargetIDs, 'function');
  assert.strictEqual(typeof FT.detectTargetAsk, 'function');
  assert.strictEqual(typeof FT.resolveOwningPOForTarget, 'function');
  assert.strictEqual(typeof FT.rowMatchesHighlight, 'function');
  assert.strictEqual(typeof FT.planTargetAskFocus, 'function');

  assert.deepStrictEqual(FT.extractTargetIDs('Talk about 🎯T267 and 🎯T10.2 please'), ['T267', 'T10.2']);
  assert.deepStrictEqual(FT.extractTargetIDs('no targets here T1 bare'), []);
  assert.strictEqual(FT.detectTargetAsk('status: 🎯T267 is fine'), null);

  const marker = FT.detectTargetAsk('__TARGET_ASK__:T267\nShould we accept residual X?');
  assert.ok(marker, 'explicit marker detects');
  assert.strictEqual(marker.targetId, 'T267');
  assert.strictEqual(marker.po, '');

  const withPO = FT.detectTargetAsk('__TARGET_ASK__:T10.2|yourworld2-po\nDecide?');
  assert.strictEqual(withPO.targetId, 'T10.2');
  assert.strictEqual(withPO.po, 'yourworld2-po');

  const atPO = FT.detectTargetAsk('__TARGET_ASK__:T10.2@yourworld2-po');
  assert.strictEqual(atPO.po, 'yourworld2-po');

  const prose = FT.detectTargetAsk(
    'Needs-owner call on 🎯T262.4 — please decide whether to accept the packet.');
  assert.ok(prose, 'needs-owner prose detects');
  assert.strictEqual(prose.targetId, 'T262.4');

  const agents = [
    { name: 'jevons', purpose: 'overseer', workdir: '/Users/x/.jevons/jevons' },
    { name: 'jevons-po', purpose: 'work', workdir: '/Users/x/work/github.com/marcelocantos/jevons' },
    {
      name: 'yourworld2-po',
      purpose: 'work',
      workdir: '/Users/x/work/github.com/marcelocantos/yourworld2',
    },
    {
      name: 'yw2-worker',
      purpose: 'work',
      parent: 'yourworld2-po',
      target_id: 'T10.2',
      workdir: '/Users/x/work/github.com/marcelocantos/yourworld2',
    },
  ];

  // Fixture target-ask (marker) → default jevons-po when no engagement.
  const planA = FT.planTargetAskFocus({
    text: '__TARGET_ASK__:T267\nOwner: confirm residual for context chrome?',
    agents: agents,
  });
  assert.ok(planA, 'fixture plan');
  assert.strictEqual(planA.targetId, 'T267');
  assert.strictEqual(planA.highlightId, 'T267');
  assert.strictEqual(planA.po, 'jevons-po');
  assert.strictEqual(planA.tab, 'frontier');

  // Engaged worker on T10.2 → owning PO is yourworld2-po.
  const planB = FT.planTargetAskFocus({
    text: '__TARGET_ASK__:T10.2\nPlease confirm kickoff scope.',
    agents: agents,
  });
  assert.strictEqual(planB.po, 'yourworld2-po');
  assert.strictEqual(planB.highlightId, 'T10.2');

  // Marker preferred PO wins over engagement residual.
  const planC = FT.planTargetAskFocus({
    text: '__TARGET_ASK__:T10.2|jevons-po',
    agents: agents,
  });
  assert.strictEqual(planC.po, 'jevons-po');

  // Direct targetId fixture (smoke driver without prose).
  const planD = FT.planTargetAskFocus({ targetId: '🎯T267', agents: agents });
  assert.strictEqual(planD.targetId, 'T267');
  assert.strictEqual(planD.po, 'jevons-po');

  assert.strictEqual(FT.rowMatchesHighlight({ id: 'T267' }, 'T267'), true);
  assert.strictEqual(FT.rowMatchesHighlight({ id: 'T267' }, '🎯T267'), true);
  assert.strictEqual(FT.rowMatchesHighlight({ id: 'T10.2' }, 'T267'), false);
  assert.strictEqual(FT.rowMatchesHighlight(null, 'T267'), false);
});

test('T267 index.html wires focusTargetAsk + highlight class + seal path', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('function focusTargetAsk') >= 0, 'focusTargetAsk defined');
  assert.ok(html.indexOf('function maybeFocusTargetAsk') >= 0, 'maybeFocusTargetAsk defined');
  assert.ok(html.indexOf('frontierHighlightId') >= 0, 'highlight state');
  assert.ok(html.indexOf('ft-highlight') >= 0, 'highlight CSS/class');
  assert.ok(html.indexOf('data-frontier-highlight') >= 0, 'highlight data attr');
  assert.ok(html.indexOf('data-target-id') >= 0, 'row target id attr');
  assert.ok(html.indexOf('maybeFocusTargetAsk') >= 0, 'seal path can focus target ask');
  assert.ok(html.indexOf('window.focusTargetAsk') >= 0, 'smoke seam exposed');
  assert.ok(html.indexOf('T267') >= 0, 'T267 marker in product');

  // selectAgent accepts opts.tab so target-ask can land on Frontier (not only Transcript).
  const selStart = html.indexOf('function selectAgent');
  assert.ok(selStart >= 0);
  const selEnd = html.indexOf('\nfunction ', selStart + 10);
  const selBody = html.slice(selStart, selEnd > selStart ? selEnd : selStart + 4000);
  assert.ok(selBody.indexOf('opts.tab') >= 0, 'selectAgent honors tab preference for T267');
  // Default owner pick still lands on Transcript (T208 residual).
  assert.ok(
    /setRhsBottomTab\([\s\S]*?tabAfterAgentSelect\(true\)/.test(selBody) ||
      /setRhsBottomTab\([\s\S]*?['"]transcript['"]/.test(selBody),
    'selectAgent default still transcript on open inspect');

  // render applies highlight from frontierHighlightId.
  const renderStart = html.indexOf('function renderFrontierTable');
  assert.ok(renderStart >= 0);
  const renderEnd = html.indexOf('function loadFrontier', renderStart);
  const renderBody = html.slice(renderStart, renderEnd > renderStart ? renderEnd : renderStart + 8000);
  assert.ok(renderBody.indexOf('frontierHighlightId') >= 0, 'render reads highlight id');
  assert.ok(renderBody.indexOf('ft-highlight') >= 0, 'render paints highlight class');
  assert.ok(renderBody.indexOf('scrollIntoView') >= 0, 'highlight row scrolled into view');

  // focusTargetAsk selects PO + frontier tab.
  const focusStart = html.indexOf('function focusTargetAsk');
  const focusEnd = html.indexOf('\nfunction ', focusStart + 10);
  const focusBody = html.slice(focusStart, focusEnd > focusStart ? focusEnd : focusStart + 3500);
  assert.ok(focusBody.indexOf('planTargetAskFocus') >= 0, 'uses pure plan');
  assert.ok(focusBody.indexOf('selectAgent') >= 0, 'selects owning PO');
  assert.ok(focusBody.indexOf('frontier') >= 0, 'switches to frontier tab');
  assert.ok(focusBody.indexOf('loadFrontier') >= 0, 'reloads frontier after focus');
});

// 🎯T230: frontier re-render must not kill tips while pointer is over card.
test('T230 skip re-render while InstantTip hover latched', function () {
  assert.strictEqual(typeof FT.shouldSkipRerenderWhileTipLatched, 'function');
  assert.strictEqual(FT.shouldSkipRerenderWhileTipLatched(true), true);
  assert.strictEqual(FT.shouldSkipRerenderWhileTipLatched(false), false);
  assert.strictEqual(FT.shouldSkipRerenderWhileTipLatched(0), false);
  assert.strictEqual(FT.shouldSkipRerenderWhileTipLatched(1), true);

  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const start = html.indexOf('function renderFrontierTable');
  assert.ok(start >= 0);
  const end = html.indexOf('function loadFrontier', start);
  const region = html.slice(start, end > start ? end : start + 5000);
  assert.ok(region.indexOf('anyHoverLatched') >= 0, 'anyHoverLatched gate in render');
  assert.ok(region.indexOf('T230') >= 0, 'T230 marked');
  assert.ok(region.indexOf('discardDetachedTips') >= 0, 'safe tip cleanup');
  // Old bug: unconditional removeChild of every .instant-tip on every poll.
  // Must gate on latch first (early return), then discardDetachedTips.
  const latchIdx = region.indexOf('anyHoverLatched');
  const discardIdx = region.indexOf('discardDetachedTips');
  assert.ok(latchIdx >= 0 && discardIdx >= 0 && latchIdx < discardIdx,
    'latch check before discardDetachedTips wipe');
  assert.ok(/anyHoverLatched\(\)\s*\)\s*\{\s*return;/.test(region)
    || /anyHoverLatched\(\)[\s\S]{0,80}return;/.test(region),
    'early return when latched');
});


if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('All frontier_table tests passed');
