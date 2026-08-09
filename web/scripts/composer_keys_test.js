// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for composer key policy
// (🎯T126 / 🎯T149 Home/End; 🎯T132 Enter chords).
// Run: node web/scripts/composer_keys_test.js
// Pure policy — no DOM, no Playwright.

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const CK = require('./composer_keys.js');
const VL = require('./virtual_list.js');
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

function seedOpts(value) {
  return {
    seedPrefixLen: WC.seedPrefixLen(value),
    effectiveLength: WC.stripSeed(value).length,
    isSeedOnly: WC.isSeedOnly(value),
  };
}

// ── caret positions (field-content policy) ──────────────────────────

test('T126 Home → selectionStart 0 for multi-char value', function () {
  const value = 'hello world';
  assert.strictEqual(CK.caretAfterHome(value, 5), 0);
  const sel = CK.selectionAfterHomeEnd('Home', value, 5, 5, {});
  assert.deepStrictEqual(sel, { start: 0, end: 0 });
});

test('T126 End → caret at end of content', function () {
  const value = 'hello world';
  assert.strictEqual(CK.caretAfterEnd(value, 0), value.length);
  const sel = CK.selectionAfterHomeEnd('End', value, 0, 0, {});
  assert.deepStrictEqual(sel, { start: value.length, end: value.length });
});

test('T126 Shift+Home/End extend selection within field', function () {
  const value = 'abcdef';
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, 3, 3, { shiftKey: true }),
    { start: 0, end: 3 }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('End', value, 2, 2, { shiftKey: true }),
    { start: 2, end: 6 }
  );
});

test('T126 Alt Home/End leave caret to other handlers', function () {
  assert.strictEqual(CK.selectionAfterHomeEnd('End', 'ab', 1, 1, { altKey: true }), null);
  assert.strictEqual(CK.selectionAfterHomeEnd('ArrowLeft', 'ab', 1, 1, {}), null);
});

// ── 🎯T149 seed-aware bounds ────────────────────────────────────────

test('T149 seed-only Home/End collapse to insert point (after EMPTY_SEED)', function () {
  const value = WC.EMPTY_SEED;
  assert.ok(WC.isSeedOnly(value));
  assert.strictEqual(WC.seedPrefixLen(value), WC.EMPTY_SEED.length);
  assert.strictEqual(WC.stripSeed(value), '');
  const opts = seedOpts(value);
  const bounds = CK.contentCaretBounds(value, opts);
  assert.deepStrictEqual(bounds, { home: value.length, end: value.length });
  // Caret mid-seed → End snaps to insert point (visible move, not no-op).
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('End', value, 1, 1, {}, opts),
    { start: value.length, end: value.length }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, 1, 1, {}, opts),
    { start: value.length, end: value.length }
  );
  // Already at insert point → both stay (empty-field correct).
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, value.length, value.length, {}, opts),
    { start: value.length, end: value.length }
  );
});

test('T149 seed+text Home skips seed; End at full length', function () {
  const value = WC.EMPTY_SEED + 'hello';
  const opts = seedOpts(value);
  assert.strictEqual(opts.seedPrefixLen, WC.EMPTY_SEED.length);
  assert.strictEqual(opts.effectiveLength, 5);
  assert.strictEqual(opts.isSeedOnly, false);
  assert.deepStrictEqual(CK.contentCaretBounds(value, opts), {
    home: WC.EMPTY_SEED.length,
    end: value.length,
  });
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, value.length, value.length, {}, opts),
    { start: WC.EMPTY_SEED.length, end: WC.EMPTY_SEED.length }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('End', value, WC.EMPTY_SEED.length, WC.EMPTY_SEED.length, {}, opts),
    { start: value.length, end: value.length }
  );
  // Shift+Home selects effective text only (not the invisible seed).
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, value.length, value.length, { shiftKey: true }, opts),
    { start: WC.EMPTY_SEED.length, end: value.length }
  );
});

