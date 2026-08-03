// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic oracle for 🎯T150 progressive streaming markdown.
// Run: node web/scripts/streaming_markdown_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');

const smd = require('./smd.js');
const StreamingMarkdown = require('./streaming_markdown.js');
const MarkdownNormalize = require('./markdown_normalize.js');

let passed = 0;
function test(name, fn) {
  try {
    fn();
    passed++;
    console.log('ok  -', name);
  } catch (e) {
    console.error('FAIL -', name);
    console.error(e && e.stack ? e.stack : e);
    process.exitCode = 1;
  }
}

// ── Spike: lib loads + incomplete → complete **bold** ───────────

test('smd UMD loads (parser + default_renderer + write/end)', () => {
  assert.strictEqual(typeof smd.parser, 'function');
  assert.strictEqual(typeof smd.parser_write, 'function');
  assert.strictEqual(typeof smd.parser_end, 'function');
  assert.strictEqual(typeof smd.default_renderer, 'function');
});

test('T150 hermetic: incomplete then complete **text** → <strong> before seal', () => {
  // Stream paint path simulation: successive full _streamRaw snapshots.
  const steps = StreamingMarkdown.renderSteps(
    ['**tex', '**text', '**text**', '**text** done'],
    { smd: smd },
  );
  // After closer arrives (third snapshot) — before any seal/end paint.
  const afterCloser = steps.steps[2];
  assert.ok(
    afterCloser.html.includes('<strong>'),
    'expected <strong> after closer, got: ' + afterCloser.html,
  );
  assert.ok(
    /<strong>text<\/strong>/.test(afterCloser.html),
    'closed span must wrap text, got: ' + afterCloser.html,
  );
  // No raw **text** left for the closed span.
  assert.ok(
    !afterCloser.html.includes('**text**'),
    'must not leave raw **text** in HTML: ' + afterCloser.html,
  );
  assert.ok(
    !afterCloser.text.includes('**'),
    'visible text must not show asterisks for closed span: ' + afterCloser.text,
  );
  assert.ok(afterCloser.text.includes('text'), 'text content present');
});

test('T150 hermetic: bold lead like **claudia-po** done', () => {
  const steps = StreamingMarkdown.renderSteps(
    ['**claudia-po', '**claudia-po**', '**claudia-po** done: hello'],
    { smd: smd },
  );
  const closed = steps.steps[1];
  assert.ok(/<strong>claudia-po<\/strong>/.test(closed.html), closed.html);
  assert.ok(!closed.html.includes('**claudia-po**'), closed.html);
  const more = steps.steps[2];
  assert.ok(more.html.includes('<strong>'), more.html);
  assert.ok(more.text.includes('done: hello') || more.html.includes('done'), more.html);
});

test('T150 renderProgressive with ensureFenceNewlines hygiene', () => {
  const out = StreamingMarkdown.renderProgressive(
    ['See:```cpp\nint x;\n```'],
    { smd: smd, normalize: MarkdownNormalize.ensureFenceNewlines },
  );
  // Fence should open as code, not stuck as prose with backticks.
  assert.ok(
    out.html.includes('<pre') || out.html.includes('<code'),
    'expected fence structure: ' + out.html,
  );
  assert.ok(out.text.includes('int x'), out.text);
});

test('T150 progressive append: Hello → Hello **world** ends with strong', () => {
  // smd may hold a trailing char in pending mid-write; after closer + end, sealed.
  const a = StreamingMarkdown.renderSteps(
    ['Hello ', 'Hello **wor', 'Hello **world**'],
    { smd: smd },
  );
  const closed = a.steps[2];
  assert.ok(/<strong>world<\/strong>/.test(closed.html), closed.html);
  assert.ok(closed.text.includes('Hello'), closed.text);
  assert.ok(closed.text.includes('world'), closed.text);
  assert.ok(!closed.html.includes('**world**'), closed.html);
});

// ── index.html product wiring (no plain textContent stream) ─────

test('index.html loads smd + StreamingMarkdown and progressive stream paint', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/smd.js'), 'must load vendored smd.js');
  assert.ok(
    html.includes('scripts/streaming_markdown.js'),
    'must load streaming_markdown.js',
  );
  assert.ok(
    html.includes('paintStreamBody') || html.includes('StreamingMarkdown.createSession'),
    'must wire progressive stream paint',
  );
  assert.ok(
    html.includes('_smdSession') || html.includes('createSession'),
    'must keep per-bubble stream parser state',
  );
  // Forbidden product default: plain textContent mid-stream for jevons body.
  // Allow textContent elsewhere (user/status/probe) but not the T150 WIP pattern.
  assert.ok(
    !/if\s*\([^)]*_streamRaw[^)]*\)\s*\{[^}]*textContent\s*=/s.test(html),
    'must not assign textContent gated on _streamRaw (plain stream WIP)',
  );
});

test('index.html seal still uses full marked path (mermaid/highlight gated)', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('parseAssistantMarkdown'), 'seal/marked path present');
  assert.ok(html.includes('renderMermaidIn'), 'T59 mermaid');
  assert.ok(html.includes('highlightCodeIn'), 'T74 highlight');
  // Stream vs seal branch: progressive only while typeof _streamRaw === 'string'.
  assert.ok(
    html.includes("typeof d._streamRaw === 'string'")
      || html.includes('typeof d._streamRaw === "string"'),
    'mermaid/highlight only on sealed (non-stream) paint branch',
  );
  assert.ok(html.includes('paintStreamBody'), 'stream path is paintStreamBody');
  assert.ok(html.includes('destroyStreamSession'), 'seal tears down smd session');
});

if (!process.exitCode) {
  console.log('\n' + passed + ' tests passed (T150 streaming markdown)');
}
