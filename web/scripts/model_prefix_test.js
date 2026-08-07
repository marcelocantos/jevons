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

// 🎯T296: T293's replacement read as a generic pair of blades. The Grok mark
// is a ring cut by one diagonal slash — and still neither X nor blades.
test('grok wears the slashed ring', function () {
  const X_MARK = 'M3 3h4.2l13.8 18h-4.2L3 3z';
  const BLADE_MARK = 'M13.6 2.6h7L9.4 21.4h-7l11.2-18.8z';
  const icon = MP.companyIconHtml('xai');
  assert.ok(icon.indexOf(X_MARK) === -1, 'still painting the X mark: ' + icon);
  assert.ok(icon.indexOf(BLADE_MARK) === -1, 'still painting the blades: ' + icon);
  // A ring: an unfilled stroked circle, not a disc.
  const ring = /<circle\b[^>]*>/.exec(icon);
  assert.ok(ring, 'no ring in the Grok mark: ' + icon);
  assert.ok(/fill="none"/.test(ring[0]), 'ring is a filled disc: ' + ring[0]);
  assert.ok(/stroke="currentColor"/.test(ring[0]), 'ring ignores row colour: ' + ring[0]);
  // One diagonal slash across it: a single straight segment whose ends differ
  // on both axes, and which runs the other way from the ring's own centre.
  const slash = /d="M([\d.]+) ([\d.]+)L([\d.]+) ([\d.]+)"/.exec(icon);
  assert.ok(slash, 'no slash segment in the Grok mark: ' + icon);
  const [x1, y1, x2, y2] = slash.slice(1).map(Number);
  assert.notStrictEqual(x1, x2, 'slash is vertical, not diagonal: ' + slash[0]);
  assert.notStrictEqual(y1, y2, 'slash is horizontal, not diagonal: ' + slash[0]);
  // Overshoots the ring on both ends, so the cut reads at 12px.
  const cx = Number(/cx="([\d.]+)"/.exec(ring[0])[1]);
  const cy = Number(/cy="([\d.]+)"/.exec(ring[0])[1]);
  const r = Number(/r="([\d.]+)"/.exec(ring[0])[1]);
  [[x1, y1], [x2, y2]].forEach(function (pt) {
    const d = Math.hypot(pt[0] - cx, pt[1] - cy);
    assert.ok(d > r, 'slash end ' + pt + ' stops inside the ring (r=' + r + ')');
  });
  // Still distinct from every other company mark.
  assert.notStrictEqual(icon, MP.companyIconHtml('anthropic'));
  assert.notStrictEqual(icon, MP.companyIconHtml('openai'));
  assert.ok(icon.indexOf('data-mark="grok"') !== -1, icon);
});

// 🎯T295: the Anthropic slot wore the A-wordmark, which names the vendor's
// letterhead rather than the model the row is running.
test('claude wears the splat, not the anthropic wordmark', function () {
  const WORDMARK = 'M16.23 5h-3.02l5.507 15h3.02L16.23 5z';
  const icon = MP.companyIconHtml('anthropic');
  assert.ok(icon.indexOf('<svg class="model-icon"') !== -1, icon);
  assert.ok(icon.indexOf('data-mark="claude-splat"') !== -1, icon);
  assert.ok(icon.indexOf(WORDMARK) === -1, 'still painting the wordmark: ' + icon);
  // The splat is a burst of blades from one centre, not a glyph.
  assert.ok((icon.match(/M12 12L/g) || []).length >= 8, icon);
  // Gray splat only: no plate, ring, or outer border around the mark.
  assert.ok(!/<(circle|rect|ellipse)\b/.test(icon), 'mark carries a border: ' + icon);
  // Distinct from every other company mark, so the row is not ambiguous.
  assert.notStrictEqual(icon, MP.companyIconHtml('xai'));
  assert.notStrictEqual(icon, MP.companyIconHtml('openai'));
  // Each slot names which mark it wears — 🎯T293's Grok mark stays Grok's.
  assert.ok(MP.companyIconHtml('xai').indexOf('data-mark="grok"') !== -1);
});

// 🎯T295: the owner read a padded subscript as a different version. No segment
// keeps a leading zero; internal zeros are significant.
test('version segments are never zero-padded', function () {
  assert.strictEqual(MP.condenseModel('claude-opus-4-05'), 'O4.5');
  assert.strictEqual(MP.condenseModel('claude-opus-05'), 'O5');
  assert.strictEqual(MP.condenseModel('claude-sonnet-04-05'), 'S4.5');
  assert.strictEqual(MP.condenseModel('grok-05'), '5');
  // Internal zeros survive: 10 is ten, not one.
  assert.strictEqual(MP.condenseModel('claude-opus-10'), 'O10');
  assert.strictEqual(MP.condenseModel('claude-opus-4-10'), 'O4.10');
  assert.strictEqual(MP.condenseModel('grok-10.0'), '10.0');
  // A lone zero is a real segment, not padding.
  assert.strictEqual(MP.condenseModel('claude-opus-5-0'), 'O5.0');
  // Every representative Claude id lands with no zero-padded segment.
  ['claude-opus-5', 'claude-opus-5[1m]', 'claude-opus-4-8', 'claude-opus-05',
    'claude-opus-4-5-20250929', 'claude-sonnet-4-5-20250929',
    'claude-haiku-4-5-20251001', 'us.anthropic.claude-opus-4-5-v1:0',
  ].forEach(function (id) {
    const label = MP.condenseModel(id);
    assert.ok(!/(^|\.)0\d/.test(label), id + ' → padded label ' + label);
  });
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

// 🎯T296: the badge read as two spaced pieces — mark, gap, version — instead of
// one word. Icon and subscript must sit adjacent with no separating space.
test('badge CSS keeps icon and subscript as one word', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const badge = /\.agent-node \.model-badge \{([^}]*)\}/.exec(html);
  assert.ok(badge, '.model-badge rule missing');
  // The flex gap is what pulled the two apart; nothing separates them now.
  const gap = /(^|[;{\s])gap:\s*([^;]+);/.exec(badge[1]);
  if (gap) {
    assert.ok(/^0(px|em|rem)?$/.test(gap[2].trim()),
      'badge still spaces icon from subscript: gap: ' + gap[2].trim());
  }

  const sub = /\.agent-node \.model-badge sub \{([^}]*)\}/.exec(html);
  assert.ok(sub, '.model-badge sub rule missing');
  // Subscript sits on the mark's edge, never pushed off it.
  const ml = /margin-left:\s*(-?[\d.]+)px/.exec(sub[1]);
  assert.ok(ml, 'subscript declares no margin-left: ' + sub[1].trim());
  assert.ok(Number(ml[1]) <= 0, 'subscript pushed off the mark: ' + ml[0]);
  // Tight tracking within the version itself, so it reads as one token.
  const ls = /letter-spacing:\s*(-?[\d.]+)em/.exec(sub[1]);
  assert.ok(ls, 'subscript declares no letter-spacing: ' + sub[1].trim());
  assert.ok(Number(ls[1]) <= -0.03, 'subscript tracking is loose: ' + ls[0]);
});