test('T149 real draft without seed still field-wide Home/End', function () {
  const value = 'draft text';
  const opts = seedOpts(value);
  assert.strictEqual(opts.seedPrefixLen, 0);
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, 5, 5, {}, opts),
    { start: 0, end: 0 }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('End', value, 0, 0, {}, opts),
    { start: value.length, end: value.length }
  );
});

// ── 🎯T307 chord split: Home/End = field, Cmd/Ctrl+Arrow = line ──────
//
// Supersedes the 🎯T149 'Meta/Ctrl+ArrowLeft/Right are field ends' and
// 'Meta+Arrow with seed+text uses effective bounds' tests, which encoded
// the bug the owner reported: aliasing Cmd/Ctrl+Left/Right onto field
// Home/End collapsed two distinct chords into one behaviour and stole the
// native line-local caret. Line navigation belongs to the browser/OS.

test('T307 Meta/Ctrl+ArrowLeft/Right are NOT owned — browser does line nav', function () {
  const value = 'abcdef';
  assert.strictEqual(
    CK.selectionAfterHomeEnd('ArrowLeft', value, 4, 4, { metaKey: true }),
    null
  );
  assert.strictEqual(
    CK.selectionAfterHomeEnd('ArrowRight', value, 1, 1, { metaKey: true }),
    null
  );
  assert.strictEqual(
    CK.selectionAfterHomeEnd('ArrowLeft', value, 4, 4, { ctrlKey: true }),
    null
  );
  assert.strictEqual(
    CK.selectionAfterHomeEnd('ArrowRight', value, 1, 1, { ctrlKey: true }),
    null
  );
  // Shift+Meta+Arrow (line-local extend) is the platform's too.
  assert.strictEqual(
    CK.selectionAfterHomeEnd('ArrowLeft', value, 4, 4, { metaKey: true, shiftKey: true }),
    null
  );
  assert.strictEqual(
    CK.selectionAfterHomeEnd('ArrowRight', value, 1, 1, { ctrlKey: true, shiftKey: true }),
    null
  );
  // Meta+ArrowDown stays null (jump-to-bottom owns it).
  assert.strictEqual(
    CK.selectionAfterHomeEnd('ArrowDown', value, 1, 1, { metaKey: true }),
    null
  );
});

test('T307 Meta/Ctrl+Arrow stays unowned even with seed+text', function () {
  const value = WC.EMPTY_SEED + 'xyz';
  const opts = seedOpts(value);
  assert.strictEqual(
    CK.selectionAfterHomeEnd('ArrowLeft', value, value.length, value.length, { metaKey: true }, opts),
    null
  );
  assert.strictEqual(
    CK.selectionAfterHomeEnd('ArrowRight', value, opts.seedPrefixLen, opts.seedPrefixLen, { ctrlKey: true }, opts),
    null
  );
});

test('T307 multi-line draft: Home/End remain field-wide, not line-local', function () {
  const value = 'first line\nsecond line\nthird';
  const opts = seedOpts(value);
  // Caret parked mid-second-line: Home goes to field start, not line start.
  const midSecond = value.indexOf('second') + 3;
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', value, midSecond, midSecond, {}, opts),
    { start: 0, end: 0 }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('End', value, midSecond, midSecond, {}, opts),
    { start: value.length, end: value.length }
  );
  // Same field-wide bounds with a seed prefix in front of the draft.
  const seeded = WC.EMPTY_SEED + value;
  const seededOpts = seedOpts(seeded);
  const seededMid = seeded.indexOf('second') + 3;
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('Home', seeded, seededMid, seededMid, {}, seededOpts),
    { start: WC.EMPTY_SEED.length, end: WC.EMPTY_SEED.length }
  );
  assert.deepStrictEqual(
    CK.selectionAfterHomeEnd('End', seeded, seededMid, seededMid, {}, seededOpts),
    { start: seeded.length, end: seeded.length }
  );
});

// ── jump-to-bottom must not steal Home/End while composer focused ───

