// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for the 🎯T374 module gate.
// Run: node web/scripts/module_gate_test.js
//
// Three halves, and the last two are the ones that keep this honest:
//   TIGHT          — an absent module gets a stand-in that answers every
//                    shape a caller reaches for without throwing.
//   NOT OVER-BROAD — a present module is never stood in over, and the gate
//                    never swallows an error it did not cause.
//   NOT DRIFTING   — the snake_case → PascalCase name rule is recomputed
//                    against every file in web/scripts/, so a module whose
//                    global does not follow the rule fails here instead of
//                    silently falling out of the gate's coverage.
//
// The containment claim itself — "the page still boots with a module
// removed" — is not decidable here; it needs a real document with real
// script-ordering semantics. That is
// scripts/chat-ui-test/t374-module-gate-test.js, and this file deliberately
// does not pretend to cover it.

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const MG = require('./module_gate.js');

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

// ── Name derivation, pure ────────────────────────────────────────

test('snake_case module name becomes its PascalCase global', function () {
  assert.strictEqual(MG.globalNameFor('scripts/pending_turns.js'), 'PendingTurns');
  assert.strictEqual(MG.globalNameFor('scripts/composer_persist.js'), 'ComposerPersist');
  assert.strictEqual(MG.globalNameFor('scripts/target_context_chrome.js'), 'TargetContextChrome');
  assert.strictEqual(MG.globalNameFor('scripts/virtual_list.js'), 'VirtualList');
});

test('the measured exceptions win over the rule', function () {
  assert.strictEqual(MG.globalNameFor('scripts/jlog.js'), 'jLog');
  assert.strictEqual(MG.globalNameFor('scripts/owner_ux.js'), 'OwnerUX');
  assert.strictEqual(MG.globalNameFor('scripts/rsi_dispositions.js'), 'RSIDispositions');
  assert.strictEqual(MG.globalNameFor('scripts/smd.js'), 'smd');
  assert.strictEqual(MG.globalNameFor('scripts/transport.js'), 'transport');
});

test('a cache-busting query string does not change the derived name', function () {
  assert.strictEqual(MG.globalNameFor('scripts/fleet_row.js?v=3'), 'FleetRow');
});

// ── plan: which modules need a stand-in ──────────────────────────

test('plan stands in only for absent local modules', function () {
  const present = { PendingTurns: false, FleetRow: true, jLog: true };
  const need = MG.plan(
    ['scripts/pending_turns.js', 'scripts/fleet_row.js', 'scripts/jlog.js'],
    (n) => present[n],
    {});
  assert.deepStrictEqual(need, [{ src: 'scripts/pending_turns.js', global: 'PendingTurns' }]);
});

test('plan ignores CDN tags — they are not ours to stand in for', function () {
  const need = MG.plan(
    ['https://cdn.jsdelivr.net/npm/marked/marked.min.js', 'scripts/fleet_row.js'],
    () => false, {});
  assert.deepStrictEqual(need.map((n) => n.src), ['scripts/fleet_row.js']);
});

test('plan does not report a module signal (a) already handled', function () {
  const need = MG.plan(['scripts/pending_turns.js'], () => false, { PendingTurns: true });
  assert.deepStrictEqual(need, []);
});

// ── The stand-in: every shape a caller might reach for ───────────

test('a stand-in answers property access, call, and construction', function () {
  const X = MG.makeStandIn('PendingTurns', 'scripts/pending_turns.js');
  assert.strictEqual(typeof X, 'function');
  assert.strictEqual(typeof X.empty, 'function');
  assert.doesNotThrow(() => X.empty());
  assert.doesNotThrow(() => X.deserialize({ a: 1 }).items.first.deep.chain());
  assert.doesNotThrow(() => new X());
});

