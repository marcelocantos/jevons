// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for Wispr Flow composer context (🎯T21 / 🎯T133 / 🎯T183).
// Run: node web/scripts/wispr_context_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const WC = require('./wispr_context.js');

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

// ── context text (empty history still sentence-shaped) ───────────

test('buildContextText: empty turns → non-empty sentence-shaped bootstrap', function () {
  const t = WC.buildContextText([]);
  assert.ok(t.length > 0, 'context must be non-empty for empty composer');
  assert.ok(WC.isSentenceShaped(t) || /[.!?]/.test(t), 'bootstrap must be sentence-shaped');
  assert.ok(/question mark/i.test(t) || /\?/.test(t), 'bootstrap should mention or use ? style cue');
  assert.ok(/[A-Z]/.test(t), 'must include capital letters');
});

test('buildContextText: includes last N owner/assistant excerpts', function () {
  const turns = [
    { role: 'user', text: 'are we tracking progress on the po lane' },
    { role: 'jevons', text: 'yes — T21 is on the frontier and ready to implement' },
    { role: 'user', text: 'what about the seed strip path' },
    { role: 'assistant', text: 'prepareWireText removes only the empty seed' },
  ];
  const t = WC.buildContextText(turns, { maxTurns: 4 });
  assert.ok(t.indexOf('Owner:') >= 0, 'must label owner turns');
  assert.ok(t.indexOf('Assistant:') >= 0, 'must label assistant turns');
  assert.ok(/Progress|progress|Tracking|tracking|Seed|seed|Wire|wire/i.test(t));
  // Excerpts are capitalised / punctuated for document mode.
  const ownerLines = t.split('\n').filter(function (l) { return l.indexOf('Owner:') === 0; });
  assert.ok(ownerLines.length >= 1);
  ownerLines.forEach(function (line) {
    const body = line.replace(/^Owner:\s*/, '');
    assert.ok(/^[A-Z]/.test(body) || WC.isSentenceShaped(body), 'owner excerpt sentence-shaped: ' + body);
  });
});

test('buildContextText: maxTurns keeps only recent slice', function () {
  const turns = [];
  for (let i = 0; i < 10; i++) {
    turns.push({ role: 'user', text: 'message number ' + i + ' is here' });
  }
  const t = WC.buildContextText(turns, { maxTurns: 2 });
  assert.ok(t.indexOf('number 8') >= 0 || t.indexOf('Number 8') >= 0);
  assert.ok(t.indexOf('number 9') >= 0 || t.indexOf('Number 9') >= 0);
  assert.ok(t.indexOf('number 0') < 0 && t.indexOf('Number 0') < 0);
});

test('ensureSentence capitalises and adds terminal period', function () {
  assert.strictEqual(WC.ensureSentence('hello world'), 'Hello world.');
  assert.strictEqual(WC.ensureSentence('Already done.'), 'Already done.');
  assert.strictEqual(WC.ensureSentence('What now?'), 'What now?');
});

// ── describedby ids ──────────────────────────────────────────────

test('describedByIds joins static hint + live region', function () {
  assert.strictEqual(
    WC.describedByIds(WC.STATIC_HINT_ID, WC.LIVE_REGION_ID),
    'input-hint wispr-context'
  );
  assert.strictEqual(WC.describedByIds('a', 'b'), 'a b');
});

// ── seed policy (fallback B) ─────────────────────────────────────

test('EMPTY_SEED is sentence-shaped and not plain whitespace', function () {
  assert.ok(WC.EMPTY_SEED.indexOf('.') >= 0, 'seed must include period anchor');
  assert.ok(WC.EMPTY_SEED.length > 0);
  assert.ok(WC.needsSeed(''));
  assert.ok(WC.needsSeed('   '));
  assert.ok(WC.needsSeed(WC.EMPTY_SEED));
  assert.ok(!WC.needsSeed('Hello world?'));
});

