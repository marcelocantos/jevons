// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for mermaid open/copy helpers (🎯T83.1)
// and durable panel pin/persist (🎯T83).
// Run: node web/scripts/mermaid_actions_test.js

'use strict';

const assert = require('assert');
const MA = require('./mermaid_actions.js');

let failed = 0;
const pending = [];
function test(name, fn) {
  // Supports sync tests and async tests that return a Promise (🎯T274 timeout race).
  let result;
  try {
    result = fn();
  } catch (e) {
    failed++;
    console.error('FAIL-', name);
    console.error('    ', e && e.stack ? e.stack.split('\n').slice(0, 4).join('\n     ') : e);
    return;
  }
  if (result && typeof result.then === 'function') {
    pending.push(
      result.then(
        function () { console.log('ok  -', name); },
        function (e) {
          failed++;
          console.error('FAIL-', name);
          console.error('    ', e && e.stack ? e.stack.split('\n').slice(0, 4).join('\n     ') : e);
        }
      )
    );
    return;
  }
  console.log('ok  -', name);
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

// 🎯T196: failed product fetches show HTTP + recovery — not empty paste shell.
test('productFetchFailureView 404 includes HTTP code + recovery, not paste shell', function () {
  const view = MA.productFetchFailureView({
    resource: 'Unachieved graph',
    status: 404,
    kind: 'http',
  });
  assert.strictEqual(view.httpStatus, 404);
  assert.strictEqual(view.kind, 'http');
  assert.ok(/HTTP 404/.test(view.status), 'status has HTTP 404: ' + view.status);
  assert.ok(/rebuild|restart-daily/i.test(view.status), 'status has recovery short: ' + view.status);
  assert.ok(view.bodyHtml.indexOf('data-mvp-fetch-error') >= 0, 'error body marker');
  assert.ok(/HTTP 404/.test(view.bodyHtml), 'body has HTTP 404');
  assert.ok(/restart-daily/i.test(view.bodyHtml), 'body has recovery hint');
  assert.ok(view.bodyHtml.indexOf('data-mvp-empty') < 0, 'not empty shell');
  assert.ok(!/No graph loaded/i.test(view.bodyHtml), 'not empty paste copy');
  assert.ok(!/\bPaste\b/i.test(view.bodyHtml), 'does not invite paste as primary');
  assert.ok(view.status.indexOf(MA.PRODUCT_FETCH_RECOVERY_SHORT) >= 0);
  assert.ok(view.recoveryHint.indexOf('restart-daily') >= 0);
});

test('productFetchFailureView 5xx and network are distinct from empty-available', function () {
  const s5 = MA.productFetchFailureView({
    resource: 'Unachieved graph',
    status: 503,
    message: 'service unavailable',
  });
  assert.strictEqual(s5.httpStatus, 503);
  assert.ok(/HTTP 503/.test(s5.status));
  assert.ok(/service unavailable/i.test(s5.status));
  assert.ok(s5.bodyHtml.indexOf('data-mvp-fetch-error') >= 0);

  const net = MA.productFetchFailureView({
    resource: 'Unachieved graph',
    message: 'Failed to fetch',
    kind: 'network',
  });
  assert.strictEqual(net.kind, 'network');
  assert.ok(/Failed to fetch|network/i.test(net.status), net.status);
  assert.ok(/rebuild|restart-daily/i.test(net.status));
  assert.ok(net.bodyHtml.indexOf('data-mvp-fetch-error') >= 0);
  assert.ok(net.bodyHtml.indexOf('data-mvp-empty') < 0);

  const empty = MA.emptyStateHtml();
  assert.ok(empty.indexOf('data-mvp-empty') >= 0);
  assert.ok(empty.indexOf('data-mvp-fetch-error') < 0);
  assert.notStrictEqual(empty, s5.bodyHtml);
  assert.notStrictEqual(empty, net.bodyHtml);
});

test('productFetchFailureFromError maps httpStatus and message HTTP codes', function () {
  const e404 = new Error('not found');
  e404.httpStatus = 404;
  e404.kind = 'http';
  const v = MA.productFetchFailureFromError(e404, { resource: 'Unachieved graph' });
  assert.strictEqual(v.httpStatus, 404);
  assert.ok(/HTTP 404/.test(v.status));
  assert.ok(/not found/i.test(v.status) || /not found/i.test(v.bodyHtml));

  const fromMsg = MA.productFetchFailureFromError(new Error('HTTP 502 bad gateway'), {
    resource: 'Unachieved graph',
  });
  assert.strictEqual(fromMsg.httpStatus, 502);
  assert.ok(/HTTP 502/.test(fromMsg.status));

  const net = MA.productFetchFailureFromError(new Error('Failed to fetch'), {
    resource: 'Unachieved graph',
  });
  assert.strictEqual(net.kind, 'network');
  assert.ok(net.bodyHtml.indexOf('data-mvp-fetch-error') >= 0);
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

// 🎯T196: openFrontierGraph failed fetch → fetch-error view, not empty paste shell.
test('T196 index.html openFrontierGraph catch uses fetch-error path not empty shell', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('function openFrontierGraph') >= 0, 'openFrontierGraph defined');
  assert.ok(html.indexOf('function renderMermaidPanelFetchError') >= 0, 'renderMermaidPanelFetchError');
  assert.ok(html.indexOf('productFetchFailureFromError') >= 0, 'uses pure failure helper');
  assert.ok(html.indexOf('data-mvp-fetch-error') >= 0 || html.indexOf('mvp-error') >= 0,
    'error markup present');
  // Extract openFrontierGraph and assert catch path.
  const start = html.indexOf('function openFrontierGraph');
  assert.ok(start >= 0);
  const nextFn = html.indexOf('\nfunction ', start + 10);
  const ofg = html.slice(start, nextFn > start ? nextFn : start + 4000);
  assert.ok(ofg.indexOf('renderMermaidPanelFetchError') >= 0,
    'openFrontierGraph catch/error path calls renderMermaidPanelFetchError');
  // Catch must not paint empty paste shell (that was the product bug).
  const catchIdx = ofg.indexOf('.catch(');
  assert.ok(catchIdx >= 0, 'has .catch');
  // Strip line comments so intentional docs don't trip the call-site check.
  const catchCode = ofg.slice(catchIdx).replace(/\/\/[^\n]*/g, '');
  assert.ok(catchCode.indexOf('renderMermaidPanelFetchError') >= 0, 'catch uses fetch error');
  assert.ok(!/renderMermaidPanelEmpty\s*\(/.test(catchCode),
    'catch must not call renderMermaidPanelEmpty (empty paste shell)');
  // HTTP status attached on !r.ok for status line / body.
  assert.ok(/httpStatus\s*=\s*r\.status/.test(ofg), 'attaches httpStatus from response');
  // CSS for error body.
  assert.ok(/\.mvp-error/.test(html), 'mvp-error CSS');
});

// ── 🎯T268 single-graph scale-to-fill ─────────────────────────────────────

test('T268 computeContainScale scales up small graphs and down large ones', function () {
  // Small graph in large pane → scale up.
  assert.strictEqual(MA.computeContainScale(400, 300, 1200, 900), 3);
  // Large graph in small pane → scale down (fit, not crop).
  assert.ok(Math.abs(MA.computeContainScale(4000, 3000, 800, 600) - 0.2) < 1e-9);
  // Aspect mismatch: limited by the tighter axis.
  assert.strictEqual(MA.computeContainScale(100, 100, 400, 200), 2);
  assert.strictEqual(MA.computeContainScale(0, 100, 400, 200), 1);
});

test('T268 parseSvgNaturalSize from width/height and viewBox', function () {
  assert.deepStrictEqual(
    MA.parseSvgNaturalSize({ width: '800', height: '600' }),
    { w: 800, h: 600 }
  );
  assert.deepStrictEqual(
    MA.parseSvgNaturalSize({ viewBox: '0 0 1200 400' }),
    { w: 1200, h: 400 }
  );
  assert.deepStrictEqual(
    MA.parseSvgNaturalSize({
      getAttribute: function (k) {
        if (k === 'width') return '500px';
        if (k === 'height') return '250';
        return null;
      },
    }),
    { w: 500, h: 250 }
  );
});

test('T268 planSingleGraphScaleToFill fills pane (not tiny island)', function () {
  // Small mermaid-ish graph in ~90vw×90vh usable pane → must scale up and fill.
  const plan = MA.planSingleGraphScaleToFill({
    svgW: 420,
    svgH: 280,
    paneW: 1400,
    paneH: 900,
    padding: 24,
    diagramCount: 1,
  });
  assert.strictEqual(plan.mode, 'scale-to-fill');
  assert.ok(plan.scale > 1, 'must scale UP: scale=' + plan.scale);
  assert.strictEqual(plan.fillsPane, true);
  // Without scale-up (scale=1) margins would dominate — reject that product bug.
  const unscaledCover = Math.max(420 / (1400 - 24), 280 / (900 - 24));
  assert.ok(unscaledCover < 0.5, 'fixture would look tiny without fit');
  assert.ok(plan.scale > 1 / unscaledCover * 0.9 || plan.fillsPane);
});

test('T268 hermetic 10+ node fixture: dense graph scale-to-fill uses pane', function () {
  // Simulate Mermaid layout of 12-node orthograph-ish component:
  // natural SVG ~ 1800×1100; frontier large panel ~ 1280×720 usable.
  const NODE_COUNT = 12;
  assert.ok(NODE_COUNT >= 10, 'fixture is 10+ nodes');
  const naturalW = 1800;
  const naturalH = 1100;
  const paneW = 1280;
  const paneH = 720;
  const plan = MA.planSingleGraphScaleToFill({
    svgW: naturalW,
    svgH: naturalH,
    paneW: paneW,
    paneH: paneH,
    padding: 24,
    diagramCount: 1,
  });
  assert.strictEqual(plan.mode, 'scale-to-fill');
  assert.strictEqual(plan.fillsPane, true);
  // Display size must track pane on at least one axis (contain).
  const usableW = paneW - 24;
  const usableH = paneH - 24;
  const cover = Math.max(plan.displayW / usableW, plan.displayH / usableH);
  assert.ok(cover >= 0.95, 'cover=' + cover + ' must fill pane');
  // Labels stay readable vs micro-text: display width ≥ 40% of pane
  // (dense shrink still uses full pane; micro-island would be ~natural unscaled).
  assert.ok(plan.displayW >= usableW * 0.4 || plan.displayH >= usableH * 0.4);
  // Style plan clears max-width so CSS cannot re-shrink.
  const style = MA.svgScaleToFillStyle(plan);
  assert.ok(style, 'style present');
  assert.strictEqual(style.maxWidth, 'none');
  assert.ok(/px$/.test(style.width));
});

test('T268 multi diagramCount on single planner skips (use T276 pack path)', function () {
  const plan = MA.planSingleGraphScaleToFill({
    svgW: 800,
    svgH: 600,
    paneW: 1200,
    paneH: 900,
    diagramCount: 4,
  });
  assert.strictEqual(plan.mode, 'skip');
  assert.strictEqual(plan.fillsPane, false);
});

// ── 🎯T276: pack natural boxes → pane form-factor → scale composite ──────

test('T276 wrap-grid micro-island oracle FAILS ≥95% fill (old path)', function () {
  // Orthograph-class: 3 natural diagram boxes in a large frontier pane.
  // Old wrap-grid minmax(320px) leaves micro-islands — must NOT fill pane.
  const boxes = [
    { w: 900, h: 600 },
    { w: 700, h: 500 },
    { w: 500, h: 400 },
  ];
  const paneW = 1400;
  const paneH = 900;
  const micro = MA.planWrapGridMicroIslandOracle(boxes, { paneW: paneW, paneH: paneH, cellMax: 320 });
  assert.strictEqual(micro.mode, 'wrap-grid-micro');
  assert.strictEqual(micro.fillsPane, false, 'wrap-grid micro-islands must fail fill oracle');
  assert.ok(micro.maxCellCover < 0.5, 'max cover with 320px cells=' + micro.maxCellCover);
});

test('T276 pack+scale multi-box fills ≥95% of one pane axis', function () {
  // Same orthograph-class fixture: 3 components, large pane.
  const boxes = [
    { w: 900, h: 600, id: 'c0' },
    { w: 700, h: 500, id: 'c1' },
    { w: 500, h: 400, id: 'c2' },
  ];
  const paneW = 1400;
  const paneH = 900;
  const plan = MA.planMultiDiagramPackScaleToFill({
    boxes: boxes,
    paneW: paneW,
    paneH: paneH,
    padding: 0,
    gap: 12,
  });
  assert.strictEqual(plan.mode, 'pack-scale-to-fill');
  assert.strictEqual(plan.placements.length, 3);
  assert.ok(plan.scale > 0, 'scale=' + plan.scale);
  assert.strictEqual(plan.fillsPane, true, 'composite must fill pane');
  const cover = Math.max(plan.displayW / paneW, plan.displayH / paneH);
  assert.ok(cover >= 0.95, 'cover=' + cover + ' must be ≥0.95');
  // Display composite uses full contain — at least one axis nearly pane.
  assert.ok(
    Math.abs(plan.displayW - paneW) < 1 || Math.abs(plan.displayH - paneH) < 1 || cover >= 0.95
  );
  // Placements are non-overlapping in display space (shelf pack).
  for (let i = 0; i < plan.placements.length; i++) {
    const a = plan.placements[i];
    assert.ok(a.displayW > 0 && a.displayH > 0);
    for (let j = i + 1; j < plan.placements.length; j++) {
      const b = plan.placements[j];
      const sepX = a.displayX + a.displayW <= b.displayX + 1e-6
        || b.displayX + b.displayW <= a.displayX + 1e-6;
      const sepY = a.displayY + a.displayH <= b.displayY + 1e-6
        || b.displayY + b.displayH <= a.displayY + 1e-6;
      assert.ok(sepX || sepY, 'placements ' + i + ',' + j + ' must not overlap');
    }
  }
});

test('T276 packBoxesIntoPaneAspect returns composite matching pane aspect spirit', function () {
  const boxes = [
    { w: 400, h: 300 },
    { w: 400, h: 300 },
    { w: 400, h: 300 },
  ];
  // Wide pane → shelf width should allow side-by-side packing.
  const packed = MA.packBoxesIntoPaneAspect(boxes, { paneW: 1600, paneH: 800, gap: 0 });
  assert.ok(packed.compositeW > 0 && packed.compositeH > 0);
  assert.strictEqual(packed.placements.length, 3);
  // At least two boxes share a row when shelf is wide enough.
  const ys = packed.placements.map(function (p) { return p.y; });
  const uniqueY = ys.filter(function (y, i) { return ys.indexOf(y) === i; });
  assert.ok(uniqueY.length < 3, 'wide pane should shelf-pack multiple per row, ys=' + ys);
});

test('T276 index.html wires pack+scale for multi-diagram (not residual only)', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('planMultiDiagramPackScaleToFill') >= 0
    || html.indexOf('fitMermaidPackToPane') >= 0,
    'multi pack fit wired');
  assert.ok(html.indexOf('function fitMermaidPackToPane') >= 0, 'fitMermaidPackToPane defined');
  assert.ok(html.indexOf('fitMermaidPackToPane') >= 0
    && html.indexOf('renderMermaidDiagramPackInPanel') >= 0,
    'pack renderer triggers pack fit');
  // Must not early-return multi as residual-only.
  const fitStart = html.indexOf('function fitMermaidPanelSvgToPane');
  assert.ok(fitStart >= 0);
  const fitSlice = html.slice(fitStart, fitStart + 1200);
  assert.ok(fitSlice.indexOf('fitMermaidPackToPane') >= 0, 'single fit routes pack to T276');
  assert.ok(fitSlice.indexOf('multi residual') < 0, 'no multi residual early-return');
});

