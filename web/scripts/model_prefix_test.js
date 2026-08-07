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

// Marks this badge has worn and must never wear again. Each one shipped, the
// owner read it as the wrong thing, and a later target replaced it.
const RETIRED_MARKS = {
  'X/Twitter (🎯T293)': 'M3 3h4.2l13.8 18h-4.2L3 3z',
  'twin blades (🎯T296)': 'M13.6 2.6h7L9.4 21.4h-7l11.2-18.8z',
  'Anthropic wordmark (🎯T295)': 'M16.23 5h-3.02l5.507 15h3.02L16.23 5z',
  'hand-drawn splat (🎯T299)': 'M12 12L12 3M12 12L15.89 5.94',
};

function indexHtml() {
  return fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
}

console.log('model_prefix_test (🎯T287)');

// 🎯T299: the family initial is gone. 'O' butted against digits read as a
// zero, so Opus 5 landed on the owner's screen as '05'. The splat already
// says Claude; the version alone is what the subscript carries.
test('anthropic models condense to the bare version — no family initial', function () {
  assert.strictEqual(MP.condenseModel('claude-opus-4-8'), '4.8');
  assert.strictEqual(MP.condenseModel('claude-opus-4-5-20250929'), '4.5');
  assert.strictEqual(MP.condenseModel('claude-opus-5[1m]'), '5');
  assert.strictEqual(MP.condenseModel('us.anthropic.claude-opus-4-5-v1:0'), '4.5');
  assert.strictEqual(MP.condenseModel('claude-sonnet-4-5-20250929'), '4.5');
  assert.strictEqual(MP.condenseModel('claude-haiku-4-5-20251001'), '4.5');
  // A family with no version says nothing the mark does not already say.
  assert.strictEqual(MP.condenseModel('opus'), '');
});

// 🎯T298 / 🎯T299: the failure the owner reported was reading, not arithmetic —
// 'O5' is indistinguishable from '05' at 9px. No label may open with a glyph
// that can pass for a leading zero.
test('no label can read as a zero-padded number', function () {
  ['claude-opus-5', 'claude-opus-05', 'claude-opus-5[1m]', 'claude-opus-4-8',
    'claude-opus-4-05', 'claude-opus-4-5-20250929', 'claude-sonnet-04-05',
    'claude-haiku-4-5-20251001', 'us.anthropic.claude-opus-4-5-v1:0',
    'grok-4.5-build', 'grok-05',
  ].forEach(function (id) {
    const label = MP.condenseModel(id);
    assert.ok(!/^[Oo0]\d/.test(label), id + ' → label opens as a zero: ' + label);
    assert.ok(!/(^|\.)0\d/.test(label), id + ' → padded segment in label: ' + label);
    // Nothing but digits and dots reaches the subscript at all.
    assert.ok(/^[\d.]*$/.test(label), id + ' → non-numeric label ' + label);
  });
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

// 🎯T299: every mark this badge wore before was drawn here by hand, and the
// owner rejected each one in turn. The paths are now the brands' own — Claude
// from Simple Icons (CC0-1.0), Grok from the app icon on Wikimedia Commons
// (CC0) — so the assertions pin the real geometry, not a fresh approximation.
test('claude wears the real Claude mark from Simple Icons', function () {
  const icon = MP.companyIconHtml('anthropic');
  assert.ok(icon.indexOf('<svg class="model-icon"') !== -1, icon);
  assert.ok(icon.indexOf('data-mark="claude-splat"') !== -1, icon);
  // Verbatim opening and interior of simple-icons/icons/claude.svg.
  const d = /<path[^>]*\bd="([^"]+)"/.exec(icon);
  assert.ok(d, 'no path data in the Claude mark: ' + icon);
  assert.ok(d[1].indexOf('m4.7144 15.9555 4.7174-2.6471') === 0,
    'Claude path does not open with the Simple Icons glyph: ' + d[1].slice(0, 60));
  assert.ok(d[1].indexOf('.5343-.7042.0546-.3522.4797-.3218') !== -1,
    'Claude path interior is not the Simple Icons glyph');
  // Real brand geometry, not a handful of strokes standing in for it.
  assert.ok(d[1].length > 1500, 'Claude path is too simple to be the real mark: ' + d[1].length);
  // Gray glyph only: no plate, ring, or outer border around the mark.
  assert.ok(!/<(circle|rect|ellipse)\b/.test(icon), 'mark carries a border: ' + icon);
  assert.ok(/fill="currentColor"/.test(icon), 'mark ignores row colour: ' + icon);
});