test('applySeedIfEmpty seeds empty; leaves real draft', function () {
  assert.strictEqual(WC.applySeedIfEmpty(''), WC.EMPTY_SEED);
  assert.strictEqual(WC.applySeedIfEmpty(WC.EMPTY_SEED), WC.EMPTY_SEED);
  assert.strictEqual(WC.applySeedIfEmpty('Real draft?'), 'Real draft?');
});

test('prepareWireText strips seed; preserves real user ? and punctuation', function () {
  assert.strictEqual(WC.prepareWireText(WC.EMPTY_SEED), '');
  assert.strictEqual(WC.prepareWireText(WC.EMPTY_SEED + 'are we done'), 'are we done');
  assert.strictEqual(
    WC.prepareWireText(WC.EMPTY_SEED + 'Are you tracking Jevons PO progress?'),
    'Are you tracking Jevons PO progress?'
  );
  // Real punctuation alone (user typed) must not be stripped as seed.
  assert.strictEqual(WC.prepareWireText('What about T21?'), 'What about T21?');
  assert.strictEqual(WC.prepareWireText('Hello.'), 'Hello.');
  assert.strictEqual(WC.prepareWireText('Why? Because.'), 'Why? Because.');
  // Seed-only is effectively empty after strip.
  assert.ok(WC.isEffectivelyEmpty(WC.EMPTY_SEED));
  assert.ok(!WC.isEffectivelyEmpty('?'));
  // 🎯T192: seed-shaped residue wires empty (not a real draft period).
  assert.strictEqual(WC.prepareWireText('.'), '');
  assert.strictEqual(WC.prepareWireText('\u200B'), '');
  assert.strictEqual(WC.prepareWireText('\u200B.'), '');
});

test('stripSeed is prefix-only (no grammar fixer)', function () {
  // Interior period/question must survive.
  const raw = 'Did the seed strip path keep this? Yes.';
  assert.strictEqual(WC.stripSeed(raw), raw);
  assert.strictEqual(WC.prepareWireText(raw), raw);
});

test('seedPrefixLen reports leading EMPTY_SEED length (🎯T149 caret bounds)', function () {
  assert.strictEqual(WC.seedPrefixLen(''), 0);
  assert.strictEqual(WC.seedPrefixLen('plain'), 0);
  assert.strictEqual(WC.seedPrefixLen(WC.EMPTY_SEED), WC.EMPTY_SEED.length);
  assert.strictEqual(WC.seedPrefixLen(WC.EMPTY_SEED + 'hello'), WC.EMPTY_SEED.length);
  assert.strictEqual(WC.seedPrefixLen('x' + WC.EMPTY_SEED), 0);
});

// ── seed-only visibility class (🎯T133) ──────────────────────────

test('isSeedOnly / needsSeedOnlyClass: seed-only vs real draft', function () {
  assert.strictEqual(WC.SEED_ONLY_CLASS, 'composer-seed-only');
  assert.ok(WC.isSeedOnly(WC.EMPTY_SEED), 'EMPTY_SEED is seed-only');
  assert.ok(WC.needsSeedOnlyClass(WC.EMPTY_SEED), 'class on for EMPTY_SEED');
  assert.ok(!WC.isSeedOnly(''), 'blank string is not seed-only (nothing to hide)');
  assert.ok(!WC.needsSeedOnlyClass(''));
  assert.ok(!WC.isSeedOnly('Hello world?'));
  assert.ok(!WC.needsSeedOnlyClass('Hello world?'));
  assert.ok(!WC.isSeedOnly(WC.EMPTY_SEED + 'real draft'));
  assert.ok(!WC.needsSeedOnlyClass(WC.EMPTY_SEED + 'Are you done?'));
  // Whitespace-only still effectively empty → hide if present as value.
  assert.ok(WC.isSeedOnly('   '));
  assert.ok(WC.needsSeedOnlyClass('   '));
  // 🎯T192: bare seed-period / ZWSP residue counts as seed-only (hide + empty).
  assert.ok(WC.isSeedOnly('.'), 'bare period is seed-shaped empty');
  assert.ok(WC.needsSeedOnlyClass('.'));
  assert.ok(WC.isSeedOnly('\u200B'), 'ZWSP-only is seed-shaped empty');
  // Real user punctuation that is not the seed period is not seed-only.
  assert.ok(!WC.isSeedOnly('?'));
});

