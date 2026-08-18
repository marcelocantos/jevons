// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for the fleet model prefix (🎯T287).
//
//   node web/scripts/model_prefix_test.js

'use strict';

const assert = require('assert');
const crypto = require('crypto');
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

// The page's single <style> block, as { selector, body } rules. Bounded to the
// stylesheet so script braces cannot masquerade as CSS.
function cssRules() {
  const html = indexHtml();
  const open = html.indexOf('<style>');
  const close = html.indexOf('</style>');
  assert.ok(open !== -1 && close > open, 'index.html has no <style> block');
  // Comments out first: they sit between rules, so an unstripped one would be
  // captured as part of the next selector and a rule could pass a selector
  // assertion on the strength of its own comment text.
  const css = html.slice(open + '<style>'.length, close).replace(/\/\*[\s\S]*?\*\//g, '');
  const rules = [];
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  while ((m = re.exec(css))) rules.push({ sel: m[1].trim(), body: m[2] });
  return rules;
}

console.log('model_prefix_test (🎯T287)');

// 🎯T302 reverses 🎯T299's deletion of the family initial. T299 read 'O5' as a
// zero-padded five and cut the letter; the owner's correction is that the O was
// never a numeral — it is Opus, and without it Opus 4.5 and Sonnet 4.5 painted
// the same badge. Each Anthropic family carries its own letter again.
test('anthropic models carry the family initial ahead of the version', function () {
  assert.strictEqual(MP.condenseModel('claude-opus-4-8'), 'O4.8');
  assert.strictEqual(MP.condenseModel('claude-opus-5'), 'O5');
  assert.strictEqual(MP.condenseModel('claude-opus-5[1m]'), 'O5');
  assert.strictEqual(MP.condenseModel('us.anthropic.claude-opus-4-5-v1:0'), 'O4.5');
  assert.strictEqual(MP.condenseModel('claude-sonnet-4-5-20250929'), 'S4.5');
  assert.strictEqual(MP.condenseModel('claude-haiku-4-5-20251001'), 'H4.5');
  // One letter per family, and three different ones — the whole point of
  // restoring it is that the three stop colliding.
  assert.strictEqual(MP.familyInitial('claude-opus-5'), 'O');
  assert.strictEqual(MP.familyInitial('claude-sonnet-5'), 'S');
  assert.strictEqual(MP.familyInitial('claude-haiku-5'), 'H');
  assert.strictEqual(MP.familyInitial('claude-fable-5'), 'F');
  const badges = ['opus', 'sonnet', 'haiku', 'fable'].map(function (f) {
    return MP.condenseModel('claude-' + f + '-4-5');
  });
  assert.strictEqual(new Set(badges).size, 4, 'two families paint the same badge: ' + badges);
  // The version half stays honest: a family with no version paints the letter
  // and nothing else, rather than inventing a number.
  assert.strictEqual(MP.condenseModel('opus'), 'O');
  assert.strictEqual(MP.versionOf('opus'), '');
});

// 🎯T298 / 🎯T299 / 🎯T302: the reading failure was real — at 9px an O butted
// against digits can pass for a zero — but deleting the letter was the wrong
// fix. The letter stays and the *digits* carry no padding of their own, so
// nothing in the label is a zero that should not be there.
test('no digit segment can read as zero-padded', function () {
  ['claude-opus-5', 'claude-opus-05', 'claude-opus-5[1m]', 'claude-opus-4-8',
    'claude-opus-4-05', 'claude-opus-4-5-20250929', 'claude-sonnet-04-05',
    'claude-haiku-4-5-20251001', 'us.anthropic.claude-opus-4-5-v1:0',
    'claude-fable-5', 'claude-fable-5[1m]', 'claude-fable-05',
    'grok-4.5-build', 'grok-05',
  ].forEach(function (id) {
    const version = MP.versionOf(id);
    assert.ok(!/(^|\.)0\d/.test(version), id + ' → padded segment: ' + version);
    // The version half is digits and dots only; the letter is a separate
    // field, never spliced into the number.
    assert.ok(/^[\d.]*$/.test(version), id + ' → non-numeric version ' + version);
    const label = MP.condenseModel(id);
    assert.strictEqual(label, MP.familyInitial(id) + version, id);
    assert.ok(/^[OSHF]?[\d.]*$/.test(label), id + ' → unexpected label ' + label);
  });
});

// 🎯T302: the initial is not glued into the digit run — it is its own element,
// which is what lets the CSS below weight and colour it apart. That separation
// *is* the O/0 fix; if the letter ever lands as a bare character inside the
// subscript text, the ambiguity T299 complained about is back.
test('the family initial is painted as its own element, not spliced into the digits', function () {
  const html = MP.modelPrefixHtml({ provider: 'claude', model: 'claude-opus-5' });
  assert.ok(html.indexOf('<sub><span class="model-family">O</span>5</sub>') !== -1, html);
  // Never a raw 'O5' text run.
  assert.ok(!/<sub>[^<]*O/.test(html), 'initial is loose in the subscript text: ' + html);

  // A family with no version still gets its letter, wrapped the same way.
  const bare = MP.modelPrefixHtml({ provider: 'claude', model: 'opus' });
  assert.ok(bare.indexOf('<sub><span class="model-family">O</span></sub>') !== -1, bare);
});

// 🎯T312: the owner's own PO row moved onto claude-fable-5 and painted a bare
// splat — the family list knew opus/sonnet/haiku only, so an unknown family word
// condensed to '' and took the version down with it. Fable is an Anthropic
// family like any other: initial F, version on whichever side of the word it
// sits, and the same chrome the Opus O wears.
test('fable condenses to F5 like the Opus O5', function () {
  assert.strictEqual(MP.condenseModel('claude-fable-5'), 'F5');
  assert.strictEqual(MP.condenseModel('fable-5'), 'F5');
  assert.strictEqual(MP.condenseModel('claude-fable-5[1m]'), 'F5');
  assert.strictEqual(MP.condenseModel('claude-fable-4-5'), 'F4.5');
  assert.strictEqual(MP.condenseModel('claude-fable-5-20260601'), 'F5');
  // Version leads the family word on the 3-series spelling (🎯T302's fix
  // applies here too — the family is new, the extractor is not).
  assert.strictEqual(MP.condenseModel('claude-3-5-fable'), 'F3.5');
  // A family with no version paints the bare letter, never an invented number.
  assert.strictEqual(MP.condenseModel('fable'), 'F');
  assert.strictEqual(MP.versionOf('fable'), '');

  // The mark is the Claude splat: fable is Anthropic whether the provider says
  // so or only the model id does.
  assert.strictEqual(MP.companyFor('claude', 'claude-fable-5'), 'anthropic');
  assert.strictEqual(MP.companyFor('', 'claude-fable-5'), 'anthropic');
  assert.strictEqual(MP.companyFromModel('fable-5'), 'anthropic');

  // Same chrome as O5: the letter is its own accented element, the digits are
  // plain, and the splat rides beside them.
  const html = MP.modelPrefixHtml({ provider: 'claude', model: 'claude-fable-5' });
  assert.ok(html.indexOf('data-company="anthropic"') !== -1, html);
  assert.ok(html.indexOf('data-mark="claude-splat"') !== -1, html);
  assert.ok(html.indexOf('<sub><span class="model-family">F</span>5</sub>') !== -1, html);
  assert.ok(!/<sub>[^<]*F/.test(html), 'initial is loose in the subscript text: ' + html);
  assert.ok(html.indexOf('title="Anthropic · claude-fable-5"') !== -1, html);

  const p = MP.modelPrefix({ provider: 'claude', model: 'claude-fable-5' });
  assert.strictEqual(p.initial, 'F');
  assert.strictEqual(p.version, '5');
  assert.strictEqual(p.label, 'F5');
});

// 🎯T348: the owner's jv-t347-reload-pin row wore the bare splat while its
// model was known the whole time — the exact failure 🎯T312 fixed for fable,
// reachable again by any family word the FAMILIES list has not met yet. The
// list is now a styling preference, not a load-bearing gate: an unlisted
// family word in a 'claude-…' id is sniffed from the id itself and painted
// with its own initial, and a family-less id still paints its version. A
// Claude row with a known model never condenses to nothing.
test('an unlisted Anthropic family still paints initial + version, never a bare mark', function () {
  // A family name we have never shipped a FAMILY_INITIAL entry for.
  assert.strictEqual(MP.condenseModel('claude-zephyr-6'), 'Z6');
  assert.strictEqual(MP.familyInitial('claude-zephyr-6'), 'Z');
  assert.strictEqual(MP.versionOf('claude-zephyr-6'), '6');
  assert.strictEqual(MP.condenseModel('claude-zephyr-4-5'), 'Z4.5');
  // Bedrock spelling: family sniffed past the prefix, version stops at 'v1'.
  assert.strictEqual(MP.condenseModel('us.anthropic.claude-nova-4-5-v1:0'), 'N4.5');
  // Family-less ids paint the bare version — it is real, and beats a mark alone.
  assert.strictEqual(MP.condenseModel('claude-2'), '2');
  assert.strictEqual(MP.condenseModel('claude-2.1'), '2.1');
  assert.strictEqual(MP.familyInitial('claude-2'), '');
  // 'claude' alone carries no version to repeat: mark-only remains correct.
  assert.strictEqual(MP.condenseModel('claude'), '');

  // The sniffed family paints the same chrome as a listed one.
  const html = MP.modelPrefixHtml({ provider: 'claude', model: 'claude-zephyr-6' });
  assert.ok(html.indexOf('data-mark="claude-splat"') !== -1, html);
  assert.ok(html.indexOf('<sub><span class="model-family">Z</span>6</sub>') !== -1, html);

  // Production ids: any Claude id that names a version condenses to something.
  ['claude-opus-5', 'claude-sonnet-5', 'claude-haiku-4-5-20251001',
   'claude-fable-5', 'claude-fable-5[1m]', 'claude-zephyr-6',
   'claude-3-5-sonnet-20240620', 'claude-2'].forEach(function (id) {
    assert.notStrictEqual(MP.condenseModel(id), '', id + ' condensed to nothing');
    const p = MP.modelPrefix({ provider: 'claude', model: id });
    assert.notStrictEqual(p.label, '', id + ' painted a bare mark');
  });

  // Sniffing is an Anthropic affair: unknown non-Claude ids stay untouched.
  assert.strictEqual(MP.condenseModel('mystery-9'), '');
  assert.strictEqual(MP.familyInitial('grok-4.5'), '');
  assert.strictEqual(MP.familyInitial('gpt-5'), '');
});

// 🎯T302 restores the initial for Anthropic only. xAI ships one family, so a
// 'G' would name nothing the mark does not already say — Grok rows keep exactly
// the bare version 🎯T299 gave them.
test('grok drops the family letter — one flavour', function () {
  assert.strictEqual(MP.condenseModel('grok-4.5'), '4.5');
  assert.strictEqual(MP.condenseModel('grok-4'), '4');
  assert.strictEqual(MP.condenseModel('grok'), '');
  assert.strictEqual(MP.familyInitial('grok-4.5'), '');
  assert.strictEqual(MP.familyInitial('gpt-5.1'), '');
  // No letter element reaches a Grok badge at all.
  const html = MP.modelPrefixHtml({ provider: 'grok', model: 'grok-4.5' });
  assert.ok(html.indexOf('model-family') === -1, 'grok badge carries a family letter: ' + html);
});

// 🎯T323: provider wins for the company mark; family/version only when the
// model id belongs to that company. Owner evidence after mass claude→grok
// migrate: Grok mark + F (sticky model=fable) — fail closed to mark alone.
test('migrate residue fable under grok paints mark only — never Grok+F', function () {
  ['fable', 'claude-fable-5', 'claude-opus-4-8', 'opus', 'sonnet-4-5'].forEach(function (foreign) {
    const p = MP.modelPrefix({ provider: 'grok', model: foreign });
    assert.strictEqual(p.company, 'xai', foreign + ' company');
    assert.strictEqual(p.initial, '', foreign + ' initial');
    assert.strictEqual(p.version, '', foreign + ' version');
    assert.strictEqual(p.label, '', foreign + ' label');
    const html = MP.modelPrefixHtml({ provider: 'grok', model: foreign });
    assert.ok(html.indexOf('data-company="xai"') !== -1, foreign + ' html company: ' + html);
    assert.ok(html.indexOf('data-mark="grok"') !== -1, foreign + ' html mark: ' + html);
    assert.ok(html.indexOf('model-family') === -1, foreign + ' painted a family letter: ' + html);
    assert.ok(html.indexOf('<sub>') === -1, foreign + ' painted a version sub: ' + html);
    // Tooltip must not advertise the foreign Anthropic id beside the Grok mark.
    assert.ok(html.indexOf(foreign) === -1, foreign + ' leaked into tooltip: ' + html);
  });
  assert.strictEqual(MP.modelMatchesCompany('xai', 'fable'), false);
  assert.strictEqual(MP.modelMatchesCompany('xai', 'grok-4.5-build'), true);
  assert.strictEqual(MP.modelMatchesCompany('anthropic', 'fable'), true);
});

// 🎯T323 acceptance trio (product cases the owner named):
//   sticky fable under grok → mark alone
//   empty model → mark alone
//   grok-4.5-build → 4.5
test('T323 product cases: residue / empty / grok-4.5-build', function () {
  const residue = MP.modelPrefixHtml({ provider: 'grok', model: 'fable' });
  assert.ok(residue.indexOf('data-mark="grok"') !== -1, residue);
  assert.ok(residue.indexOf('<sub>') === -1, 'residue painted version: ' + residue);
  assert.ok(residue.indexOf('F') === -1, 'residue painted F: ' + residue);

  const empty = MP.modelPrefixHtml({ provider: 'grok', model: '' });
  assert.ok(empty.indexOf('data-mark="grok"') !== -1, empty);
  assert.ok(empty.indexOf('<sub>') === -1, 'empty painted version: ' + empty);

  const build = MP.modelPrefix({ provider: 'grok', model: 'grok-4.5-build' });
  assert.strictEqual(build.company, 'xai');
  assert.strictEqual(build.initial, '');
  assert.strictEqual(build.version, '4.5');
  assert.strictEqual(build.label, '4.5');
  const buildHtml = MP.modelPrefixHtml({ provider: 'grok', model: 'grok-4.5-build' });
  assert.ok(buildHtml.indexOf('<sub>4.5</sub>') !== -1, buildHtml);
});

// Symmetric fail-closed: Grok model under Claude mark is also mark-only.
test('foreign grok model under claude paints mark only', function () {
  const p = MP.modelPrefix({ provider: 'claude', model: 'grok-4.5-build' });
  assert.strictEqual(p.company, 'anthropic');
  assert.strictEqual(p.label, '');
  const html = MP.modelPrefixHtml({ provider: 'claude', model: 'grok-4.5-build' });
  assert.ok(html.indexOf('data-company="anthropic"') !== -1, html);
  assert.ok(html.indexOf('<sub>') === -1, html);
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

// 🎯T507: OpenAI retired the Codex-specific >_ cloud and this repo's
// hand-built substitute. Pin the ChatGPT logo geometry from Wikimedia Commons
// so a locally plausible approximation cannot silently replace it again.
test('openai wears the real ChatGPT mark from Wikimedia Commons', function () {
  const icon = MP.companyIconHtml('openai');
  assert.ok(icon.indexOf('data-mark="chatgpt"') !== -1, icon);
  assert.ok(icon.indexOf('viewBox="0 0 320 320"') !== -1, icon);
  const d = /<path[^>]*\bd="([^"]+)"/.exec(icon);
  assert.ok(d, 'no path data in the ChatGPT mark: ' + icon);
  assert.strictEqual(d[1].length, 1655, 'ChatGPT path length drifted');
  assert.strictEqual(
    crypto.createHash('sha256').update(d[1]).digest('hex'),
    '9499e7547042a397882e0d96308f4795ead582b2e8502e062f57bcbc783ce1bb',
    'OpenAI path is not File:ChatGPT-Logo.svg');
  assert.ok(!/<(circle|rect|ellipse|polygon|polyline)\b/.test(icon),
    'mark carries a plate or cloud chrome: ' + icon);
  assert.ok(/fill="currentColor"/.test(icon), 'mark ignores row colour: ' + icon);

  const badge = MP.modelPrefixHtml({ provider: 'codex', model: 'gpt-5.6' });
  assert.ok(badge.indexOf('data-company="openai"') !== -1, badge);
  assert.ok(badge.indexOf('data-mark="chatgpt"') !== -1, badge);
});

// 🎯T508: provider identity is additional chrome, not a replacement for the
// vendor identity. Pin the supplied asset and the provider → vendor → label
// order so a future company-map refactor cannot collapse Bedrock into Claude.
test('bedrock paints the supplied provider mark before the vendor mark', function () {
  const asset = fs.readFileSync(path.join(__dirname, '..', 'assets', 'bedrock.svg'), 'utf8').trim();
  assert.strictEqual(
    crypto.createHash('sha256').update(asset).digest('hex'),
    'b07f07bb6b77a1ee83916430250dc9adfc799ec43fbc5932947af099bcfcb9f9',
    'tracked Bedrock SVG differs from the owner-supplied asset');

  const badge = MP.modelPrefixHtml({ provider: 'bedrock', model: 'claude-opus-4-8' });
  const provider = badge.indexOf('data-mark="amazon-bedrock"');
  const vendor = badge.indexOf('data-mark="claude-splat"');
  const label = badge.indexOf('<sub>');
  assert.ok(provider !== -1 && vendor !== -1 && label !== -1, badge);
  assert.ok(provider < vendor && vendor < label, 'want provider → vendor → label: ' + badge);
  assert.ok(/mask:\s*url\('assets\/bedrock\.svg'\)/.test(indexHtml()),
    'Bedrock mark is not painted from the tracked asset');
});

test('bedrock provider mark is explicit and leaves other providers unchanged', function () {
  assert.strictEqual(MP.providerIconHtml('bedrock').indexOf('amazon-bedrock') !== -1, true);
  ['claude', 'grok', 'codex', '', 'amazon-bedrock'].forEach(function (provider) {
    assert.strictEqual(MP.providerIconHtml(provider), '', provider);
  });
  const inferred = MP.modelPrefixHtml({ provider: 'claude', model: 'bedrock/claude-opus-4-8' });
  assert.strictEqual(inferred.indexOf('amazon-bedrock'), -1, inferred);
  const claude = MP.modelPrefixHtml({ provider: 'claude', model: 'claude-opus-4-8' });
  assert.strictEqual((claude.match(/class="model-icon/g) || []).length, 1, claude);
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
  assert.strictEqual(MP.versionOf('claude-opus-4-05'), '4.5');
  assert.strictEqual(MP.versionOf('claude-opus-05'), '5');
  assert.strictEqual(MP.versionOf('claude-sonnet-04-05'), '4.5');
  assert.strictEqual(MP.versionOf('grok-05'), '5');
  assert.strictEqual(MP.versionOf('claude-opus-000-007'), '0.7');
  // Internal zeros survive: 10 is ten, not one.
  assert.strictEqual(MP.versionOf('claude-opus-10'), '10');
  assert.strictEqual(MP.versionOf('claude-opus-4-10'), '4.10');
  assert.strictEqual(MP.versionOf('grok-10.0'), '10.0');
  // A lone zero is a real segment, not padding.
  assert.strictEqual(MP.versionOf('claude-opus-5-0'), '5.0');
  // A release date is still a date, not a version segment.
  assert.strictEqual(MP.versionOf('claude-opus-4-5-20250929'), '4.5');
  // Restoring the initial did not re-pad anything: the whole label is the
  // letter and the unpadded version, nothing else.
  assert.strictEqual(MP.condenseModel('claude-opus-4-05'), 'O4.5');
  assert.strictEqual(MP.condenseModel('claude-sonnet-04-05'), 'S4.5');
});

// 🎯T302: Anthropic has spelled the version on both sides of the family word.
// The extractor read only the tail, so every 3-series id — where the version
// *leads* — condensed to nothing and painted a bare mark with no version at
// all. That is the "why do some Claude rows show no version" the owner saw.
test('version parses on either side of the family word', function () {
  assert.strictEqual(MP.versionOf('claude-3-5-sonnet'), '3.5');
  assert.strictEqual(MP.condenseModel('claude-3-5-sonnet'), 'S3.5');
  assert.strictEqual(MP.condenseModel('claude-3-5-sonnet-20241022'), 'S3.5');
  assert.strictEqual(MP.condenseModel('claude-3-opus'), 'O3');
  assert.strictEqual(MP.condenseModel('claude-3-haiku-20240307'), 'H3');
  assert.strictEqual(MP.condenseModel('us.anthropic.claude-3-5-sonnet-v1:0'), 'S3.5');
  // A trailing version still wins where the id carries one — the current
  // naming puts it there.
  assert.strictEqual(MP.condenseModel('claude-opus-4-5'), 'O4.5');
  // A date on the leading side is a date on that side too.
  assert.strictEqual(MP.versionOf('claude-20240620-sonnet'), '');
  // Nothing on either side is still nothing — no version is invented.
  assert.strictEqual(MP.versionOf('claude-sonnet'), '');
  assert.strictEqual(MP.versionOf('opus'), '');
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
  const anth = MP.modelPrefixHtml({ name: 'jv-worker', provider: 'claude', model: 'claude-opus-4-8' });
  assert.ok(anth.indexOf('data-company="anthropic"') !== -1, anth);
  assert.ok(anth.indexOf('<sub><span class="model-family">O</span>4.8</sub>') !== -1, anth);
  assert.ok(anth.indexOf('<svg class="model-icon"') !== -1, anth);
  assert.ok(anth.indexOf('<button type="button"') === 0, anth);
  assert.ok(anth.indexOf('aria-label="Select provider and model for jv-worker.') !== -1, anth);
  assert.ok(anth.endsWith('</button>'), anth);

  const grok = MP.modelPrefixHtml({ provider: 'grok', model: 'grok-4.5' });
  assert.ok(grok.indexOf('data-company="xai"') !== -1, grok);
  assert.ok(grok.indexOf('<sub>4.5</sub>') !== -1, grok);
  // No leading G — only one Grok flavour.
  assert.ok(grok.indexOf('G4.5') === -1, grok);
});

test('T506 badge CSS declares a readable label and minimum pointer target', function () {
  const html = indexHtml();
  const badge = /\.agent-node \.model-badge \{([^}]*)\}/.exec(html);
  assert.ok(badge, '.model-badge rule missing');
  assert.ok(/min-width:\s*32px/.test(badge[1]), 'badge lacks 32px minimum width');
  assert.ok(/min-height:\s*32px/.test(badge[1]), 'badge lacks 32px minimum height');
  const sub = /\.agent-node \.model-badge sub \{([^}]*)\}/.exec(html);
  assert.ok(sub, '.model-badge sub rule missing');
  assert.ok(/font-size:\s*12px/.test(sub[1]), 'model label is smaller than 12px');
  assert.ok(/\.agent-node \.model-badge:focus-visible\s*\{[^}]*outline:/.test(html),
    'keyboard focus indicator missing');
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
  assert.strictEqual(before.initial, '');
  assert.strictEqual(after.company, 'anthropic');
  assert.strictEqual(after.label, 'O4.8');
  assert.strictEqual(after.initial, 'O');
  assert.strictEqual(after.version, '4.8');
});

// The subscript names the family again (🎯T302), but the hover still carries
// the full id — the badge is two characters and the date, suffix and vendor
// prefix live only here.
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

// 🎯T302: restoring the letter only works if the letter does not read as a
// digit. That is carried entirely by the CSS — bolder than the digits beside
// it, and painted in the --accent *token* rather than a literal colour. The
// token matters: --accent is purple in the light palette and red in the dark
// in-car one, so only the token keeps the badge's letter matching the target
// ids the frontier table paints beside it.
test('the family initial is bold and carries the accent token', function () {
  const html = indexHtml();
  const rule = /\.agent-node \.model-badge sub \.model-family \{([^}]*)\}/.exec(html);
  assert.ok(rule, '.model-family rule missing — the initial paints as plain digits');

  const colour = /color:\s*([^;]+);/.exec(rule[1]);
  assert.ok(colour, 'family initial declares no colour: ' + rule[1].trim());
  assert.strictEqual(colour[1].trim(), 'var(--accent)',
    'family initial must ride the --accent token, not a fixed colour: ' + colour[1].trim());

  const weight = /font-weight:\s*(\d+)/.exec(rule[1]);
  assert.ok(weight, 'family initial declares no font-weight: ' + rule[1].trim());
  const sub = /\.agent-node \.model-badge sub \{([^}]*)\}/.exec(html);
  const subWeight = /font-weight:\s*(\d+)/.exec(sub[1]);
  assert.ok(subWeight, 'subscript declares no font-weight: ' + sub[1].trim());
  assert.ok(Number(weight[1]) >= 700, 'family initial is not bold: ' + weight[0]);
  assert.ok(Number(weight[1]) > Number(subWeight[1]),
    'family initial is no heavier than the digits it must be told apart from: '
    + weight[0] + ' vs ' + subWeight[0]);

  // Both palettes define --accent, so the letter is never left unpainted.
  const decls = html.match(/--accent:\s*[^;]+;/g) || [];
  assert.ok(decls.length >= 2, '--accent is not defined per palette: ' + decls.join(' '));
});