test('grok wears the real Grok mark from its app icon', function () {
  const icon = MP.companyIconHtml('xai');
  assert.ok(icon.indexOf('data-mark="grok"') !== -1, icon);
  const d = /<path[^>]*\bd="([^"]+)"/.exec(icon);
  assert.ok(d, 'no path data in the Grok mark: ' + icon);
  // Verbatim from the white mark of File:Grok-icon.svg — both subpaths.
  assert.ok(d[1].indexOf('M213.235 306.019l178.976-180.002') === 0,
    'Grok path does not open with the app-icon glyph: ' + d[1].slice(0, 60));
  assert.ok(d[1].indexOf('zm-25.786 22.437') !== -1, 'Grok mark lost its second stroke');
  assert.ok(d[1].indexOf('68.094 435.217') !== -1, 'Grok path interior is not the app-icon glyph');
  assert.ok(d[1].length > 600, 'Grok path is too simple to be the real mark: ' + d[1].length);
  // The plate the source art sits on is dropped; only the glyph survives.
  assert.ok(!/<(circle|rect|ellipse)\b/.test(icon), 'mark carries a plate: ' + icon);
  assert.ok(/fill="currentColor"/.test(icon), 'mark ignores row colour: ' + icon);
  // Its own 512-unit coordinate space, squared so the 12px badge does not
  // stretch the glyph on one axis.
  const vb = /viewBox="([^"]+)"/.exec(icon);
  assert.ok(vb, 'Grok mark has no viewBox: ' + icon);
  const box = vb[1].trim().split(/\s+/).map(Number);
  assert.strictEqual(box.length, 4, 'malformed viewBox: ' + vb[1]);
  assert.strictEqual(box[2], box[3], 'viewBox is not square — glyph will stretch: ' + vb[1]);
  // The glyph's own bbox is 68.09,74.42 375.81x360.79; the box must hold it.
  assert.ok(box[0] <= 68.09 && box[1] <= 74.42, 'viewBox clips the glyph: ' + vb[1]);
  assert.ok(box[0] + box[2] >= 443.91 && box[1] + box[3] >= 435.22,
    'viewBox clips the glyph: ' + vb[1]);
});

test('no retired mark is ever painted again', function () {
  ['anthropic', 'xai', 'openai'].forEach(function (company) {
    const icon = MP.companyIconHtml(company);
    Object.keys(RETIRED_MARKS).forEach(function (why) {
      assert.ok(icon.indexOf(RETIRED_MARKS[why]) === -1,
        company + ' still paints the ' + why + ' mark');
    });
  });
  // Each company reads as a different row at a glance.
  const marks = ['anthropic', 'xai', 'openai'].map(MP.companyIconHtml);
  assert.strictEqual(new Set(marks).size, marks.length, 'two companies share a mark');
});

// 🎯T295 / 🎯T299: the owner read a padded subscript as a different version.
// Each segment is parsed as an integer and printed back, so padding cannot
// survive whatever the model id spells. Internal zeros are significant.
test('version segments are integers, not digit runs', function () {
  assert.strictEqual(MP.condenseModel('claude-opus-4-05'), '4.5');
  assert.strictEqual(MP.condenseModel('claude-opus-05'), '5');
  assert.strictEqual(MP.condenseModel('claude-sonnet-04-05'), '4.5');
  assert.strictEqual(MP.condenseModel('grok-05'), '5');
  assert.strictEqual(MP.condenseModel('claude-opus-000-007'), '0.7');
  // Internal zeros survive: 10 is ten, not one.
  assert.strictEqual(MP.condenseModel('claude-opus-10'), '10');
  assert.strictEqual(MP.condenseModel('claude-opus-4-10'), '4.10');
  assert.strictEqual(MP.condenseModel('grok-10.0'), '10.0');
  // A lone zero is a real segment, not padding.
  assert.strictEqual(MP.condenseModel('claude-opus-5-0'), '5.0');
  // A release date is still a date, not a version segment.
  assert.strictEqual(MP.condenseModel('claude-opus-4-5-20250929'), '4.5');
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
  assert.ok(anth.indexOf('<sub>4.8</sub>') !== -1, anth);
  assert.ok(anth.indexOf('<svg class="model-icon"') !== -1, anth);
  // No leading O — the splat says Claude, and 'O4.8' read as '04.8'.
  assert.ok(anth.indexOf('O4.8') === -1, anth);

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
  assert.strictEqual(after.label, '4.8');
});