test('handleSeedBackspace: seed-only Backspace clears whole seed once', function () {
  const act = WC.handleSeedBackspace(WC.EMPTY_SEED, 'Backspace');
  assert.ok(act && act.consume, 'must consume Backspace on seed-only');
  assert.strictEqual(act.value, '');
  assert.strictEqual(
    WC.handleSeedBackspace(WC.EMPTY_SEED, 'Delete'),
    null,
    'Delete is not the seed-only policy key'
  );
  assert.strictEqual(
    WC.handleSeedBackspace('Hello?', 'Backspace'),
    null,
    'real draft Backspace is default browser'
  );
  assert.strictEqual(WC.handleSeedBackspace('', 'Backspace'), null);
  // After clear, prepareWireText still empty; re-seed via applySeedIfEmpty.
  assert.strictEqual(WC.prepareWireText(act.value), '');
  assert.strictEqual(WC.applySeedIfEmpty(act.value), WC.EMPTY_SEED);
});

test('seed-only class state flips when real draft appears; strip unchanged', function () {
  assert.ok(WC.needsSeedOnlyClass(WC.applySeedIfEmpty('')));
  const withReal = WC.EMPTY_SEED + 'What about T133?';
  assert.ok(!WC.needsSeedOnlyClass(WC.applySeedIfEmpty(withReal)));
  assert.strictEqual(WC.prepareWireText(withReal), 'What about T133?');
  assert.strictEqual(WC.prepareWireText(WC.EMPTY_SEED), '');
});

// ── 🎯T192 seed-shaped empty (Alt+Enter / composerEmpty) ─────────

test('T192 isEffectivelyEmpty: EMPTY_SEED, bare ., ZWSP, partial thrash', function () {
  const emptyish = [
    '',
    '   ',
    WC.EMPTY_SEED,
    '.',
    '\u200B',
    '\u200B\u200B',
    '\u200B.',
    '.\u200B',
    WC.EMPTY_SEED + WC.EMPTY_SEED,
    ' \u200B. \u200B ',
  ];
  emptyish.forEach(function (v) {
    assert.ok(
      WC.isEffectivelyEmpty(v),
      'expected effectively empty: ' + JSON.stringify(v)
    );
    assert.strictEqual(
      WC.prepareWireText(v),
      '',
      'wire empty for seed-shaped: ' + JSON.stringify(v)
    );
  });
  // Real drafts stay non-empty.
  assert.ok(!WC.isEffectivelyEmpty('?'));
  assert.ok(!WC.isEffectivelyEmpty('hello'));
  assert.ok(!WC.isEffectivelyEmpty(WC.EMPTY_SEED + 'hi'));
  assert.ok(!WC.isEffectivelyEmpty('Hello.'));
  assert.ok(!WC.isEffectivelyEmpty('..'));
  assert.strictEqual(WC.prepareWireText(WC.EMPTY_SEED + 'hi'), 'hi');
  assert.strictEqual(WC.prepareWireText('Hello.'), 'Hello.');
});

// ── index.html wiring ────────────────────────────────────────────