test('T126 plain End does not jump when composer focused', function () {
  const opts = {
    composerFocused: true,
    isJumpHotkey: VL.isJumpToBottomHotkey,
  };
  assert.strictEqual(CK.shouldAllowJumpToBottom('End', {}, opts), false);
  // Home is never a jump key.
  assert.strictEqual(CK.shouldAllowJumpToBottom('Home', {}, opts), false);
  assert.strictEqual(VL.isJumpToBottomHotkey('Home', {}), false);
});

test('T126 plain End jumps when composer not focused', function () {
  const opts = {
    composerFocused: false,
    isJumpHotkey: VL.isJumpToBottomHotkey,
  };
  assert.strictEqual(CK.shouldAllowJumpToBottom('End', {}, opts), true);
});

test('T126 Meta/Ctrl+ArrowDown still jumps with composer focused', function () {
  const opts = {
    composerFocused: true,
    isJumpHotkey: VL.isJumpToBottomHotkey,
  };
  assert.strictEqual(
    CK.shouldAllowJumpToBottom('ArrowDown', { metaKey: true }, opts),
    true
  );
  assert.strictEqual(
    CK.shouldAllowJumpToBottom('ArrowDown', { ctrlKey: true }, opts),
    true
  );
});

test('T126 isComposerFocused only true for main input ref', function () {
  const input = { id: 'input' };
  const other = { id: 'other' };
  assert.strictEqual(CK.isComposerFocused(input, input), true);
  assert.strictEqual(CK.isComposerFocused(other, input), false);
  assert.strictEqual(CK.isComposerFocused(null, input), false);
  assert.ok(CK.isComposerTextControl('TEXTAREA'));
  assert.ok(CK.isComposerTextControl('input'));
  assert.ok(!CK.isComposerTextControl('DIV'));
});

// ── index wiring: script load + jump gate uses ComposerKeys ─────────

test('T126/T149 index.html loads composer_keys and gates jump on composer focus', function () {
  const htmlPath = path.join(__dirname, '..', 'index.html');
  const html = fs.readFileSync(htmlPath, 'utf8');
  assert.ok(html.includes('scripts/composer_keys.js'), 'must load composer_keys.js');
  assert.ok(
    html.includes('ComposerKeys') || html.includes('shouldAllowJumpToBottom'),
    'must wire ComposerKeys jump gate'
  );
  assert.ok(
    /selectionAfterHomeEnd|caretAfterHome|Home/.test(html) &&
      (html.includes('T126') || html.includes('T149')),
    'must handle Home/End caret on composer'
  );
  assert.ok(
    html.includes('seedPrefixLen') || html.includes('isSeedOnly') || html.includes('seedOpts'),
    'must pass seed-aware opts into selectionAfterHomeEnd (🎯T149)'
  );
});

test('T149 wispr_context exports seedPrefixLen / isSeedOnly / stripSeed / EMPTY_SEED', function () {
  assert.ok(typeof WC.EMPTY_SEED === 'string' && WC.EMPTY_SEED.length > 0);
  assert.strictEqual(typeof WC.seedPrefixLen, 'function');
  assert.strictEqual(typeof WC.isSeedOnly, 'function');
  assert.strictEqual(typeof WC.stripSeed, 'function');
  assert.strictEqual(WC.seedPrefixLen(WC.EMPTY_SEED + 'x'), WC.EMPTY_SEED.length);
  assert.strictEqual(WC.seedPrefixLen('plain'), 0);
  assert.strictEqual(WC.stripSeed(WC.EMPTY_SEED + 'hi'), 'hi');
  assert.ok(WC.isSeedOnly(WC.EMPTY_SEED));
  assert.ok(!WC.isSeedOnly('hi'));
});

// ── 🎯T132 Enter-chord policy ───────────────────────────────────────