test('a stand-in coerces to a string that names the module', function () {
  const X = MG.makeStandIn('PendingTurns', 'scripts/pending_turns.js');
  const s = '' + X;
  assert.ok(s.indexOf('scripts/pending_turns.js') >= 0, 'marker does not name the module: ' + s);
  assert.ok(s.indexOf('jevons') >= 0, 'marker is not attributable to us: ' + s);
  assert.doesNotThrow(() => `${X}`);
  assert.doesNotThrow(() => String(X.anything.at.all));
});

test('a stand-in iterates empty rather than throwing', function () {
  const X = MG.makeStandIn('FleetRow', 'scripts/fleet_row.js');
  assert.deepStrictEqual([...X], []);
  assert.deepStrictEqual(Object.keys(X), []);
  let n = 0;
  for (const _ of X) n++;
  assert.strictEqual(n, 0);
  for (const _k in X) n++;
  assert.strictEqual(n, 0, 'for..in over a stand-in must yield nothing');
  assert.strictEqual(X.length, 0, 'array-like consumers must see an empty list');
});

test('a stand-in is NOT thenable — await must not hang', async function () {
  // If `then` returned another stand-in, await would call it, receive a
  // stand-in back, and never settle: a deadlock in place of a throw, which is
  // strictly worse than the disease.
  const X = MG.makeStandIn('HistoryLoading', 'scripts/history_loading.js');
  assert.strictEqual(X.then, undefined);
});

test('writing to a stand-in is accepted silently', function () {
  const X = MG.makeStandIn('OwnerUX', 'scripts/owner_ux.js');
  assert.doesNotThrow(() => { X.state = { mounted: true }; });
  assert.doesNotThrow(() => { delete X.state; });
});

test('a stand-in is identifiable, and a real module is not', function () {
  assert.ok(MG.isStandIn(MG.makeStandIn('smd', 'scripts/smd.js')));
  assert.ok(!MG.isStandIn(require('./boot_sentinel.js')));
  assert.ok(!MG.isStandIn(null));
  assert.ok(!MG.isStandIn('PendingTurns'));
});

// ── install / seal against a fake document ───────────────────────
//
// The browser oracle covers what only a real document can decide — script
// ordering, TDZ, whether the page still boots. What it must NOT be is the
// only thing exercising install() and seal() at all, because a full browser
// run is slow and easy to skip, and the first defect here was exactly that
// shape: index.html calls ModuleGate.seal() on the module namespace, which
// had no seal, so the seal silently never ran anywhere. These tests are the
// cheap net under that class.

function fakeElement(tag) {
  return {
    tagName: String(tag).toUpperCase(),
    attrs: {},
    children: [],
    style: { cssType: '', cssText: '' },
    textContent: '',
    type: '',
    setAttribute(k, v) { this.attrs[k] = String(v); },
    getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; },
    appendChild(c) { this.children.push(c); return c; },
    addEventListener() {},
    // Mirrors how the DOM renders a subtree, which is what the banner
    // assertions in the browser oracle read.
    get text() {
      return this.textContent + this.children.map((c) => c.text).join(' ');
    },
  };
}

function fakeDoc(scriptSrcs) {
  const scripts = scriptSrcs.map((src) => {
    const el = fakeElement('script');
    el.setAttribute('src', src);
    return el;
  });
  const body = fakeElement('body');
  return {
    body,
    documentElement: fakeElement('html'),
    createElement: (t) => fakeElement(t),
    querySelectorAll: () => scripts,
    querySelector: (sel) => {
      const want = sel.replace(/^\[|\]$/g, '');
      return body.children.find((c) => c.getAttribute(want) !== null) || null;
    },
  };
}

function fakeWin(globals) {
  return Object.assign({
    addEventListener() {},
    location: { reload() {} },
    console: { error() {} },
  }, globals);
}

test('the namespace exposes seal — that is what index.html calls', function () {
  // 45 script tags separate install() from seal(); a handle carried between
  // them would have to live in the inline script this module protects.
  assert.strictEqual(typeof MG.seal, 'function');
});