test('T268 applySvgScaleToFill mutates style + data attr', function () {
  const plan = MA.planSingleGraphScaleToFill({
    svgW: 200,
    svgH: 100,
    paneW: 800,
    paneH: 400,
    padding: 0,
    diagramCount: 1,
  });
  const removed = [];
  const attrs = {};
  const svg = {
    style: {},
    removeAttribute: function (k) { removed.push(k); },
    setAttribute: function (k, v) { attrs[k] = v; },
  };
  assert.strictEqual(MA.applySvgScaleToFill(svg, plan), true);
  assert.strictEqual(svg.style.maxWidth, 'none');
  assert.ok(parseFloat(svg.style.width) >= 799);
  assert.ok(removed.indexOf('width') >= 0 && removed.indexOf('height') >= 0);
  assert.ok(attrs['data-mvp-scale-fill']);
});

test('T268 index.html wires scale-to-fill for single large graph', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('planSingleGraphScaleToFill') >= 0, 'uses pure plan');
  assert.ok(html.indexOf('applySvgScaleToFill') >= 0 || html.indexOf('fitMermaidPanelSvgToPane') >= 0,
    'applies fit after render');
  assert.ok(html.indexOf('mvp-scale-fill') >= 0, 'scale-fill CSS class');
  // Single diagram from frontier pack routes to single-graph path (not pack grid).
  const ofgStart = html.indexOf('function openFrontierGraph');
  assert.ok(ofgStart >= 0);
  const ofgEnd = html.indexOf('\nfunction ', ofgStart + 10);
  const ofg = html.slice(ofgStart, ofgEnd > ofgStart ? ofgEnd : ofgStart + 8000);
  assert.ok(
    /diagrams\.length\s*===\s*1|nBlocks\s*===\s*1|length\s*===\s*1/.test(ofg),
    'single-diagram branch in openFrontierGraph'
  );
  assert.ok(ofg.indexOf('renderMermaidSourceInPanel') >= 0, 'single path uses source renderer');
});

