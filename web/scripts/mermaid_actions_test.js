// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for mermaid open/copy helpers (🎯T83.1).
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

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('all mermaid_actions tests passed');
