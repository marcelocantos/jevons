// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for the 🎯T375 boot sentinel.
// Run: node web/scripts/boot_sentinel_test.js
//
// Two halves, both of which have to hold or the mechanism is a liability:
//   TIGHT  — an aborted boot is detected, its recurring timers are torn
//            down, and the owner gets a banner naming the cause.
//   NOT OVER-BROAD — a healthy boot is never touched, errors after boot are
//            not treated as boot failures, and setInterval is handed back
//            unwrapped so the cockpit's own polling is unaffected.
//
// The second half is the one that matters most: a sentinel that fires on a
// working cockpit would clear the fleet poll and break the product it is
// meant to protect. The index.html wiring greps are here too, because the
// module is inert unless it is loaded first and marked at the end.

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const BS = require('./boot_sentinel.js');

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

// ── classify: the whole decision, pure ───────────────────────────

test('booted page is ok even past the grace window', function () {
  const v = BS.classify({ booted: true, errors: [], elapsedMs: 60000, graceMs: 1200 });
  assert.strictEqual(v.status, 'ok');
});

test('booted page with errors is still ok — those are runtime faults', function () {
  // Over-broadness guard. A late render bug must not tear down the timers of
  // a cockpit that booted fine; it belongs to its own feature.
  const v = BS.classify({
    booted: true,
    errors: [{ message: 'isAgentOriginBubble is not defined', source: '/', line: 4488 }],
    elapsedMs: 30000,
    graceMs: 1200,
  });
  assert.strictEqual(v.status, 'ok');
});

test('inside the grace window the verdict is pending, not failed', function () {
  const v = BS.classify({ booted: false, errors: [], elapsedMs: 1199, graceMs: 1200 });
  assert.strictEqual(v.status, 'pending');
});

test('unbooted past grace fails and names the first captured cause', function () {
  const v = BS.classify({
    booted: false,
    errors: [
      { message: 'isAgentOriginBubble is not defined', source: 'http://x/', line: 4488 },
      { message: "can't access lexical declaration 'workingEl'", source: 'http://x/', line: 5873 },
    ],
    elapsedMs: 1200,
    graceMs: 1200,
  });
  assert.strictEqual(v.status, 'failed');
  assert.ok(/isAgentOriginBubble is not defined/.test(v.detail), v.detail);
  assert.ok(/4488/.test(v.detail), v.detail);
  // The FIRST error is the cause; the TDZ storm after it is fallout.
  assert.ok(!/workingEl/.test(v.detail), 'reported fallout instead of cause: ' + v.detail);
});

test('unbooted with no captured error is still a failure', function () {
  // A module that 404s can leave the script dead through a path no listener
  // saw. Silence is not health.
  const v = BS.classify({ booted: false, errors: [], elapsedMs: 5000, graceMs: 1200 });
  assert.strictEqual(v.status, 'failed');
  assert.ok(v.detail.length > 0);
});

test('describeError tolerates ErrorEvent, rejection and bare string shapes', function () {
  assert.strictEqual(BS.describeError({ message: 'boom', source: 'a.js', line: 7 }), 'boom (a.js:7)');
  assert.strictEqual(BS.describeError({ reason: 'nope' }), 'nope');
  assert.strictEqual(BS.describeError('plain'), 'plain');
  assert.strictEqual(BS.describeError(null), '');
});

// ── install: behaviour against a minimal fake document ───────────

function fakeEnv() {
  const listeners = {};
  const timers = [];
  let nextId = 1;
  let cleared = [];
  const nodes = [];

  function node(tag) {
    return {
      tag: tag,
      style: { cssText: '' },
      children: [],
      attrs: {},
      textContent: '',
      type: '',
      setAttribute: function (k, v) { this.attrs[k] = v; },
      appendChild: function (c) { this.children.push(c); },
      addEventListener: function () {},
    };
  }

  const body = node('body');
  const win = {
    reloaded: 0,
    location: { reload: function () { win.reloaded++; } },
    setInterval: function (fn, ms) { const id = nextId++; timers.push({ id: id, fn: fn, ms: ms }); return id; },
    clearInterval: function (id) { cleared.push(id); },
    setTimeout: function (fn) { return fn; },   // fire-later handled manually
    addEventListener: function (t, fn) { (listeners[t] = listeners[t] || []).push(fn); },
  };
  const doc = {
    readyState: 'complete',
    body: body,
    documentElement: node('html'),
    createElement: function (t) { const n = node(t); nodes.push(n); return n; },
    addEventListener: function (t, fn) { (listeners[t] = listeners[t] || []).push(fn); },
  };
  return {
    win: win, doc: doc, body: body,
    emit: function (t, e) { (listeners[t] || []).forEach(function (fn) { fn(e); }); },
    clearedIds: function () { return cleared.slice(); },
  };
}

function bannerOf(env) {
  return env.body.children.filter(function (c) { return c.attrs[BS.BANNER_ATTR]; })[0] || null;
}

