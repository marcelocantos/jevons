// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic tests for markdown fence smush normalization (🎯T145, 🎯T146).
// Run: node web/scripts/markdown_normalize_test.js

'use strict';

const assert = require('assert');
const MarkdownNormalize = require('./markdown_normalize.js');

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

const { ensureFenceNewlines } = MarkdownNormalize;

// ── Repro: colon glued to ```cpp (acceptance fixture) ───────────

test('smushed colon+cpp fence gets blank line before fence', () => {
  const input = "Here's a snippet:```cpp\nint x;\n```";
  const out = ensureFenceNewlines(input);
  assert.strictEqual(out, "Here's a snippet:\n\n```cpp\nint x;\n```");
  // Fence must not share a line with preceding prose.
  const fenceLine = out.split('\n').find((l) => l.startsWith('```'));
  assert.ok(fenceLine, 'expected a fence line');
  assert.strictEqual(fenceLine, '```cpp');
});

test('smushed bare fence (no lang) gets blank line', () => {
  const input = 'See:```\ncode\n```';
  assert.strictEqual(ensureFenceNewlines(input), 'See:\n\n```\ncode\n```');
});

// ── Already well-formed: unchanged ──────────────────────────────

test('fence after blank line is unchanged', () => {
  const input = "Here's a snippet:\n\n```cpp\nint x;\n```";
  assert.strictEqual(ensureFenceNewlines(input), input);
});

test('fence after single newline is unchanged', () => {
  const input = "Here's a snippet:\n```cpp\nint x;\n```";
  assert.strictEqual(ensureFenceNewlines(input), input);
});

test('mermaid fence with newlines is unchanged', () => {
  const input = 'Diagram:\n\n```mermaid\ngraph TD\n  A-->B\n```';
  assert.strictEqual(ensureFenceNewlines(input), input);
});

test('message that is only a fence is unchanged', () => {
  const input = '```go\nfmt.Println("hi")\n```';
  assert.strictEqual(ensureFenceNewlines(input), input);
});

// ── Edge cases ──────────────────────────────────────────────────

test('empty and null passthrough', () => {
  assert.strictEqual(ensureFenceNewlines(''), '');
  assert.strictEqual(ensureFenceNewlines(null), null);
  assert.strictEqual(ensureFenceNewlines(undefined), undefined);
});

test('c++ language tag (plus in lang)', () => {
  const input = 'Use:```c++\nint x;\n```';
  assert.strictEqual(ensureFenceNewlines(input), 'Use:\n\n```c++\nint x;\n```');
});

test('multiple smushed fences each get a blank line', () => {
  const input = 'A:```\na\n``` then B:```js\nb\n```';
  const out = ensureFenceNewlines(input);
  assert.ok(out.includes('A:\n\n```\n'), 'first fence');
  assert.ok(out.includes('B:\n\n```js\n'), 'second fence');
});

test('idempotent: second pass is no-op', () => {
  const input = "Here's a snippet:```cpp\nint x;\n```";
  const once = ensureFenceNewlines(input);
  assert.strictEqual(ensureFenceNewlines(once), once);
});

// ── Closing fence on its own line still fine ────────────────────

test('closing fence after code line unchanged when opening already good', () => {
  // Opening is at start — no smush. Inner mid-line ``` is not a real block
  // start (no newline after fence token) so stays put (🎯T146).
  const well = '```python\nprint(1)\n```';
  assert.strictEqual(ensureFenceNewlines(well), well);
  const withInner = '```\nline with ``` ticks inside\n```';
  assert.strictEqual(ensureFenceNewlines(withInner), withInner);
});

// ── 🎯T146: mid-prose fence markers must not open blocks ────────

test('T146 mid-prose bare ``` example is unchanged (no empty fence)', () => {
  // Acceptance: intentional fence characters in prose must not become an
  // opener. Pre-T146 blank-line insert would yield "...like \n\n``` in..."
  // and marked would treat the rest of the message as a broken code block.
  const input = 'Use triple backticks like ``` in the docs.';
  const out = ensureFenceNewlines(input);
  assert.strictEqual(out, input);
  assert.ok(!out.includes('\n\n```'), 'must not insert blank line before mid-prose fence');
  // No line that is only a fence opener with dangling info-string prose.
  const lines = out.split('\n');
  assert.ok(
    !lines.some((l) => /^```/.test(l)),
    'mid-prose example must not produce a column-0 fence line: ' + JSON.stringify(out),
  );
});

test('T146 mid-prose ```lang word example is unchanged', () => {
  const input = 'Here is ```cpp as a language tag example only.';
  assert.strictEqual(ensureFenceNewlines(input), input);
});

test('T146 smushed-form description without real block stays prose', () => {
  // Describes the smush pattern without providing a following newline body.
  const input = 'The smushed form is prose:```lang then more words.';
  assert.strictEqual(ensureFenceNewlines(input), input);
});

test('T146 CRLF smushed real fence still gets blank line', () => {
  const input = 'See:```cpp\r\nint x;\r\n```';
  const out = ensureFenceNewlines(input);
  assert.strictEqual(out, 'See:\n\n```cpp\r\nint x;\r\n```');
});

test('T146 incomplete stream fence without newline is left (no premature open)', () => {
  // Streaming may pause after ```lang before the body newline; do not invent
  // a fence line yet. When "\ncode" arrives, full-text re-normalize fixes it.
  const partial = "Here's a snippet:```cpp";
  assert.strictEqual(ensureFenceNewlines(partial), partial);
  const full = partial + '\nint x;\n```';
  assert.strictEqual(
    ensureFenceNewlines(full),
    "Here's a snippet:\n\n```cpp\nint x;\n```",
  );
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nall passed');
