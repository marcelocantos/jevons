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
  assert.ok(region.indexOf('InstantTip.attach(name') >= 0 || /InstantTip\.attach\(\s*name/.test(region),
    'attach on name cell');
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
// 🎯T190: layout init, flowchart TB, island packing (not graph TD LR strip).
test('buildActiveDependencyMermaid from unachieved set (🎯T185/T190)', function () {
  assert.strictEqual(FT.GRAPH_API_PATH, '/api/frontier/graph');
  const src = FT.buildActiveDependencyMermaid([
    { id: 'T2', name: 'Ready leaf', depends_on: [{ id: 'T1', name: 'Done' }] },
    { id: 'T3', name: 'Blocked', depends_on: [{ id: 'T2' }] },
    { id: 'T3.1', name: 'Nested', depends_on: ['T3', 'T1'] },
    { id: 'T4', name: 'Orphan', depends_on: [] },
  ]);
  // Layout directives (🎯T190).
  assert.ok(src.indexOf("%%{init:") === 0, 'starts with mermaid init: ' + src.slice(0, 60));
  assert.ok(src.indexOf("useMaxWidth': true") >= 0 || src.indexOf('useMaxWidth\': true') >= 0
    || /useMaxWidth['"]?\s*:\s*true/.test(src), 'useMaxWidth: ' + src.slice(0, 120));
  assert.ok(/nodeSpacing['"]?\s*:\s*\d+/.test(src), 'nodeSpacing');
  assert.ok(/rankSpacing['"]?\s*:\s*\d+/.test(src), 'rankSpacing');
  assert.ok(/wrappingWidth['"]?\s*:\s*\d+/.test(src), 'wrappingWidth');
  assert.ok(src.indexOf('flowchart TB') >= 0, 'flowchart TB');
  assert.ok(src.indexOf('graph LR') < 0 || src.indexOf('flowchart TB') < src.indexOf('graph LR'),
    'primary direction is TB not LR');
  assert.ok(src.indexOf('T2[') >= 0 && src.indexOf('T3[') >= 0 && src.indexOf('T3_1[') >= 0, 'nodes');
  assert.ok(src.indexOf('T4[') >= 0, 'orphan node');
  // Edge to T1 dropped (T1 not in unachieved set).
  assert.ok(src.indexOf('T1[') < 0, 'no T1 node');
  assert.ok(src.indexOf('|needs| T1') < 0, 'no edge to T1');
  assert.ok(src.indexOf('T3 -.->|needs| T2') >= 0, 'T3→T2: ' + src);
  assert.ok(src.indexOf('T3_1 -.->|needs| T3') >= 0, 'T3.1→T3');
  // Island packing: connected {T2,T3,T3.1} vs orphan T4 → subgraphs + ~~~.
  assert.ok(src.indexOf('subgraph island_') >= 0, 'island subgraphs: ' + src);
  assert.ok(src.indexOf('direction TB') >= 0, 'subgraph direction TB');
  assert.ok(src.indexOf('island_0 ~~~ island_1') >= 0, 'vertical packing link: ' + src);
  assert.ok(/linkStyle .+stroke:none/.test(src), 'packing links hidden');
  // Empty set still valid Mermaid with layout header.
  const empty = FT.buildActiveDependencyMermaid([]);
  assert.ok(empty.indexOf('flowchart TB') >= 0, empty);
  assert.ok(empty.indexOf('useMaxWidth') >= 0, 'empty still has init');
});

test('packActiveGraphIslands stacks components (🎯T190)', function () {
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

test('buildActiveDependencyMermaid id order natural (🎯T199)', function () {
  // Nodes should appear in natural order within islands (T2 before T10).
  const src = FT.buildActiveDependencyMermaid([
    { id: 'T10', name: 'Ten' },
    { id: 'T2', name: 'Two', depends_on: [{ id: 'T10' }] },
    { id: 'T100', name: 'Hundred' },
  ]);
  const i2 = src.indexOf('T2[');
  const i10 = src.indexOf('T10[');
  const i100 = src.indexOf('T100[');
  assert.ok(i2 >= 0 && i10 >= 0 && i100 >= 0, src);
  assert.ok(i2 < i10, 'T2 before T10 in mermaid: ' + src);
  // T2-T10 island before orphan T100 by first id natural order.
  assert.ok(i10 < i100 || src.indexOf('island_0') < src.indexOf('island_1'), src);
});

test('normalizeGraphPayload (🎯T185)', function () {
  const ok = FT.normalizeGraphPayload({
    available: true,
    mermaid: 'flowchart TB\n  A --> B\n',
    node_count: 2,
    edge_count: 1,
    ledger: '/x/bullseye.yaml',
  });
  assert.strictEqual(ok.available, true);
  assert.ok(ok.mermaid.indexOf('flowchart TB') >= 0);
  assert.strictEqual(ok.nodeCount, 2);
  assert.strictEqual(ok.edgeCount, 1);
  const bad = FT.normalizeGraphPayload(null, new Error('boom'));
  assert.strictEqual(bad.available, false);
  assert.ok(/boom/.test(bad.error));
});

test('T185/T190 index.html Graph control + large panel + layout CSS', function () {
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
  const ofg = html.slice(ofgStart, ofgEnd > ofgStart ? ofgEnd : ofgStart + 4000);
  const catchIdx = ofg.indexOf('.catch(');
  assert.ok(catchIdx >= 0, 'openFrontierGraph has catch');
  assert.ok(ofg.slice(catchIdx).indexOf('renderMermaidPanelEmpty') < 0,
    'T196: catch must not use empty paste shell');
  assert.ok(ofg.slice(catchIdx).indexOf('renderMermaidPanelFetchError') >= 0,
    'T196: catch uses fetch-error path');
  // Pure module exports.
  assert.strictEqual(typeof FT.buildActiveDependencyMermaid, 'function');
  assert.strictEqual(typeof FT.normalizeGraphPayload, 'function');
  assert.strictEqual(typeof FT.packActiveGraphIslands, 'function');
  assert.strictEqual(typeof FT.mermaidActiveGraphHeader, 'function');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('All frontier_table tests passed');