test('index.html wires wispr helper, live region, describedby, send strip', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/wispr_context.js'), 'must load wispr_context.js');
  assert.ok(html.includes('WisprContext'), 'must reference WisprContext');
  assert.ok(
    html.includes('id="wispr-context"') || html.includes("id='wispr-context'"),
    'live context region #wispr-context must exist'
  );
  assert.ok(
    /aria-describedby\s*=\s*["'][^"']*input-hint[^"']*wispr-context/.test(html) ||
      /aria-describedby\s*=\s*["'][^"']*wispr-context[^"']*input-hint/.test(html) ||
      (html.includes('describedByIds') && html.includes('wispr-context')),
    'aria-describedby must include static hint + live region'
  );
  assert.ok(
    html.includes('prepareWireText') || html.includes('stripSeed'),
    'send path must strip seed before wire'
  );
  assert.ok(
    html.includes('buildContextText') || html.includes('refreshWisprContext'),
    'history update must refresh context'
  );
});

test('index.html has composer-seed-only CSS and class/Backspace wiring (🎯T133)', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(
    html.includes('composer-seed-only'),
    'CSS/class name composer-seed-only must appear'
  );
  assert.ok(
    /composer-seed-only\s*\{[^}]*color\s*:\s*transparent/s.test(html) ||
      html.includes('color: transparent'),
    'seed-only CSS must set color: transparent'
  );
  assert.ok(
    html.includes('caret-color'),
    'seed-only CSS must set caret-color so caret stays visible'
  );
  assert.ok(
    html.includes('syncComposerSeedClass') || html.includes('needsSeedOnlyClass'),
    'must toggle seed-only class from helpers'
  );
  assert.ok(
    html.includes('handleSeedBackspace') || html.includes('SEED_ONLY_CLASS'),
    'must wire seed Backspace policy or SEED_ONLY_CLASS'
  );
  assert.ok(
    html.includes('handleSeedBackspace'),
    'Backspace handler must call handleSeedBackspace'
  );
});

// ── restored draft visibility after reload (🎯T183) ─────────────
// Bug: #input.composer-seed-only { color: transparent } left on after
// draft restore (hot-reload sessionStorage / form restore) → invisible
// text until an edit re-syncs the class. Fix: re-sync after restore.

test('T183 restored non-empty draft must not keep seed-only class', function () {
  // Pure model of post-restore class decision (same helper index.html uses).
  const restored = 'Hello after reload — still here';
  assert.ok(
    !WC.needsSeedOnlyClass(restored),
    'non-empty restored draft must not request seed-only (transparent) class'
  );
  assert.ok(
    !WC.isSeedOnly(restored),
    'non-empty restored draft is not seed-only'
  );
  // EMPTY_SEED alone still hides (empty residual).
  assert.ok(WC.needsSeedOnlyClass(WC.EMPTY_SEED));
  // Seed prefix + real text: visible (class off).
  assert.ok(!WC.needsSeedOnlyClass(WC.EMPTY_SEED + restored));
  // Blank residual: class off (nothing to hide).
  assert.ok(!WC.needsSeedOnlyClass(''));
});

test('T183 index.html re-syncs seed class after draft restore paths', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  // Boot: hot-reload draft restored before ensureWisprSeed so class is not stuck.
  assert.ok(
    /jevons-input[\s\S]{0,400}ensureWisprSeed\(\)/.test(html) ||
      /bootSavedInput[\s\S]{0,400}ensureWisprSeed\(\)/.test(html),
    'boot must restore jevons-input draft before ensureWisprSeed'
  );
  // Late restore / safety: syncComposerSeedClass after savedInput path.
  assert.ok(
    /jevons-input[\s\S]{0,500}syncComposerSeedClass/.test(html),
    'after jevons-input restore must call syncComposerSeedClass'
  );
  // Browser form-restore race: pageshow re-sync.
  assert.ok(
    /pageshow[\s\S]{0,200}syncComposerSeedClass/.test(html),
    'pageshow must re-sync seed-only class (form-restore race)'
  );
  // Transparent CSS still gated on seed-only class only.
  assert.ok(
    /#input\.composer-seed-only\s*\{[^}]*color\s*:\s*transparent/s.test(html),
    'transparent color only under .composer-seed-only'
  );
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('PASS wispr_context_test');