test('T132 Ctrl+Enter → interrupt (immediate send)', function () {
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { ctrlKey: true }, { composerEmpty: false }),
    'interrupt'
  );
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { ctrlKey: true }, { composerEmpty: true }),
    'interrupt'
  );
  // Alt alone is never plain 'send' (enqueue path) or 'interrupt' — force_send / send_queue_now / noop.
  assert.notStrictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, { composerEmpty: false }),
    'send'
  );
  assert.notStrictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, { composerEmpty: true, queueLen: 0 }),
    'interrupt'
  );
});

// 🎯T241: Alt+Enter = force-send only (never pop_last).
test('T241 Alt+Enter real draft → force_send (not pop_last, not noop)', function () {
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, { composerEmpty: false }),
    'force_send'
  );
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, {
      composerEmpty: WC.isEffectivelyEmpty('real draft'),
      queueLen: 0,
    }),
    'force_send'
  );
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, {
      composerEmpty: WC.isEffectivelyEmpty(WC.EMPTY_SEED + 'Are we done?'),
      queueLen: 3,
    }),
    'force_send',
    'real draft wins over queue (send composer text, not sendQueueNow)'
  );
  assert.notStrictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, { composerEmpty: false }),
    'pop_last'
  );
});

test('T241 Alt+Enter empty/seed-only + queue≥1 → send_queue_now', function () {
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, {
      composerEmpty: true,
      queueLen: 1,
    }),
    'send_queue_now'
  );
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, {
      composerEmpty: WC.isEffectivelyEmpty(WC.EMPTY_SEED),
      queueLen: 2,
    }),
    'send_queue_now',
    'EMPTY_SEED counts as empty → queue send-now'
  );
  assert.notStrictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, {
      composerEmpty: true,
      queueLen: 1,
    }),
    'pop_last'
  );
});

test('T241 Alt+Enter empty + no queue → noop (never pop_last)', function () {
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, {
      composerEmpty: true,
      queueLen: 0,
    }),
    'noop'
  );
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, {
      composerEmpty: WC.isEffectivelyEmpty(WC.EMPTY_SEED),
      queueLen: 0,
    }),
    'noop'
  );
  assert.notStrictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, { composerEmpty: true }),
    'pop_last'
  );
});

// 🎯T192/T241: seed-shaped residue is empty for Alt+Enter force-send branching.
test('T192/T241 Alt+Enter seed-shaped → queue branch; real draft → force_send', function () {
  const seedShaped = [
    '',
    WC.EMPTY_SEED,
    '.',
    '\u200B',
    '\u200B.',
    '.\u200B',
    WC.EMPTY_SEED + WC.EMPTY_SEED,
  ];
  seedShaped.forEach(function (v) {
    assert.ok(WC.isEffectivelyEmpty(v), 'isEffectivelyEmpty: ' + JSON.stringify(v));
    assert.strictEqual(
      CK.classifyEnterAction('Enter', { altKey: true }, {
        composerEmpty: WC.isEffectivelyEmpty(v),
        queueLen: 1,
      }),
      'send_queue_now',
      'seed-shaped + queue → send_queue_now for ' + JSON.stringify(v)
    );
    assert.strictEqual(
      CK.classifyEnterAction('Enter', { altKey: true }, {
        composerEmpty: WC.isEffectivelyEmpty(v),
        queueLen: 0,
      }),
      'noop',
      'seed-shaped no queue → noop for ' + JSON.stringify(v)
    );
  });
  const drafts = ['real draft', '?', 'Hello.', WC.EMPTY_SEED + 'Are we done?'];
  drafts.forEach(function (v) {
    assert.ok(!WC.isEffectivelyEmpty(v), 'not empty: ' + JSON.stringify(v));
    assert.strictEqual(
      CK.classifyEnterAction('Enter', { altKey: true }, {
        composerEmpty: WC.isEffectivelyEmpty(v),
        queueLen: 0,
      }),
      'force_send',
      'Alt+Enter force_send for draft ' + JSON.stringify(v)
    );
  });
});