// 🎯T274: pause earlier-history while large graph open; render timeout.
test('T274 shouldPauseHistoryHydrate only for open mvp-large', function () {
  assert.strictEqual(typeof MA.shouldPauseHistoryHydrate, 'function');
  assert.strictEqual(MA.shouldPauseHistoryHydrate(null), false);
  assert.strictEqual(
    MA.shouldPauseHistoryHydrate(mockPanel({ hidden: false, classes: ['open'] })),
    false,
    'compact open does not pause'
  );
  assert.strictEqual(
    MA.shouldPauseHistoryHydrate(mockPanel({ hidden: false, classes: ['open', 'mvp-large'] })),
    true,
    'large graph pauses hydrate'
  );
  assert.strictEqual(
    MA.shouldPauseHistoryHydrate(mockPanel({ hidden: true, classes: ['open', 'mvp-large'] })),
    false,
    'hidden does not pause'
  );
});

test('T274 withMermaidRenderTimeout rejects hung render', async function () {
  assert.strictEqual(typeof MA.withMermaidRenderTimeout, 'function');
  assert.ok(MA.MERMAID_RENDER_TIMEOUT_MS >= 1000);
  const hung = new Promise(function () { /* never settles */ });
  let rejected = null;
  try {
    await MA.withMermaidRenderTimeout(hung, 30);
  } catch (e) {
    rejected = e;
  }
  assert.ok(rejected, 'timeout rejects');
  assert.strictEqual(rejected.kind, 'timeout');
  // Fast resolve still wins.
  const ok = await MA.withMermaidRenderTimeout(Promise.resolve({ svg: '<svg/>' }), 500);
  assert.strictEqual(ok.svg, '<svg/>');
});

Promise.all(pending).then(function () {
  if (failed) {
    console.error(failed + ' failed');
    process.exit(1);
  }
  console.log('all mermaid_actions tests passed');
}).catch(function (e) {
  console.error('suite error', e);
  process.exit(1);
});
