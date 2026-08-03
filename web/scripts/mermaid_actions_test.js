// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for mermaid open/copy helpers (🎯T83.1)
// and durable panel pin/persist (🎯T83).
// Run: node web/scripts/mermaid_actions_test.js

'use strict';

const assert = require('assert');
const MA = require('./mermaid_actions.js');

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

test('toolbar has Open, Copy source, Copy image', function () {
  const labels = MA.toolbarButtons().map(function (b) { return b.label; });
  assert.deepStrictEqual(labels, ['Open', 'Copy source', 'Copy image']);
  assert.strictEqual(MA.toolbarButtons()[0].action, 'open');
  assert.strictEqual(MA.toolbarButtons()[1].action, 'copy-source');
  assert.strictEqual(MA.toolbarButtons()[2].action, 'copy-image');
});

test('clipboardCapabilities multiType needs write + ClipboardItem + secure', function () {
  const full = MA.clipboardCapabilities({
    isSecureContext: true,
    clipboard: { write: function () {}, writeText: function () {} },
    ClipboardItem: function () {},
  });
  assert.strictEqual(full.multiType, true);
  assert.strictEqual(full.write, true);
  assert.strictEqual(full.writeText, true);

  const noItem = MA.clipboardCapabilities({
    isSecureContext: true,
    clipboard: { write: function () {}, writeText: function () {} },
  });
  assert.strictEqual(noItem.multiType, false);
  assert.strictEqual(noItem.write, true);

  const insecure = MA.clipboardCapabilities({
    isSecureContext: false,
    clipboard: { write: function () {}, writeText: function () {} },
    ClipboardItem: function () {},
  });
  assert.strictEqual(insecure.multiType, false);
  assert.strictEqual(insecure.write, false);
});

test('clipboardWritePlan multi when png + multiType', function () {
  const caps = {
    secure: true, write: true, writeText: true, multiType: true,
  };
  const plan = MA.clipboardWritePlan({
    mermaidSrc: 'graph TD; A-->B;',
    pngBlob: { type: 'image/png' },
    caps: caps,
  });
  assert.strictEqual(plan.mode, 'multi');
  assert.deepStrictEqual(plan.types, ['image/png', 'text/plain']);
  assert.strictEqual(plan.text, 'graph TD; A-->B;');
});

test('clipboardWritePlan image-only when multi unavailable but write ok', function () {
  const plan = MA.clipboardWritePlan({
    mermaidSrc: 'graph TD; A-->B;',
    pngBlob: { type: 'image/png' },
    caps: { secure: true, write: true, writeText: true, multiType: false },
  });
  assert.strictEqual(plan.mode, 'image');
  assert.deepStrictEqual(plan.types, ['image/png']);
});

test('clipboardWritePlan text fallback without png', function () {
  const plan = MA.clipboardWritePlan({
    mermaidSrc: 'flowchart LR; X-->Y;',
    caps: { secure: true, write: false, writeText: true, multiType: false },
  });
  assert.strictEqual(plan.mode, 'text');
  assert.strictEqual(plan.text, 'flowchart LR; X-->Y;');
});

test('clipboardWritePlan unavailable when no clipboard', function () {
  const plan = MA.clipboardWritePlan({
    mermaidSrc: 'x',
    caps: { secure: false, write: false, writeText: false, multiType: false },
  });
  assert.strictEqual(plan.mode, 'unavailable');
  assert.ok(/secure context/i.test(plan.reason));
});

test('buildOpenDocumentHtml embeds svg and escaped source', function () {
  const html = MA.buildOpenDocumentHtml({
    title: 'Test <graph>',
    svgMarkup: '<svg><text>A</text></svg>',
    mermaidSrc: 'graph TD; A-->B;\n<script>alert(1)</script>',
    dark: true,
  });
  assert.ok(html.indexOf('<main class="mermaid-open-host"><svg><text>A</text></svg></main>') >= 0);
  assert.ok(html.indexOf('Test &lt;graph&gt;') >= 0);
  assert.ok(html.indexOf('graph TD; A--&gt;B;') >= 0);
  assert.ok(html.indexOf('&lt;script&gt;alert(1)&lt;/script&gt;') >= 0);
  assert.ok(html.indexOf('mermaid-open-source') >= 0);
  // Source must not inject raw script tags into the open document.
  assert.ok(!/<script>alert\(1\)<\/script>/.test(html));
});