test('T132 plain Enter → send; Shift+Enter → newline; Meta is not interrupt', function () {
  assert.strictEqual(CK.classifyEnterAction('Enter', {}, { composerEmpty: false }), 'send');
  assert.strictEqual(CK.classifyEnterAction('Enter', { metaKey: true }, {}), 'send');
  assert.strictEqual(CK.classifyEnterAction('Enter', { shiftKey: true }, {}), 'newline');
  // Shift wins over other modifiers for newline.
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { shiftKey: true, ctrlKey: true }, {}),
    'newline'
  );
  assert.strictEqual(CK.classifyEnterAction('ArrowUp', {}, {}), null);
});

test('T132 Ctrl+Alt+Enter still interrupt (Ctrl owns immediate send)', function () {
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { ctrlKey: true, altKey: true }, { composerEmpty: true }),
    'interrupt'
  );
});

test('T132 lastOwnerHistoryEntry returns tail text for pop_last wire-up', function () {
  assert.strictEqual(CK.lastOwnerHistoryEntry(null), null);
  assert.strictEqual(CK.lastOwnerHistoryEntry([]), null);
  const hist = [
    { text: 'first', el: { id: 1 } },
    { text: 'most recent owner', el: { id: 2 } },
  ];
  const last = CK.lastOwnerHistoryEntry(hist);
  assert.ok(last);
  assert.strictEqual(last.text, 'most recent owner');
  assert.strictEqual(last.index, 1);
  assert.strictEqual(last.el.id, 2);
});

// 🎯T227: product break was empty msgHistory after progressive hydrate (DOM has
// .msg.user, memory does not) while classifyEnterAction still returned pop_last.
// Pure resolve must fall back to DOM-derived entries so empty Alt+Enter works.
test('T227 resolveLastOwnerEntry: empty history + DOM users → last owner', function () {
  assert.strictEqual(typeof CK.resolveLastOwnerEntry, 'function');
  assert.strictEqual(typeof CK.ownerEntriesFromUserNodes, 'function');

  assert.strictEqual(CK.resolveLastOwnerEntry(null, null), null);
  assert.strictEqual(CK.resolveLastOwnerEntry([], []), null);

  const dom = [
    { text: 'older owner', el: { id: 'a' } },
    { text: 'most recent owner', el: { id: 'b' } },
  ];
  // Empty / textless history → DOM tail (the real product-path break).
  const fromDom = CK.resolveLastOwnerEntry([], dom);
  assert.ok(fromDom, 'must resolve from DOM when history empty');
  assert.strictEqual(fromDom.text, 'most recent owner');
  assert.strictEqual(fromDom.source, 'dom');
  assert.strictEqual(fromDom.el.id, 'b');

  const emptyTextHist = [{ text: '', el: { id: 'z' } }];
  const fromDom2 = CK.resolveLastOwnerEntry(emptyTextHist, dom);
  assert.ok(fromDom2);
  assert.strictEqual(fromDom2.text, 'most recent owner');
  assert.strictEqual(fromDom2.source, 'dom');

  // Non-empty history wins over DOM (live/WS path).
  const hist = [
    { text: 'hist older', el: { id: 1 } },
    { text: 'from msgHistory', el: { id: 2 } },
  ];
  const fromHist = CK.resolveLastOwnerEntry(hist, dom);
  assert.ok(fromHist);
  assert.strictEqual(fromHist.text, 'from msgHistory');
  assert.strictEqual(fromHist.source, 'history');
});

test('T227 ownerEntriesFromUserNodes uses _layoutText (hydrate shells)', function () {
  const nodes = [
    { _layoutText: 'first', textContent: 'wrong' },
    { _layoutText: '', textContent: 'skip empty layout' },
    { _layoutText: 'last owner from hydrate', textContent: 'painted' },
  ];
  const entries = CK.ownerEntriesFromUserNodes(nodes);
  assert.strictEqual(entries.length, 2);
  assert.strictEqual(entries[0].text, 'first');
  assert.strictEqual(entries[1].text, 'last owner from hydrate');
  const resolved = CK.resolveLastOwnerEntry([], entries);
  assert.strictEqual(resolved.text, 'last owner from hydrate');
  assert.strictEqual(resolved.source, 'dom');
});

