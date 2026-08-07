// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for the fleet model prefix (🎯T287).
//
//   node web/scripts/model_prefix_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const MP = require('./model_prefix.js');

function test(name, fn) {
  try {
    fn();
    console.log('  ok -', name);
  } catch (e) {
    console.error('  FAIL -', name);
    console.error('   ', e && e.stack ? e.stack.split('\n').slice(0, 6).join('\n    ') : e);
    process.exitCode = 1;
  }
}

console.log('model_prefix_test (🎯T287)');

test('anthropic opus condenses to O + version', function () {
  assert.strictEqual(MP.condenseModel('claude-opus-4-8'), 'O4.8');
  assert.strictEqual(MP.condenseModel('claude-opus-4-5-20250929'), 'O4.5');
  assert.strictEqual(MP.condenseModel('claude-opus-5[1m]'), 'O5');
  assert.strictEqual(MP.condenseModel('opus'), 'O');
  assert.strictEqual(MP.condenseModel('us.anthropic.claude-opus-4-5-v1:0'), 'O4.5');
});

test('other anthropic families keep their initial', function () {
  assert.strictEqual(MP.condenseModel('claude-sonnet-4-5-20250929'), 'S4.5');
  assert.strictEqual(MP.condenseModel('claude-haiku-4-5-20251001'), 'H4.5');
});

test('grok drops the family letter — one flavour', function () {
  assert.strictEqual(MP.condenseModel('grok-4.5'), '4.5');
  assert.strictEqual(MP.condenseModel('grok-4'), '4');
  assert.strictEqual(MP.condenseModel('grok'), '');
});

// 🎯T293: the model id the server now reports for Grok rows comes from Grok's
// own billing frame ("grok-4.5-build"), not from a hand-typed override. The
// empty family initial must not swallow the version with it.
test('grok build ids still condense to the bare version', function () {
  assert.strictEqual(MP.condenseModel('grok-4.5-build'), '4.5');
  assert.strictEqual(MP.condenseModel('grok-4-build'), '4');
  const html = MP.modelPrefixHtml({ provider: 'grok', model: 'grok-4.5-build' });
  assert.ok(html.indexOf('<sub>4.5</sub>') !== -1, html);
  assert.ok(html.indexOf('data-company="xai"') !== -1, html);
});

// 🎯T293: the badge wore the X/Twitter mark, which names the wrong product.
test('grok wears a Grok mark, not the X mark', function () {
  const X_MARK = 'M3 3h4.2l13.8 18h-4.2L3 3z';
  const icon = MP.companyIconHtml('xai');
  assert.ok(icon.indexOf('<svg class="model-icon"') !== -1, icon);
  assert.ok(icon.indexOf(X_MARK) === -1, 'still painting the X mark: ' + icon);
  // Distinct from every other company mark, so the row is not ambiguous.
  assert.notStrictEqual(icon, MP.companyIconHtml('anthropic'));
  assert.notStrictEqual(icon, MP.companyIconHtml('openai'));
});

test('company comes from provider, falling back to the model id', function () {
  assert.strictEqual(MP.companyFor('claude', ''), 'anthropic');
  assert.strictEqual(MP.companyFor('bedrock', ''), 'anthropic');
  assert.strictEqual(MP.companyFor('grok', ''), 'xai');
  assert.strictEqual(MP.companyFor('codex', ''), 'openai');
  // No provider stored → sniff the model, never the agent name.
  assert.strictEqual(MP.companyFor('', 'claude-opus-4-8'), 'anthropic');
  assert.strictEqual(MP.companyFor('', 'grok-4.5'), 'xai');
  assert.strictEqual(MP.companyFor('', 'gpt-5.1'), 'openai');
  assert.strictEqual(MP.companyFor('', ''), '');
  assert.strictEqual(MP.companyFor('mystery-llm', ''), '');
});

test('owner examples paint icon + subscript label', function () {
  const anth = MP.modelPrefixHtml({ provider: 'claude', model: 'claude-opus-4-8' });
  assert.ok(anth.indexOf('data-company="anthropic"') !== -1, anth);
  assert.ok(anth.indexOf('<sub>O4.8</sub>') !== -1, anth);
  assert.ok(anth.indexOf('<svg class="model-icon"') !== -1, anth);

  const grok = MP.modelPrefixHtml({ provider: 'grok', model: 'grok-4.5' });
  assert.ok(grok.indexOf('data-company="xai"') !== -1, grok);
  assert.ok(grok.indexOf('<sub>4.5</sub>') !== -1, grok);
  // No leading G — only one Grok flavour.
  assert.ok(grok.indexOf('G4.5') === -1, grok);
});

test('unknown model paints the icon alone — no invented version', function () {
  const html = MP.modelPrefixHtml({ provider: 'grok' });
  assert.ok(html.indexOf('data-company="xai"') !== -1, html);
  assert.ok(html.indexOf('<sub>') === -1, html);
  assert.ok(html.indexOf('title="xAI · grok"') !== -1, html);
});

test('unknown company paints nothing at all', function () {
  assert.strictEqual(MP.modelPrefixHtml({ provider: 'mystery-llm' }), '');
  assert.strictEqual(MP.modelPrefixHtml({}), '');
  assert.strictEqual(MP.modelPrefixHtml(null), '');
});

test('prefix follows a migrate — provider/model change repaints', function () {
  const before = MP.modelPrefix({ provider: 'grok', model: 'grok-4.5' });
  const after = MP.modelPrefix({ provider: 'claude', model: 'claude-opus-4-8' });
  assert.strictEqual(before.company, 'xai');
  assert.strictEqual(before.label, '4.5');
  assert.strictEqual(after.company, 'anthropic');
  assert.strictEqual(after.label, 'O4.8');
});

test('tooltip carries the full model for hover truth', function () {
  const p = MP.modelPrefix({ provider: 'claude', model: 'claude-opus-4-5-20250929' });
  assert.strictEqual(p.title, 'Anthropic · claude-opus-4-5-20250929');
});

test('label and title are HTML-escaped', function () {
  const html = MP.modelPrefixHtml({ provider: 'claude', model: 'opus"><img src=x>' });
  assert.ok(html.indexOf('<img') === -1, html);
  assert.ok(html.indexOf('&quot;') !== -1, html);
});

// Product wiring: the RHS fleet tree must paint the prefix before the bare
// name, or the mapping above is a helper nobody calls.
test('index.html wires the prefix ahead of the agent name', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/model_prefix.js') !== -1, 'script tag missing');
  assert.ok(html.indexOf('ModelPrefix.modelPrefixHtml(') !== -1, 'helper never called');
  const NAME_SPAN = '\'<span class="agent-name">';
  const line = /^.*'<span class="agent-name">.*$/m.exec(html);
  assert.ok(line, 'agent-name paint site missing');
  const before = line[0].slice(0, line[0].indexOf(NAME_SPAN));
  assert.ok(/modelPrefixHtml|modelHtml/.test(before),
    'model prefix is not painted before the bare name: ' + line[0].trim());
  assert.ok(html.indexOf('.model-badge') !== -1, 'model-badge CSS missing');
  assert.ok(html.indexOf('.model-icon') !== -1, 'model-icon CSS missing');
});