// The subscript no longer names the family, so the hover has to — Opus 4.5 and
// Sonnet 4.5 paint the same badge and are told apart here.
test('tooltip carries the full model for hover truth', function () {
  const p = MP.modelPrefix({ provider: 'claude', model: 'claude-opus-4-5-20250929' });
  assert.strictEqual(p.title, 'Anthropic · claude-opus-4-5-20250929');
  const s = MP.modelPrefix({ provider: 'claude', model: 'claude-sonnet-4-5-20250929' });
  assert.strictEqual(s.title, 'Anthropic · claude-sonnet-4-5-20250929');
  assert.notStrictEqual(p.title, s.title);
});

test('label and title are HTML-escaped', function () {
  const html = MP.modelPrefixHtml({ provider: 'claude', model: 'opus"><img src=x>' });
  assert.ok(html.indexOf('<img') === -1, html);
  assert.ok(html.indexOf('&quot;') !== -1, html);
});

// Product wiring: the RHS fleet tree must paint the prefix before the bare
// name, or the mapping above is a helper nobody calls.
test('index.html wires the prefix ahead of the agent name', function () {
  const html = indexHtml();
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
  const html = indexHtml();
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

// 🎯T299: the version was body-aligned with the mark, so it read as a second
// word beside it rather than a subscript hanging under it. Two declarations
// carry that between them — the cross-axis alignment that levels the two
// boxes, and the offset that drops the version below the level. The rule text
// is enough to compute where the subscript's foot lands relative to the
// mark's, which is the whole claim.
test('subscript hangs below the mark baseline', function () {
  const html = indexHtml();
  const badge = /\.agent-node \.model-badge \{([^}]*)\}/.exec(html);
  assert.ok(badge, '.model-badge rule missing');
  const align = /align-items:\s*([a-z-]+)/.exec(badge[1]);
  assert.ok(align, 'badge declares no align-items: ' + badge[1].trim());
  // Centring puts the version beside the mark's middle; the subscript has to
  // start level with the mark's foot for the offset below to read as a drop.
  assert.ok(/^(flex-end|baseline|last baseline)$/.test(align[1]),
    'version is not levelled with the mark foot: align-items: ' + align[1]);

  const iconRule = /\.agent-node \.model-badge \.model-icon \{([^}]*)\}/.exec(html);
  assert.ok(iconRule, '.model-icon rule missing');
  const iconH = /height:\s*([\d.]+)px/.exec(iconRule[1]);
  assert.ok(iconH, 'icon declares no pixel height: ' + iconRule[1].trim());

  const sub = /\.agent-node \.model-badge sub \{([^}]*)\}/.exec(html);
  assert.ok(sub, '.model-badge sub rule missing');
  // Relative, so the drop is visual only and the row keeps its height.
  assert.ok(/position:\s*relative/.test(sub[1]), 'subscript is not relatively placed: ' + sub[1].trim());
  const fontPx = /font-size:\s*([\d.]+)px/.exec(sub[1]);
  assert.ok(fontPx, 'subscript declares no pixel font-size: ' + sub[1].trim());
  const bottom = /bottom:\s*(-?[\d.]+)em/.exec(sub[1]);
  assert.ok(bottom, 'subscript declares no em offset: ' + sub[1].trim());

  // `bottom` on a relatively placed box moves it *down* when negative.
  const dropPx = -Number(bottom[1]) * Number(fontPx[1]);
  assert.ok(dropPx > 1,
    'subscript foot is level with the mark, not below it: ' + dropPx.toFixed(2) + 'px');
  // ...and not so far that it falls out of the 6px-padded row.
  assert.ok(dropPx < Number(iconH[1]) / 3,
    'subscript is dropped clear of the mark: ' + dropPx.toFixed(2) + 'px');
});