// 🎯T227 resolve helpers remain (history/DOM); Alt+Enter product path no longer pop_last.
test('T227 resolveLastOwnerEntry still works for history tooling (not Alt+Enter product)', function () {
  const entry = CK.resolveLastOwnerEntry([], [
    { text: 'owner text for history tooling', el: {} },
  ]);
  assert.ok(entry && entry.text);
  assert.ok(entry.text.indexOf('owner text for history') === 0);
  // 🎯T241: Alt+Enter never routes to pop_last even when empty.
  assert.notStrictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, {
      composerEmpty: WC.isEffectivelyEmpty(WC.EMPTY_SEED),
      queueLen: 0,
    }),
    'pop_last'
  );
});

test('T241 index.html wires Alt+Enter force_send / send_queue_now (not pop_last)', function () {
  const htmlPath = path.join(__dirname, '..', 'index.html');
  const html = fs.readFileSync(htmlPath, 'utf8');
  assert.ok(html.includes('classifyEnterAction'),
    'must use classifyEnterAction Enter policy');
  assert.ok(html.includes('send_queue_now') || html.includes('sendQueueNow'),
    'must wire Alt+Enter queue send-now path');
  assert.ok(html.includes('force_send') || /action === 'force_send'/.test(html),
    'must wire Alt+Enter force_send for draft');
  assert.ok(
    /queueLen/.test(html),
    'must pass queueLen into classifyEnterAction'
  );
  assert.ok(
    /Ctrl\+Enter|Control\+Enter/.test(html) && /interrupt|interject/.test(html),
    'UI must document Ctrl+Enter interject/immediate send'
  );
  assert.ok(
    /Alt\+Enter/.test(html) && /force-send|force.send|queued/i.test(html),
    'UI must document Alt+Enter force-send (not empty-pops-last)'
  );
  assert.ok(
    !/Alt\+Enter empty pops last/i.test(html),
    'placeholder must not advertise empty Alt+Enter pop_last'
  );
  // 🎯T192: empty check must use isEffectivelyEmpty (not bare !value / trim-only).
  assert.ok(
    /composerEmpty[\s\S]{0,120}isEffectivelyEmpty/.test(html) ||
      /isEffectivelyEmpty\(input\.value\)/.test(html),
    'composerEmpty must come from WisprContext.isEffectivelyEmpty(input.value)'
  );
  assert.ok(
    !/composerEmpty\s*=\s*!\s*input\.value/.test(html) &&
      !/composerEmpty\s*=\s*!\s*String\(input\.value/.test(html),
    'must not set composerEmpty from bare !input.value (seed looks non-empty)'
  );
  // Product path: Alt+Enter must not call popLast on the enter chord.
  const enterBlock = html.match(/input\.addEventListener\('keydown'[\s\S]{0,2500}?if \(isEnter\)[\s\S]{0,1800}/);
  assert.ok(enterBlock, 'must have enter keydown handler');
  assert.ok(
    !/popLastOwnerAsInterjection\s*\(/.test(enterBlock[0]),
    'Alt+Enter enter handler must not call popLastOwnerAsInterjection'
  );
});

// ── 🎯T235 product-path re-break (anti-greenwash) ───────────────────
// Prior stack (T132/T192/T227) stayed hermetic-green while daily UI still
// missed empty Alt+Enter. Cover: code-Enter fallback, clobber-after-rebuild,
// empty _layoutText fallthrough, DOM-longer prefer, failLoud, index wiring.

test('T235/T241 isEnterKey / classify: code Enter when key is not Enter (Option+Enter)', function () {
  assert.strictEqual(typeof CK.isEnterKey, 'function');
  assert.ok(CK.isEnterKey('Enter', {}));
  assert.ok(CK.isEnterKey('', { code: 'Enter' }), 'code Enter counts');
  assert.ok(CK.isEnterKey(null, { code: 'NumpadEnter' }));
  assert.ok(!CK.isEnterKey('a', { code: 'KeyA' }));
  // Product: key may be wrong/empty while code is Enter + alt → force-send policy.
  assert.strictEqual(
    CK.classifyEnterAction('', { altKey: true, code: 'Enter' }, {
      composerEmpty: true,
      code: 'Enter',
      queueLen: 0,
    }),
    'noop',
    'Option+Enter empty no queue → noop (never pop_last)'
  );
  assert.strictEqual(
    CK.classifyEnterAction('Dead', { altKey: true, code: 'Enter' }, {
      composerEmpty: WC.isEffectivelyEmpty(WC.EMPTY_SEED),
      code: 'Enter',
      queueLen: 1,
    }),
    'send_queue_now',
    'Option+Enter seed-only + queue → send_queue_now'
  );
  assert.strictEqual(
    CK.classifyEnterAction('', { altKey: true, code: 'Enter' }, {
      composerEmpty: false,
      code: 'Enter',
    }),
    'force_send',
    'non-empty draft → force_send'
  );
});

test('T235 lastOwnerHistoryEntry skips trailing empty stubs', function () {
  const last = CK.lastOwnerHistoryEntry([
    { text: 'keep me', el: 1 },
    { text: '', el: 2 },
    { text: null, el: 3 },
  ]);
  assert.ok(last);
  assert.strictEqual(last.text, 'keep me');
  assert.strictEqual(last.index, 0);
});

test('T235 resolve: DOM longer than hist prefers DOM last (hydrate ahead)', function () {
  const hist = [{ text: 'stale-tail-only-in-memory', el: 'h' }];
  const dom = [
    { text: 'older', el: 'a' },
    { text: 'true most recent painted', el: 'b' },
  ];
  const r = CK.resolveLastOwnerEntry(hist, dom);
  assert.ok(r);
  assert.strictEqual(r.source, 'dom');
  assert.strictEqual(r.text, 'true most recent painted');
});

test('T235 ownerEntriesFromUserNodes: empty _layoutText falls through to _body', function () {
  const nodes = [
    { _layoutText: '', _body: { textContent: 'visible body after dematerialize miss' }, textContent: '2m ago' },
    { _layoutText: 'has layout', _body: { textContent: 'ignored' } },
  ];
  const entries = CK.ownerEntriesFromUserNodes(nodes);
  assert.strictEqual(entries.length, 2);
  assert.strictEqual(entries[0].text, 'visible body after dematerialize miss');
  assert.strictEqual(entries[1].text, 'has layout');
});

test('T235 planPopLastOwner: empty hist + DOM → ok with text (T227 path)', function () {
  assert.strictEqual(typeof CK.planPopLastOwner, 'function');
  const plan = CK.planPopLastOwner([], [
    { text: 'older', el: 1 },
    { text: 'MOST RECENT OWNER TO POP', el: 2 },
  ]);
  assert.ok(plan.ok, 'must succeed');
  assert.strictEqual(plan.text, 'MOST RECENT OWNER TO POP');
  assert.strictEqual(plan.source, 'dom');
  assert.ok(plan.history && plan.history.length === 2);
});

test('T235 planPopLastOwner: never clobber DOM text with empty hist tail', function () {
  // Prior product bug: resolve found DOM text (hist only empty stubs),
  // rebuild no-op (hist.length >= dom.length), then entry overwritten with
  // empty hist last → silent miss. planPopLastOwner must keep DOM text.
  const hist = [
    { text: '', el: 1 },
    { text: '', el: 2 },
    { text: '', el: 3 },
  ];
  const dom = [{ text: 'only painted owner', el: 9 }];
  const plan = CK.planPopLastOwner(hist, dom);
  assert.ok(plan.ok, 'must not clobber: ' + JSON.stringify(plan));
  assert.strictEqual(plan.text, 'only painted owner');
  assert.ok(plan.text.length > 0);
  // Resynced history must not reintroduce empty-only tail as the load text.
  assert.ok(plan.history && plan.history.some(function (e) {
    return e.text === 'only painted owner';
  }));
});

test('T241 seed empty + Alt+Enter is force-send policy (planPopLast is not product path)', function () {
  const composerEmpty = WC.isEffectivelyEmpty(WC.EMPTY_SEED);
  assert.ok(composerEmpty);
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, {
      composerEmpty: composerEmpty,
      queueLen: 1,
    }),
    'send_queue_now'
  );
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, {
      composerEmpty: composerEmpty,
      queueLen: 0,
    }),
    'noop'
  );
  // planPopLastOwner remains for tooling/tests but is not Alt+Enter product.
  const nodes = [
    { _layoutText: 'first owner', textContent: 'x' },
    { _layoutText: '', _body: { textContent: 'last owner via body' } },
  ];
  const dom = CK.ownerEntriesFromUserNodes(nodes);
  const plan = CK.planPopLastOwner([], dom);
  assert.ok(plan.ok);
  assert.strictEqual(plan.text, 'last owner via body');
});

