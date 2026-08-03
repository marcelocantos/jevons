// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic oracle for 🎯T151 chat/UI links open in a new tab.
// Run: node web/scripts/link_safety_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');

const LinkSafety = require('./link_safety.js');
const smd = require('./smd.js');
const StreamingMarkdown = require('./streaming_markdown.js');

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

// ── Pure string rewrite ─────────────────────────────────────────

test('ensureHtmlAnchors adds target=_blank + rel on bare <a href>', () => {
  const inHtml = '<p>See <a href="https://example.com">ex</a> now</p>';
  const out = LinkSafety.ensureHtmlAnchors(inHtml);
  assert.ok(/target="_blank"/.test(out), out);
  assert.ok(/rel="noopener noreferrer"/.test(out), out);
  assert.ok(/href="https:\/\/example.com"/.test(out), out);
  assert.ok(out.includes('>ex</a>'), out);
});

test('ensureHtmlAnchors overwrites existing target/rel', () => {
  const inHtml = '<a href="https://x.test" target="_self" rel="nofollow">x</a>';
  const out = LinkSafety.ensureHtmlAnchors(inHtml);
  assert.ok(out.includes('target="_blank"'), out);
  assert.ok(!/target="_self"/.test(out), out);
  assert.ok(/rel="noopener noreferrer"/.test(out), out);
});

test('ensureHtmlAnchors is idempotent', () => {
  const once = LinkSafety.ensureHtmlAnchors('<a href="https://a.test">a</a>');
  const twice = LinkSafety.ensureHtmlAnchors(once);
  assert.strictEqual(twice, once);
});

test('ensureHtmlAnchors leaves non-anchor HTML alone', () => {
  const html = '<p><strong>hi</strong> and <code>x</code></p>';
  assert.strictEqual(LinkSafety.ensureHtmlAnchors(html), html);
});

test('ensureHtmlAnchors handles empty/null', () => {
  assert.strictEqual(LinkSafety.ensureHtmlAnchors(''), '');
  assert.strictEqual(LinkSafety.ensureHtmlAnchors(null), null);
});

// ── Progressive smd hermetic path (markdown → <a target=_blank>) ─

test('T151 hermetic: markdown link renders <a target=_blank rel=…>', () => {
  const out = StreamingMarkdown.renderProgressive(
    ['See [docs](https://example.com/path) please'],
    { smd: smd },
  );
  assert.ok(out.html.includes('<a'), 'expected anchor: ' + out.html);
  assert.ok(
    /target="_blank"/.test(out.html),
    'expected target=_blank: ' + out.html,
  );
  assert.ok(
    /rel="noopener noreferrer"/.test(out.html),
    'expected safe rel: ' + out.html,
  );
  assert.ok(
    /href="https:\/\/example.com\/path"/.test(out.html),
    'href preserved: ' + out.html,
  );
  assert.ok(out.text.includes('docs'), out.text);
});

test('T151 hermetic: raw URL also gets target=_blank', () => {
  const out = StreamingMarkdown.renderProgressive(
    ['Visit https://raw.example/x end'],
    { smd: smd },
  );
  // smd may or may not autolink raw URLs depending on grammar; if it does, secure it.
  if (out.html.includes('<a')) {
    assert.ok(/target="_blank"/.test(out.html), out.html);
    assert.ok(/rel="noopener noreferrer"/.test(out.html), out.html);
  } else {
    // Accept residual: no anchor emitted for this fixture.
    assert.ok(out.text.includes('https://raw.example/x'), out.text);
  }
});

// ── Product wiring in index.html ────────────────────────────────

test('index.html loads LinkSafety and wires seal + stream paths', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/link_safety.js'), 'must load link_safety.js');
  assert.ok(
    html.includes('LinkSafety.ensureHtmlAnchors') || html.includes('ensureHtmlAnchors'),
    'seal path must call ensureHtmlAnchors',
  );
  assert.ok(
    html.includes('decorateBodyLinks') || html.includes('decorateContainer'),
    'DOM paint must decorate anchors',
  );
  assert.ok(
    html.includes('LinkSafety.decorateAnchor') || html.includes('decorateAnchor'),
    'click path decorates anchors via LinkSafety',
  );
  assert.ok(
    html.includes('wireLinkSafety') || html.includes("closest('a[href]')"),
    'click capture for residual anchors',
  );
  // Module itself is the source of truth for the attr values.
  const mod = fs.readFileSync(path.join(__dirname, 'link_safety.js'), 'utf8');
  assert.ok(mod.includes('_blank'), 'module sets target _blank');
  assert.ok(mod.includes('noopener') && mod.includes('noreferrer'), 'module sets safe rel');
});

test('parseAssistantMarkdown seal path post-processes anchors', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  // Must run ensureHtmlAnchors on marked output before return.
  const fn = html.match(/function parseAssistantMarkdown[\s\S]*?\n\}/);
  assert.ok(fn, 'parseAssistantMarkdown present');
  assert.ok(
    fn[0].includes('ensureHtmlAnchors') || fn[0].includes('LinkSafety'),
    'parseAssistantMarkdown must apply LinkSafety: ' + fn[0].slice(0, 200),
  );
});

if (!process.exitCode) {
  console.log('\n' + passed + ' tests passed (T151 link safety)');
}