// 🎯T302: Grok reads as a faded version of its standard *black* mark. Claude
// must read the same way in its own brand colour — a faded Claude *orange*.
test('the Claude mark paints brand orange, not the muted row colour', function () {
  const html = indexHtml();
  // Brand hex from the same Simple Icons record that supplies the path:
  // icon "Claude", hex D97757.
  const token = /--anthropic-mark:\s*(#[0-9a-fA-F]{6})\s*;/.exec(html);
  assert.ok(token, 'no --anthropic-mark token — the splat inherits the gray row colour');
  assert.strictEqual(token[1].toLowerCase(), '#d97757',
    'Claude mark is not the Simple Icons brand hex D97757: ' + token[1]);

  // ...and it really is a warm orange, not a colour that merely got named one.
  const hex = token[1].slice(1);
  const r = parseInt(hex.slice(0, 2), 16);
  const g = parseInt(hex.slice(2, 4), 16);
  const b = parseInt(hex.slice(4, 6), 16);
  assert.ok(r > g && g > b, 'mark colour is not a warm orange ramp: ' + token[1]);
  assert.ok(r - b > 60, 'mark colour is too gray to read as orange: ' + token[1]);

  // The token has to reach the Claude mark, keyed on the mark itself.
  const tinted = cssRules().filter(function (rule) {
    return /var\(--anthropic-mark\)/.test(rule.body);
  });
  assert.ok(tinted.length > 0, '--anthropic-mark is defined but never applied');
  tinted.forEach(function (rule) {
    assert.ok(/claude-splat/.test(rule.sel),
      'brand orange leaks onto a non-Claude mark: ' + rule.sel);
    assert.ok(/(^|[;{\s])color:\s*var\(--anthropic-mark\)/.test(rule.body),
      'brand orange is not applied as the glyph colour: ' + rule.body.trim());
  });

  // The colour only lands if the strokes still take it from CSS.
  assert.ok(/fill="currentColor"/.test(MP.companyIconHtml('anthropic')),
    'Claude path does not follow the CSS colour');

  // Same fade as Grok: one opacity rule covers both marks.
  const iconRule = /\.agent-node \.model-badge \.model-icon \{([^}]*)\}/.exec(html);
  assert.ok(iconRule, '.model-icon rule missing');
  const op = /opacity:\s*([\d.]+)/.exec(iconRule[1]);
  assert.ok(op, 'marks declare no opacity — neither reads as faded: ' + iconRule[1].trim());
  assert.ok(Number(op[1]) < 1, 'mark is not faded: opacity ' + op[1]);
});

// 🎯T302, the owner's named failure class: the orange belongs on the splat's
// strokes over transparent ground. NOT an orange plate with a light or dark
// mark knocked out of it — that is the vendor's published lockup and the
// inverse of what this badge wants. Grok is a gray path on nothing; Claude is
// an orange path on nothing.
test('the Claude mark is a glyph on transparent ground — no plate, no knock-out', function () {
  const icon = MP.companyIconHtml('anthropic');
  // Nothing behind the glyph: a plate would be a rect/circle/ellipse/polygon,
  // and a full-bleed backdrop path would be a second path.
  assert.ok(!/<(rect|circle|ellipse|polygon|polyline)\b/.test(icon),
    'mark carries a plate element: ' + icon);
  assert.strictEqual((icon.match(/<path\b/g) || []).length, 1,
    'mark has more than one path — the extra one is a backdrop: ' + icon);
  // Every fill in the mark is the glyph colour; none is a hardcoded plate or a
  // knocked-out light mark.
  const fills = icon.match(/fill="[^"]*"/g) || [];
  assert.ok(fills.length > 0, 'mark declares no fill at all: ' + icon);
  fills.forEach(function (f) {
    assert.strictEqual(f, 'fill="currentColor"', 'mark paints a literal fill: ' + f);
  });
  // No ground painted on the svg element itself, either.
  assert.ok(!/\b(background|fill-opacity|style=)/.test(icon.slice(0, icon.indexOf('>'))),
    'svg element paints its own ground: ' + icon);

  // ...and none painted by CSS behind the badge or the mark.
  cssRules().filter(function (rule) {
    // T508's separate Bedrock provider glyph is intentionally a monochrome
    // SVG mask; its background-color paints the masked strokes, not a plate
    // behind the Claude vendor mark this test protects.
    return /model-badge|model-icon|claude-splat/.test(rule.sel)
      && !/model-provider-icon/.test(rule.sel);
  }).forEach(function (rule) {
    assert.ok(!/(^|[;{\s])background(-color)?:/.test(rule.body),
      'badge CSS paints a plate behind the mark: ' + rule.sel + ' {' + rule.body.trim() + '}');
    assert.ok(!/(^|[;{\s])border(-radius)?:/.test(rule.body),
      'badge CSS gives the mark a tile: ' + rule.sel + ' {' + rule.body.trim() + '}');
  });
});

// 🎯T506 lifts the selector from barely-visible text-muted to readable
// text-dim. Grok remains its own neutral mark — never tinted orange or given
// a family letter.
test('Grok uses readable neutral row colour, no orange, no letter', function () {
  const html = indexHtml();
  const badge = /\.agent-node \.model-badge \{([^}]*)\}/.exec(html);
  assert.ok(badge, '.model-badge rule missing');
  const colour = /(^|[;{\s])color:\s*([^;]+);/.exec(badge[1]);
  assert.ok(colour, 'badge declares no default colour: ' + badge[1].trim());
  assert.strictEqual(colour[2].trim(), 'var(--text-dim)',
    'the default selector colour is not the readable row colour: ' + colour[2].trim());

  // No rule tints the Grok mark, by data-mark or by company.
  cssRules().forEach(function (rule) {
    if (!/data-mark="grok"|data-company="xai"/.test(rule.sel)) return;
    assert.ok(!/(^|[;{\s])color:/.test(rule.body),
      'Grok mark is recoloured: ' + rule.sel + ' {' + rule.body.trim() + '}');
  });

  const grok = MP.modelPrefixHtml({ provider: 'grok', model: 'grok-4.5-build' });
  assert.ok(grok.indexOf('data-mark="grok"') !== -1, grok);
  assert.ok(grok.indexOf('<sub>4.5</sub>') !== -1, 'Grok subscript changed shape: ' + grok);
  assert.ok(grok.indexOf('model-family') === -1, 'Grok grew a family letter: ' + grok);
  assert.ok(grok.indexOf('anthropic-mark') === -1, 'Grok badge references the Claude tint: ' + grok);
});
