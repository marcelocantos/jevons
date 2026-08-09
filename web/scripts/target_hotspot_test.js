// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic oracle for 🎯T326: target id hotspots + shared frontier-card path.
// Run: node web/scripts/target_hotspot_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');

const TH = require('./target_hotspot.js');
const FT = require('./frontier_table.js');
const IT = require('./instant_tip.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('ok  -', name);
  } catch (e) {
    failed++;
    console.error('FAIL-', name);
    console.error('    ', e && e.stack ? e.stack.split('\n').slice(0, 6).join('\n     ') : e);
  }
}

test('formatDisplayTargetID always 🎯 prefix', function () {
  assert.strictEqual(TH.formatDisplayTargetID('T326'), '🎯T326');
  assert.strictEqual(TH.formatDisplayTargetID('🎯T27.2'), '🎯T27.2');
  assert.strictEqual(TH.formatDisplayTargetID(''), '');
  assert.strictEqual(TH.normalizeTargetID('🎯T10.2'), 'T10.2');
});

test('linkifyTargetText wraps 🎯Tn and bare Tn as hotspots', function () {
  const a = TH.linkifyTargetText('Landed 🎯T326 today');
  assert.ok(a.indexOf('class="target-hotspot') >= 0, a);
  assert.ok(a.indexOf('data-target-id="T326"') >= 0, a);
  assert.ok(a.indexOf('🎯T326') >= 0, a);
  assert.ok(a.indexOf('target-hotspot-finger') >= 0, 'finger class on host');

  const b = TH.linkifyTargetText('See T305 and T31.1');
  assert.ok(b.indexOf('data-target-id="T305"') >= 0, b);
  assert.ok(b.indexOf('data-target-id="T31.1"') >= 0, b);
  // Display form always has emoji even when source was bare.
  assert.ok(/🎯T305/.test(b), b);
});

test('linkify skips code/pre/a; idempotent on existing hotspots', function () {
  const code = TH.linkifyTargetIDsInHTML('<p>ok</p><code>T326</code><pre>🎯T1</pre>');
  assert.ok(code.indexOf('<code>T326</code>') >= 0, 'code untouched: ' + code);
  assert.ok(code.indexOf('<pre>🎯T1</pre>') >= 0, 'pre untouched: ' + code);

  const once = TH.linkifyTargetIDsInHTML('<p>🎯T326 landed</p>');
  const twice = TH.linkifyTargetIDsInHTML(once);
  assert.strictEqual(once, twice, 'idempotent');

  const anchor = TH.linkifyTargetIDsInHTML('<a href="x">T326</a>');
  assert.ok(anchor.indexOf('target-hotspot') < 0, 'anchors not linkified: ' + anchor);
});

test('findRowByTargetID + cardMarkdown uses FrontierTable shared path', function () {
  const rows = [
    {
      id: 'T326',
      name: 'Target hotspots',
      status: 'identified',
      acceptance: ['shared card'],
      context: 'Hover chrome',
    },
  ];
  const row = TH.findRowByTargetID(rows, '🎯T326');
  assert.ok(row, 'row found');
  const md = TH.cardMarkdownForRow(row, FT.formatTargetCardMarkdown);
  assert.ok(md.indexOf('🎯T326') >= 0, md);
  assert.ok(md.indexOf('Target hotspots') >= 0, md);
  assert.ok(md.indexOf('Acceptance') >= 0, md);
  // Same builder frontier table uses.
  assert.strictEqual(md, FT.formatTargetCardMarkdown(row));
});

test('hotspotCardOpts: right-of-host, finger left, card right', function () {
  const opts = TH.hotspotCardOpts();
  assert.strictEqual(opts.placement, 'right-of-host');
  assert.ok(opts.className.indexOf('instant-tip-card') >= 0, opts.className);
  assert.ok(opts.className.indexOf('target-card-tip') >= 0, opts.className);
  assert.strictEqual(opts.sticky, true);
  assert.strictEqual(opts.html, true);

  const pathSpec = TH.sharedCardRenderPath();
  assert.strictEqual(pathSpec.markdownBuilder, 'FrontierTable.formatTargetCardMarkdown');
  assert.strictEqual(pathSpec.tipAttach, 'InstantTip.attach');
  assert.strictEqual(pathSpec.placement, 'right-of-host');
  assert.strictEqual(pathSpec.fingerLeftOfCard, true);
  assert.strictEqual(pathSpec.cardOpensRightOfHotspot, true);
});

test('InstantTip placeRightOfHostRect: card right of host (finger left)', function () {
  assert.ok(IT.PLACE_RIGHT_OF_HOST === 'right-of-host', IT.PLACE_RIGHT_OF_HOST);
  assert.strictEqual(typeof IT.placeRightOfHostRect, 'function');

  const pos = IT.placeRightOfHostRect({
    hostRect: { left: 100, top: 200, right: 140, bottom: 220 },
    tipW: 300,
    tipH: 120,
    viewW: 1200,
    viewH: 800,
    gap: 12,
  });
  // Card starts to the right of host.right + gap.
  assert.ok(pos.left >= 140 + 12 - 1, 'card left >= host right + gap: ' + pos.left);
  assert.strictEqual(pos.side, 'right');
  assert.strictEqual(pos.fingerLeftOfCard, true);
  assert.strictEqual(pos.cardOpensRightOfHotspot, true);

  // Near right edge → may flip left.
  const edge = IT.placeRightOfHostRect({
    hostRect: { left: 900, top: 100, right: 940, bottom: 120 },
    tipW: 400,
    tipH: 100,
    viewW: 1000,
    viewH: 600,
    gap: 8,
  });
  assert.ok(edge.side === 'left' || edge.left + 400 <= 1000, 'viewport clamp: ' + JSON.stringify(edge));
});

test('index.html wires TargetHotspot + shared formatTargetCardMarkdown + right-of-host', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/target_hotspot.js') >= 0, 'script tag');
  assert.ok(html.indexOf('TargetHotspot.linkifyTargetIDsInHTML') >= 0, 'linkify in parse');
  assert.ok(html.indexOf('decorateTargetHotspots') >= 0, 'decorate wired');
  assert.ok(html.indexOf('formatTargetCardMarkdown') >= 0, 'shared card builder');
  assert.ok(
    html.indexOf('PLACE_RIGHT_OF_HOST') >= 0 || html.indexOf('right-of-host') >= 0,
    'right-of-host placement'
  );
  assert.ok(html.indexOf('target-hotspot') >= 0, 'hotspot CSS/class');
  assert.ok(html.indexOf('target-card-tip') >= 0, 'card tip class');
  // Finger is smaller + left of card chrome.
  assert.ok(html.indexOf('target-hotspot-finger') >= 0 || html.indexOf('0.92em') >= 0,
    'smaller finger styling');
  // Must not invent a forked card formatter in index.html.
  assert.ok(!/function\s+formatChatTargetCard/.test(html), 'no forked card builder');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('ok - target_hotspot_test (🎯T326)');