test('sealing without installing is a no-op, not a throw', function () {
  // A containment module whose own API can throw is the fault it exists to
  // prevent.
  assert.doesNotThrow(() => MG.seal());
});

test('seal stands in for an absent module and names it in the banner', function () {
  const doc = fakeDoc(['scripts/fleet_row.js', 'scripts/pending_turns.js']);
  const win = fakeWin({ FleetRow: { render() { return 'row'; } } });
  const gate = MG.install(win, doc);
  const failures = gate.seal();

  assert.deepStrictEqual(failures.map((f) => f.src), ['scripts/pending_turns.js']);
  assert.ok(MG.isStandIn(win.PendingTurns), 'absent module got no stand-in');
  assert.strictEqual(win.FleetRow.render(), 'row', 'present module was overwritten');

  const banner = doc.querySelector('[' + MG.BANNER_ATTR + ']');
  assert.ok(banner, 'no banner: the degradation is silent');
  assert.ok(banner.text.indexOf('scripts/pending_turns.js') >= 0,
    'banner does not name the missing module: ' + banner.text);
});

test('seal is inert on a healthy document', function () {
  const doc = fakeDoc(['scripts/fleet_row.js', 'scripts/smd.js']);
  const win = fakeWin({ FleetRow: {}, smd: {} });
  const gate = MG.install(win, doc);
  assert.deepStrictEqual(gate.seal(), []);
  assert.strictEqual(doc.querySelector('[' + MG.BANNER_ATTR + ']'), null,
    'a healthy page was bannered');
});

test('seal reports each absence once, however often it is called', function () {
  const doc = fakeDoc(['scripts/pending_turns.js']);
  const win = fakeWin({});
  const gate = MG.install(win, doc);
  const first = gate.seal();
  assert.strictEqual(first.length, 1);
  assert.deepStrictEqual(gate.seal(), first, 'a second seal re-reported the same absence');
});

test('seal ignores CDN tags — marked and mermaid are not ours to replace', function () {
  const doc = fakeDoc(['https://cdn.jsdelivr.net/npm/marked/marked.min.js']);
  const win = fakeWin({});
  const gate = MG.install(win, doc);
  assert.deepStrictEqual(gate.seal(), []);
  assert.strictEqual(win.Marked, undefined);
});

// ── Anti-drift: the rule against the actual sources ──────────────
//
// The gated SET is read from the document at runtime, so a new module is
// covered with no edit to module_gate.js. The NAME rule is the only thing
// that can silently fall out of step, and this is what stops it: for every
// module in web/scripts/, the name the gate derives must be a global that
// file actually publishes. A module that drifts out of the rule fails here
// and is fixed by one entry in NAME_EXCEPTIONS.

const SCRIPT_DIR = __dirname;

function moduleFiles() {
  return fs.readdirSync(SCRIPT_DIR)
    .filter((f) => f.endsWith('.js') && !f.endsWith('_test.js'))
    .sort();
}

// publishesGlobal looks for the name being bound at module scope in any of
// the shapes web/scripts/ actually uses: the UMD `root.X =` idiom, a direct
// `window.X =` (jlog.js), or a top-level lexical declaration that lands in
// the global lexical environment (transport.js).
function publishesGlobal(src, name) {
  const id = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return new RegExp('(?:root|window|self)\\.' + id + '\\s*=').test(src)
    || new RegExp('^(?:const|let|var)\\s+' + id + '\\s*=', 'm').test(src);
}

test('every module in web/scripts/ publishes the global the gate derives', function () {
  const drift = [];
  for (const f of moduleFiles()) {
    const name = MG.globalNameFor(f);
    const src = fs.readFileSync(path.join(SCRIPT_DIR, f), 'utf8');
    if (!publishesGlobal(src, name)) {
      drift.push(`${f}: gate expects global "${name}", file publishes no such binding`);
    }
  }
  assert.deepStrictEqual(drift, [],
    'module gate name rule has drifted — add an entry to NAME_EXCEPTIONS:\n  '
    + drift.join('\n  '));
});

