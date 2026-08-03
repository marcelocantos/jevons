// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for activity-strip tool arg summaries (🎯T116).
// Run: node web/scripts/tool_summary_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const TS = require('./tool_summary.js');

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

function looksLikeBareKeyList(s) {
  // Old bug: "tool_input, tool_name" or "limit, query" (comma-joined identifiers).
  return /^[a-zA-Z_][a-zA-Z0-9_]*(?:,\s*[a-zA-Z_][a-zA-Z0-9_]*)+$/.test(String(s || ''));
}

// ── plain string / string-first ──────────────────────────────────

test('plain string input is truncated single-line value', function () {
  const long = 'x'.repeat(100);
  const out = TS.summariseInput(long);
  assert.ok(out.length <= TS.MAX_LEN);
  assert.ok(out.endsWith('...'));
  assert.strictEqual(TS.summariseInput('hello world'), 'hello world');
  assert.strictEqual(TS.summariseInput('  spaced\nlines  '), 'spaced lines');
});

test('string-first object prefers a real string value', function () {
  const out = TS.summariseInput({ description: 'open the ledger', count: 2 });
  assert.ok(out.includes('open the ledger') || out === 'open the ledger');
  assert.ok(!looksLikeBareKeyList(out));
  assert.ok(!out.includes('description') || out.indexOf('open') >= 0);
});

// ── multi-key query-style (the search_tool dogfood case) ─────────

test('multi-key query-style shows query value not key dump', function () {
  // Object key order puts limit before query — old code dumped "limit, query".
  const input = { limit: 5, query: 'jevonsmcp agent list' };
  const out = TS.summariseInput(input);
  assert.ok(out.includes('jevonsmcp agent list'), 'got: ' + out);
  assert.ok(!looksLikeBareKeyList(out), 'must not be bare key list: ' + out);
  assert.ok(!/^limit/.test(out), 'must not start with key name dump: ' + out);
});

test('preferred path/command/title beat key order', function () {
  assert.ok(TS.summariseInput({ verbose: true, path: '/tmp/x' }).includes('/tmp/x'));
  assert.ok(TS.summariseInput({ timeout: 1, command: 'make test' }).includes('make test'));
  assert.ok(TS.summariseInput({ id: 'abc', title: 'Retire T116' }).includes('Retire T116'));
});

// ── nested tool_input (use_tool MCP shape) ───────────────────────

test('nested tool_input prefers nested useful fields over key dump', function () {
  const input = {
    tool_name: 'search_tool',
    tool_input: { limit: 3, query: 'bullseye frontier' },
  };
  const out = TS.summariseInput(input);
  assert.ok(out.includes('bullseye frontier'), 'value must appear: ' + out);
  assert.ok(!looksLikeBareKeyList(out), 'must not dump tool_input, tool_name: ' + out);
  // Prefer showing the nested tool name with the value gist.
  assert.ok(out.includes('search_tool'), 'tool_name should appear: ' + out);
});

test('nested tool_input without tool_name still surfaces nested value', function () {
  const out = TS.summariseInput({
    tool_input: { path: '/Users/me/repo', dry_run: true },
  });
  assert.ok(out.includes('/Users/me/repo'), 'got: ' + out);
  assert.ok(!looksLikeBareKeyList(out));
});

test('one nesting level into non-tool_input objects finds strings', function () {
  const out = TS.summariseInput({
    arguments: { query: 'nested once' },
    meta: { source: 'test' },
  });
  assert.ok(out.includes('nested once'), 'got: ' + out);
  assert.ok(!looksLikeBareKeyList(out));
});

// ── never key-only when a value exists; stay short ───────────────

test('never key-name-only when any non-empty string exists', function () {
  const cases = [
    { a: '', b: 'value-here' },
    { empty: '  ', keep: 'x' },
    { tool_input: {}, tool_name: 'only-name' },
  ];
  cases.forEach(function (c, i) {
    const out = TS.summariseInput(c);
    assert.ok(out.length > 0, 'case ' + i + ' empty');
    assert.ok(!looksLikeBareKeyList(out), 'case ' + i + ' key dump: ' + out);
  });
});

test('summaries stay single-line and truncated', function () {
  const out = TS.summariseInput({
    query: 'line1\nline2\t' + 'y'.repeat(80),
  });
  assert.ok(out.indexOf('\n') < 0);
  assert.ok(out.length <= TS.MAX_LEN);
});

test('null / empty / no useful values do not throw', function () {
  assert.strictEqual(TS.summariseInput(null), '');
  assert.strictEqual(TS.summariseInput(undefined), '');
  assert.strictEqual(TS.summariseInput({}), '');
  assert.strictEqual(TS.summariseInput({ tool_input: {} }), '');
});

// ── index.html wiring ────────────────────────────────────────────

test('index.html loads ToolSummary and uses summariseInput from it', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/tool_summary.js'), 'must load tool_summary.js');
  assert.ok(html.includes('ToolSummary'), 'must reference ToolSummary');
  assert.ok(
    /ToolSummary\.summariseInput/.test(html),
    'activity strip must call ToolSummary.summariseInput'
  );
  // Local naive implementation must be gone (T116 extraction).
  assert.ok(
    !/function summariseInput\s*\(/.test(html),
    'must not keep a local summariseInput function in index.html'
  );
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nall ok');