test('aborted boot clears the intervals registered during it', function () {
  const env = fakeEnv();
  let clock = 0;
  const h = BS.install(env.win, env.doc, { graceMs: 1200, now: function () { return clock; } });

  const pollId = env.win.setInterval(function () {}, 30000);   // the fleet poll
  env.emit('error', { message: 'isAgentOriginBubble is not defined', filename: 'http://x/', lineno: 4488 });

  clock = 1200;
  const v = h.check();
  assert.strictEqual(v.status, 'failed');
  assert.deepStrictEqual(env.clearedIds(), [pollId],
    'the recurring poll survived an aborted boot — the cascade would recur');
  assert.strictEqual(env.win[BS.FAILED_FLAG], true);
});

test('aborted boot renders an owner-visible banner naming the cause', function () {
  const env = fakeEnv();
  let clock = 0;
  const h = BS.install(env.win, env.doc, { graceMs: 1200, now: function () { return clock; } });
  env.emit('error', { message: 'isAgentOriginBubble is not defined', filename: 'http://x/', lineno: 4488 });
  clock = 1200;
  h.check();

  const b = bannerOf(env);
  assert.ok(b, 'no banner: the failure is silent, which is the whole fault');
  const text = b.children.map(function (c) { return c.textContent; }).join(' ');
  assert.ok(/did not finish loading/i.test(text), text);
  assert.ok(/isAgentOriginBubble/.test(text), 'banner does not name the cause: ' + text);
  assert.ok(b.children.some(function (c) { return c.tag === 'button'; }), 'no recovery affordance');
});

test('healthy boot: no banner, no cleared timers, setInterval unwrapped', function () {
  // The over-broadness case. Everything the failure path does must be
  // absent, including the wrapper — the cockpit polls for its whole life and
  // must not run through sentinel code once boot succeeded.
  const env = fakeEnv();
  let clock = 0;
  const original = env.win.setInterval;
  const h = BS.install(env.win, env.doc, { graceMs: 1200, now: function () { return clock; } });
  assert.notStrictEqual(env.win.setInterval, original, 'setInterval was never wrapped');

  env.win.setInterval(function () {}, 30000);
  h.markBooted();

  clock = 60000;
  const v = h.check();
  assert.strictEqual(v.status, 'ok');
  assert.deepStrictEqual(env.clearedIds(), [], 'a healthy cockpit had its timers cleared');
  assert.strictEqual(bannerOf(env), null, 'a healthy cockpit was given a failure banner');
  assert.strictEqual(env.win.setInterval, original, 'setInterval left wrapped after a good boot');
  assert.notStrictEqual(env.win[BS.FAILED_FLAG], true);
});

test('errors after a successful boot never trigger teardown', function () {
  const env = fakeEnv();
  let clock = 0;
  const h = BS.install(env.win, env.doc, { graceMs: 1200, now: function () { return clock; } });
  h.markBooted();
  const late = env.win.setInterval(function () {}, 30000);
  env.emit('error', { message: 'late render bug', filename: 'http://x/', lineno: 9000 });

  clock = 90000;
  assert.strictEqual(h.check().status, 'ok');
  assert.deepStrictEqual(env.clearedIds(), [], 'a late runtime error cleared interval ' + late);
  assert.strictEqual(bannerOf(env), null);
});

test('check is idempotent — the banner is not stacked on repeat calls', function () {
  const env = fakeEnv();
  let clock = 0;
  const h = BS.install(env.win, env.doc, { graceMs: 1200, now: function () { return clock; } });
  clock = 1200;
  h.check();
  h.check();
  h.check();
  const banners = env.body.children.filter(function (c) { return c.attrs[BS.BANNER_ATTR]; });
  assert.strictEqual(banners.length, 1, 'stacked ' + banners.length + ' banners');
});

// ── index.html wiring: the module is inert unless loaded first ───

const INDEX = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');

test('boot_sentinel.js is the FIRST local script index.html loads', function () {
  const refs = [];
  const re = /<script[^>]+src=["'](scripts\/[^"']+)["']/gi;
  let m;
  while ((m = re.exec(INDEX)) !== null) refs.push(m[1]);
  assert.ok(refs.length > 1, 'parsed no local script tags — the scan broke');
  assert.strictEqual(refs[0], 'scripts/boot_sentinel.js',
    'sentinel is not first (' + refs[0] + '): a module failing before it '
    + 'loads would abort the page with nothing watching');
});

test('index.html installs the sentinel and marks the boot at the end', function () {
  assert.ok(/BootSentinel\.install\(/.test(INDEX), 'sentinel never installed');
  const mark = INDEX.lastIndexOf('markBooted(');
  assert.ok(mark > 0, 'boot is never marked, so every load would look aborted');
  // Must be the tail of the last inline script: anything load-bearing after
  // it could throw without the sentinel noticing.
  const tailStart = INDEX.lastIndexOf('<script');
  assert.ok(mark > tailStart, 'markBooted() is not inside the final inline script');
});

test('web/embed.go serves the sentinel to released binaries too', function () {
  const embed = fs.readFileSync(path.join(__dirname, '..', 'embed.go'), 'utf8');
  assert.ok(/^\s*\/\/go:embed\s+scripts\/boot_sentinel\.js\s*$/m.test(embed),
    'sentinel not embedded: a released jevonsd would 404 it and boot unwatched');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('PASS boot_sentinel_test');