test('the exception table has no stale entries', function () {
  // A dead exception is a trap: it silently overrides the rule for a name
  // that will one day belong to a different module.
  const bases = new Set(moduleFiles().map((f) => f.replace(/\.js$/, '')));
  for (const base of Object.keys(MG.NAME_EXCEPTIONS)) {
    assert.ok(bases.has(base),
      `NAME_EXCEPTIONS has "${base}" but web/scripts/${base}.js does not exist`);
  }
});

test('the exception table carries only genuine exceptions', function () {
  // An entry that merely restates the rule is noise, and hides how few real
  // exceptions there are.
  for (const [base, name] of Object.entries(MG.NAME_EXCEPTIONS)) {
    const byRule = base.split('_')
      .filter(Boolean)
      .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
      .join('');
    assert.notStrictEqual(name, byRule,
      `NAME_EXCEPTIONS["${base}"] = "${name}" is what the rule already produces`);
  }
});

// ── index.html wiring: the module is inert unless it is wired ────

const INDEX_RAW = fs.readFileSync(path.join(SCRIPT_DIR, '..', 'index.html'), 'utf8');
// Comments are stripped before scanning for tags: the wiring comments quote
// <script src="scripts/…"> to explain themselves, and a commented-out tag
// loads nothing. Replaced with same-length padding so every index stays true.
const INDEX = INDEX_RAW.replace(/<!--[\s\S]*?-->/g, (m) => ' '.repeat(m.length));

test('the gate loads before every module it is meant to gate', function () {
  const gate = INDEX.indexOf('scripts/module_gate.js');
  assert.ok(gate > 0, 'index.html never loads scripts/module_gate.js');
  const install = INDEX.indexOf('ModuleGate.install(');
  assert.ok(install > gate, 'ModuleGate.install() does not follow its own script tag');

  // Signal (a) only contains if the listener is live before the failing
  // script is reached; every other local module must come after install.
  const re = /<script src="(scripts\/[^"]+)"/g;
  let m;
  const after = [];
  while ((m = re.exec(INDEX)) !== null) {
    if (m[1] === 'scripts/module_gate.js') continue;
    if (m[1] === 'scripts/boot_sentinel.js') continue; // 🎯T375 owns first place
    if (m.index < install) after.push(m[1]);
  }
  assert.deepStrictEqual(after, [],
    'these modules load before the gate is installed and are therefore ungated:\n  '
    + after.join('\n  '));
});

test('seal runs after the last module tag and before the inline script', function () {
  const seal = INDEX.indexOf('ModuleGate.seal(');
  assert.ok(seal > 0, 'index.html never seals the gate: a module whose factory threw is uncontained');

  const re = /<script src="(scripts\/[^"]+)"/g;
  let m, lastModule = -1;
  while ((m = re.exec(INDEX)) !== null) lastModule = m.index;
  assert.ok(seal > lastModule,
    'seal() runs before the last module tag, so later modules are never checked');

  // The load-bearing half: the throw this contains happens at the top level
  // of the big inline script, so the seal has to precede it.
  const bigScript = INDEX.indexOf('const msgs = document.getElementById(\'messages\')');
  assert.ok(bigScript > 0, 'could not locate the main inline script — this check needs updating');
  assert.ok(seal < bigScript,
    'seal() runs after the inline script starts, so it cannot contain a factory-throw for it');
});

test('web/embed.go serves the gate to released binaries too', function () {
  const embed = fs.readFileSync(path.join(SCRIPT_DIR, '..', 'embed.go'), 'utf8');
  assert.ok(/^\s*\/\/go:embed\s+scripts\/module_gate\.js\s*$/m.test(embed),
    'gate not embedded: a released jevonsd would 404 it — and it would then '
    + 'be the one module with nothing to contain its own absence');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('PASS module_gate_test');