test('T235 planPopLastOwner: failLoud when bubbles exist but unresolvable', function () {
  // All empty texts — bubbles counted via hist stubs / empty dom skip.
  const plan = CK.planPopLastOwner(
    [{ text: '' }, { text: '' }],
    []
  );
  // hist stubs have no text → bubbleCount 0 from hist filter, dom empty
  // → no_owner silent. Use dom with empty-only nodes via plan after collect.
  const plan2 = CK.planPopLastOwner([], [{ text: '', el: 1 }]);
  // empty text filtered by lastOwner — bubbleCount from dom.length is 1
  // but lastOwnerHistoryEntry returns null for empty text entries.
  // planPopLastOwner counts dom.length for bubbleCount:
  assert.strictEqual(plan2.ok, false);
  assert.strictEqual(plan2.failLoud, true, 'must fail loud when dom entries present');
  assert.ok(plan2.reason === 'resolve_miss' || plan2.reason === 'no_owner');

  const quiet = CK.planPopLastOwner([], []);
  assert.strictEqual(quiet.ok, false);
  assert.strictEqual(quiet.failLoud, false, 'no bubbles → quiet miss OK');
});

test('T241 non-empty draft Alt+Enter is force_send (not noop)', function () {
  assert.strictEqual(
    CK.classifyEnterAction('Enter', { altKey: true }, {
      composerEmpty: WC.isEffectivelyEmpty('real draft still here'),
    }),
    'force_send'
  );
});

test('T235 index.html still has planPopLastOwner helpers + Option+Enter code path', function () {
  const htmlPath = path.join(__dirname, '..', 'index.html');
  const html = fs.readFileSync(htmlPath, 'utf8');
  // Helpers may remain for residual tooling; Alt+Enter product is T241 force-send.
  assert.ok(html.includes('planPopLastOwner') || html.includes('resolveLastOwnerEntry'),
    'history resolve helpers may still exist');
  assert.ok(html.includes('e.code') || html.includes('code: e.code'),
    'must pass code for Option+Enter');
  assert.ok(html.includes('addStatusMsg'), 'fail loud via addStatusMsg');
  assert.ok(
    /getModifierState\s*\(\s*['"]Alt['"]\s*\)/.test(html) || /altHeld/.test(html),
    'must detect Option/Alt via getModifierState or altHeld'
  );
  assert.ok(
    /code\s*===\s*['"]Enter['"]/.test(html) || /e\.code/.test(html),
    'must consider e.code for Enter (macOS Option+Enter)'
  );
  assert.ok(
    /composerEmpty[\s\S]{0,200}isEffectivelyEmpty/.test(html),
    'seed-empty still via isEffectivelyEmpty'
  );
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('PASS composer_keys_test');