test('normalizeSvgMarkup adds xmlns when missing', function () {
  const n = MA.normalizeSvgMarkup('<svg width="10"><circle/></svg>');
  assert.ok(n.indexOf('xmlns="http://www.w3.org/2000/svg"') >= 0);
  const already = MA.normalizeSvgMarkup('<svg xmlns="http://www.w3.org/2000/svg"></svg>');
  assert.strictEqual((already.match(/xmlns=/g) || []).length, 1);
});

test('svgMarkupToDataUrl encodes markup', function () {
  const url = MA.svgMarkupToDataUrl('<svg><text>hi</text></svg>');
  assert.ok(url.indexOf('data:image/svg+xml') === 0);
  assert.ok(url.indexOf(encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg"><text>hi</text></svg>')) >= 0 ||
    url.indexOf('text') >= 0);
});

test('openPlacement defaults to panel; preferWindow uses window', function () {
  assert.strictEqual(MA.openPlacement({}), 'panel');
  assert.strictEqual(MA.openPlacement({ preferWindow: true }), 'window');
});

test('panelChromeButtons includes load-last, paste, pin, close', function () {
  const actions = MA.panelChromeButtons().map(function (b) { return b.action; });
  assert.ok(actions.indexOf('load-last') >= 0);
  assert.ok(actions.indexOf('paste') >= 0);
  assert.ok(actions.indexOf('render') >= 0);
  assert.ok(actions.indexOf('pin') >= 0);
  assert.ok(actions.indexOf('close') >= 0);
});

test('stripMermaidFence unwraps fenced and raw source', function () {
  assert.strictEqual(MA.stripMermaidFence('graph TD; A-->B;'), 'graph TD; A-->B;');
  assert.strictEqual(
    MA.stripMermaidFence('```mermaid\ngraph TD; A-->B;\n```'),
    'graph TD; A-->B;'
  );
  assert.strictEqual(
    MA.stripMermaidFence('```\nflowchart LR; X-->Y;\n```'),
    'flowchart LR; X-->Y;'
  );
  assert.strictEqual(MA.stripMermaidFence('  \n  '), '');
  assert.strictEqual(MA.isRenderableSource('```mermaid\nA\n```'), true);
  assert.strictEqual(MA.isRenderableSource('   '), false);
});

function memStorage(seed) {
  const map = Object.create(null);
  if (seed) {
    Object.keys(seed).forEach(function (k) { map[k] = seed[k]; });
  }
  return {
    getItem: function (k) { return Object.prototype.hasOwnProperty.call(map, k) ? map[k] : null; },
    setItem: function (k, v) { map[k] = String(v); },
    removeItem: function (k) { delete map[k]; },
    _map: map,
  };
}

test('savePinnedGraph / loadPinnedGraph round-trip and clear', function () {
  const store = memStorage();
  assert.strictEqual(MA.loadPinnedGraph(store), null);
  const saved = MA.savePinnedGraph(store, {
    src: '```mermaid\ngraph TD; A-->B;\n```',
    svgMarkup: '<svg><text>A</text></svg>',
    title: 'Bullseye active',
  });
  assert.ok(saved);
  assert.strictEqual(saved.src, 'graph TD; A-->B;');
  assert.strictEqual(saved.title, 'Bullseye active');
  assert.strictEqual(saved.version, 1);
  const loaded = MA.loadPinnedGraph(store);
  assert.ok(loaded);
  assert.strictEqual(loaded.src, 'graph TD; A-->B;');
  assert.ok(loaded.svgMarkup.indexOf('<svg>') >= 0);
  assert.ok(typeof loaded.updatedAt === 'number');
  MA.clearPinnedGraph(store);
  assert.strictEqual(MA.loadPinnedGraph(store), null);
});

test('loadPinnedGraph ignores corrupt JSON and empty payload', function () {
  const bad = memStorage();
  bad.setItem(MA.PIN_STORAGE_KEY, '{not json');
  assert.strictEqual(MA.loadPinnedGraph(bad), null);
  assert.strictEqual(MA.savePinnedGraph(memStorage(), { src: '', svgMarkup: '' }), null);
  assert.strictEqual(MA.normalizePinnedGraph(null), null);
  assert.strictEqual(MA.normalizePinnedGraph({ src: '  ' }), null);
});

test('openFromChromePlan returns empty or pinned', function () {
  const empty = MA.openFromChromePlan(memStorage());
  assert.strictEqual(empty.mode, 'empty');
  const store = memStorage();
  MA.savePinnedGraph(store, { src: 'graph TD; A-->B;' });
  const plan = MA.openFromChromePlan(store);
  assert.strictEqual(plan.mode, 'pinned');
  assert.strictEqual(plan.pin.src, 'graph TD; A-->B;');
});

test('emptyStateHtml is durable chrome empty copy', function () {
  const html = MA.emptyStateHtml();
  assert.ok(html.indexOf('data-mvp-empty') >= 0);
  assert.ok(/Load last/i.test(html));
  assert.ok(/Paste/i.test(html));
  assert.ok(/bullseye/i.test(html));
});

// 🎯T189: Escape closes open #mermaid-viz-panel (incl. mvp-large); ignore when closed.
function mockPanel(opts) {
  const o = opts || {};
  const classes = Object.create(null);
  (o.classes || []).forEach(function (c) { classes[c] = true; });
  return {
    hidden: !!o.hidden,
    classList: {
      contains: function (c) { return !!classes[c]; },
    },
  };
}

test('isMermaidPanelOpen true only when open class and not hidden', function () {
  assert.strictEqual(MA.isMermaidPanelOpen(null), false);
  assert.strictEqual(MA.isMermaidPanelOpen(mockPanel({ hidden: true, classes: ['open'] })), false);
  assert.strictEqual(MA.isMermaidPanelOpen(mockPanel({ hidden: false, classes: [] })), false);
  assert.strictEqual(MA.isMermaidPanelOpen(mockPanel({ hidden: false, classes: ['open'] })), true);
  // mvp-large Graph overlay still "open".
  assert.strictEqual(
    MA.isMermaidPanelOpen(mockPanel({ hidden: false, classes: ['open', 'mvp-large'] })),
    true
  );
});

test('shouldCloseMermaidOnEscape only for Escape when panel open', function () {
  const open = mockPanel({ hidden: false, classes: ['open', 'mvp-large'] });
  const closed = mockPanel({ hidden: true, classes: [] });
  assert.strictEqual(MA.shouldCloseMermaidOnEscape('Escape', open), true);
  assert.strictEqual(MA.shouldCloseMermaidOnEscape('Escape', closed), false);
  assert.strictEqual(MA.shouldCloseMermaidOnEscape('Escape', null), false);
  assert.strictEqual(MA.shouldCloseMermaidOnEscape('Enter', open), false);
  assert.strictEqual(MA.shouldCloseMermaidOnEscape('Esc', open), false);
});

test('T189 index.html Escape wiring calls closeMermaidPanel when open', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('shouldCloseMermaidOnEscape') >= 0, 'policy helper used');
  assert.ok(html.indexOf('mermaid-viz-panel') >= 0, 'panel id');
  // Escape branch closes panel then returns (does not always steal interrupt).
  assert.ok(/if\s*\(\s*closePanel\s*\)\s*\{[\s\S]*?closeMermaidPanel\(\)/.test(html),
    'closePanel branch invokes closeMermaidPanel');
  assert.ok(html.indexOf('function closeMermaidPanel') >= 0, 'closeMermaidPanel defined');
  // Interrupt path still present for when panel is closed.
  assert.ok(html.indexOf('{"type":"interrupt"}') >= 0, 'interrupt retained when panel closed');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('all mermaid_actions tests passed');
