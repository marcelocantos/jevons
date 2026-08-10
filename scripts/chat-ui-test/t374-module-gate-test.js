// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Browser oracle for 🎯T374 containment: with a cockpit module missing, the
// REST OF THE PAGE STILL COMES UP. Announcement is not containment, and this
// file exists to tell the two apart.
//
//   node scripts/chat-ui-test/t374-module-gate-test.js [--headed]
//
// WHY A REAL BROWSER. Every claim here is about evaluation order and script
// scheduling, and none of it exists outside a document:
//   - the browser dispatches a <script src> load error while it is still
//     ordering blocking classic scripts, which is the only reason the gate's
//     stand-in can be in place before the next module's factory runs;
//   - the bindings that died in the live fault are `let`s in one 9000-line
//     inline script. A binding whose declaration never executed is not
//     "undefined" — it is permanently in the temporal dead zone, and the only
//     way to ask is to touch it and see whether it throws. That is exactly
//     what probeBinding() does, so the assertion is the owner's symptom
//     rather than a proxy for it.
//
// THE PROBE. `typeof x` throws ReferenceError for a TDZ binding and returns a
// string for a live one, so an indirect eval in page scope answers "did this
// declaration ever run" directly. workingEl (:8146), agentInspectInput
// (:8783) and rhsBottomTab (:8879) are all declared BELOW the top-level call
// at :7209 that threw, so before this fix all three were dead for the life of
// the page, and every fleet poll fired against them.
//
// FIVE SCENARIOS, and the last three are mutants. A suite that only ran the
// happy path could be satisfied by a gate that does nothing but print a
// banner, so each mutant asserts that a specific WRONG gate is caught:
//
//   HEALTHY   nothing dropped. The gate must be completely inert: no banner,
//             no stand-ins, no swallowed errors. Over-broadness floor.
//   CONTAIN   pending_turns.js 404s — the live fault. booted must be true,
//             all four bindings alive, the banner must name the module, and
//             repeated fleet polls must produce ZERO window.onerror. Polls,
//             plural: the symptom that defined this target was recurring, and
//             a fix that survives only the first poll has not fixed it.
//   M1        the gate is replaced by an inert stub. Containment must now
//             FAIL. If the page comes up anyway the assertions above are not
//             measuring the gate, and this suite is worthless — so M1
//             surviving fails the run.
//   M2        the gate is replaced by a blanket swallow (window.onerror
//             returning true plus a capture-phase stopImmediatePropagation).
//             This is the named over-broad mutant. A genuine runtime error
//             thrown by a module that IS present must still reach the
//             product's own reporting path, and the banner must still name
//             the missing module. A swallow does neither.
//   M3        the gate's presence oracle is forced to answer "absent" for
//             every module, so stand-ins land on top of healthy modules. A
//             real feature must visibly break. This is what stops the gate
//             from being made "safe" by standing in for everything.
//
// Assertions are on the PRODUCT's reporting path — the POSTs jlog.js makes to
// /api/log — not merely on what Playwright can see. That is what accumulates
// in the daemon eventlog, so a green run means the owner-visible symptom is
// absent at its source. It is also what makes M2 decidable: a swallow blocks
// jlog, while Playwright's pageerror would report the exception either way.
//
// Hermetic: static server over web/ + mocked agents/WS. No live daemon.

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const WEB_ROOT = path.join(__dirname, '..', '..', 'web');
const GATE = 'scripts/module_gate.js';
const DROPPED = 'scripts/pending_turns.js';

// Bindings declared below the top-level statement that threw. Each is a `let`
// in the single inline script, so "did its declaration run" is the whole
// question, and TDZ is the whole symptom.
const TDZ_PROBES = ['pendingSendState', 'workingEl', 'agentInspectInput', 'rhsBottomTab'];

// A module that IS present, used as the source of the genuine runtime error
// M2 must not be allowed to swallow. It is the last module tag, so its UMD
// factory has already published CostDisplay by the time the appended throw
// runs — a real error from a healthy module, not a load failure in disguise.
const HEALTHY_MODULE = 'scripts/cost_display.js';
const GENUINE_ERROR = 'T374 genuine runtime fault from a present module';

const AGENTS = [
  { name: 'jevons', workdir: '/Users/x/.jevons/jevons', parent: '', status: 'running', purpose: 'overseer' },
  { name: 'jevons-po', workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: 'jevons', status: 'running', purpose: 'work' },
  { name: 'jv-t374-script-abort-root', workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: 'jevons-po', status: 'running', purpose: 'work' },
  { name: 'jv-t375-snapshot', workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: 'jevons-po', status: 'stopped', purpose: 'work' },
];

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

// The set of scripts/… paths named by //go:embed lines in web/embed.go — what
// a released jevonsd can actually serve. Serving exactly that set is what
// makes this test reproduce the daily path rather than a developer's tree.
function embeddedScripts() {
  const src = fs.readFileSync(path.join(WEB_ROOT, 'embed.go'), 'utf8');
  const out = new Set();
  const re = /^\s*\/\/go:embed\s+(\S+)\s*$/gm;
  let m;
  while ((m = re.exec(src)) !== null) out.add(m[1]);
  if (!out.size) throw new Error('parsed no //go:embed entries from web/embed.go');
  return out;
}

// ── The mutants, as served bytes ─────────────────────────────────
//
// Mutating what the server sends keeps the working tree clean, so a mutant can
// never be left behind in a commit — and every mutant that rewrites real
// source asserts its substitution site still exists, so a later refactor
// turns a silently-disabled mutant into a red test.

function inertGate() {
  // M1: the gate does nothing at all. index.html still calls install() and
  // seal(), so the page's wiring is unchanged and the ONLY variable is
  // whether containment happens.
  return `(function (root) {
  'use strict';
  function noop() {}
  root.ModuleGate = {
    install: function () { return { seal: noop, failures: function () { return []; } }; },
    seal: noop,
  };
}(self));
`;
}

function swallowingGate() {
  // M2: the rejected design, made concrete. It announces nothing and hides
  // everything — the exact failure mode a "just wrap it in try/catch" fix
  // produces, expressed at the only layer where a classic script can do it.
  return `(function (root) {
  'use strict';
  function noop() {}
  root.ModuleGate = {
    install: function (win) {
      win.addEventListener('error', function (e) {
        if (e && e.preventDefault) e.preventDefault();
        if (e && e.stopImmediatePropagation) e.stopImmediatePropagation();
      }, true);
      win.onerror = function () { return true; };
      win.addEventListener('unhandledrejection', function (e) {
        if (e && e.preventDefault) e.preventDefault();
        if (e && e.stopImmediatePropagation) e.stopImmediatePropagation();
      }, true);
      return { seal: noop, failures: function () { return []; } };
    },
    seal: noop,
  };
}(self));
`;
}

const M3_SITE = 'function globalBindingExists(win, name) {';

function overReachingGate(src) {
  // M3: force the presence oracle to answer "absent" for everything, so seal()
  // stands in on top of modules that loaded fine.
  if (src.indexOf(M3_SITE) < 0) {
    throw new Error('M3 substitution site is gone from module_gate.js: ' + M3_SITE
      + ' — the mutant is silently disabled, which is worse than no mutant');
  }
  return src.replace(M3_SITE, M3_SITE + '\n    if (name) return false;');
}

// ── Server ───────────────────────────────────────────────────────

function startServer(cfg, logPosts) {
  const embedded = embeddedScripts();
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');

      if (u.pathname === '/api/log' && req.method === 'POST') {
        let body = '';
        req.on('data', (c) => { body += c; });
        req.on('end', () => {
          try { logPosts.push(JSON.parse(body)); }
          catch (_) { logPosts.push({ level: 'error', msg: 'unparseable /api/log body', fields: { body } }); }
          res.writeHead(200, { 'Content-Type': 'application/json' });
          res.end('{}');
        });
        return;
      }
      if (u.pathname === '/api/agents') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(AGENTS));
        return;
      }
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 't374-module-gate-test', ok: true }));
        return;
      }
      if (u.pathname.startsWith('/api/agents/') && u.pathname.endsWith('/transcript')) {
        const who = decodeURIComponent(u.pathname.split('/')[3] || 'jevons');
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ name: who, turns: [] }));
        return;
      }
      if (u.pathname === '/api/history' || u.pathname === '/api/chatlog') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ turns: [], meta: { working: false } }));
        return;
      }
      if (u.pathname.startsWith('/api/')) {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end('{}');
        return;
      }

      const rel = u.pathname === '/' ? '/index.html' : u.pathname;
      const relPosix = rel.replace(/^\//, '');
      const file = path.normalize(path.join(WEB_ROOT, rel));
      if (!file.startsWith(WEB_ROOT)) { res.writeHead(403); res.end(); return; }

      // The injected fault: this module is simply not on the serving path,
      // exactly as pending_turns.js was on the day the cockpit died.
      if (cfg.drop && cfg.drop.indexOf(relPosix) >= 0) {
        res.writeHead(404); res.end('dropped by t374 oracle'); return;
      }
      if (relPosix.startsWith('scripts/') && !embedded.has(relPosix)) {
        res.writeHead(404); res.end('not embedded'); return;
      }

      fs.readFile(file, (err, data) => {
        if (err) { res.writeHead(404); res.end('not found'); return; }
        let body = data;
        if (relPosix === GATE && cfg.gate === 'M1') body = Buffer.from(inertGate());
        else if (relPosix === GATE && cfg.gate === 'M2') body = Buffer.from(swallowingGate());
        else if (relPosix === GATE && cfg.gate === 'M3') body = Buffer.from(overReachingGate(data.toString('utf8')));
        if (cfg.injectThrow && relPosix === HEALTHY_MODULE) {
          body = Buffer.concat([body, Buffer.from(`\nthrow new Error(${JSON.stringify(GENUINE_ERROR)});\n`)]);
        }
        res.writeHead(200, { 'Content-Type': contentType(file) });
        res.end(body);
      });
    });
    srv.listen(0, '127.0.0.1', () => resolve({ srv, base: `http://127.0.0.1:${srv.address().port}` }));
    srv.on('error', reject);
  });
}

// ── One run ──────────────────────────────────────────────────────

async function observe(browser, cfg) {
  const logPosts = [];
  const pageErrors = [];
  const { srv, base } = await startServer(cfg, logPosts);
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  page.on('pageerror', (e) => pageErrors.push(String((e && e.stack) || e)));

  const out = { logPosts, pageErrors, harnessError: null };
  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    // The sentinel installs first and is never gated away, so it is a stable
    // foothold even in the scenarios where the boot dies.
    await page.waitForFunction(() => !!window.__jevonsBoot, null, { timeout: 10000 })
      .catch(() => { /* recorded below as booted:false */ });
    // Past the sentinel's grace window, so booted is settled either way.
    await page.waitForTimeout(1600);

    out.booted = await page.evaluate(
      () => !!(window.__jevonsBoot && window.__jevonsBoot.state().booted));

    out.probes = await page.evaluate((names) => {
      const r = {};
      for (const n of names) {
        try {
          // Indirect eval runs in global scope, where the inline script's
          // top-level `let`s live. A binding whose declaration never ran is in
          // the temporal dead zone and throws here — that IS the fault.
          r[n] = { alive: true, type: (0, eval)('typeof ' + n) };
        } catch (e) {
          r[n] = { alive: false, error: String(e && e.message || e) };
        }
      }
      return r;
    }, TDZ_PROBES);

    out.banner = await page.evaluate(() => {
      const el = document.querySelector('[data-jevons-module-error]');
      return el ? el.textContent : null;
    });

    out.gateFailures = await page.evaluate(
      () => (window.__jevonsModuleGate ? window.__jevonsModuleGate.failures() : null));

    // The recurring half of the symptom. The live storm fired on every fleet
    // poll, so one clean load proves nothing; drive the poll repeatedly and
    // let the real intervals tick underneath.
    for (let i = 0; i < 4; i++) {
      await page.evaluate(() => {
        if (typeof window.refreshAgents === 'function') window.refreshAgents();
      });
      await page.waitForTimeout(500);
    }

    out.agentRows = await page.evaluate(
      () => document.querySelectorAll('#agents [data-agent]').length);

    // Ordinary owner use, on the same page that lost a module.
    if (await page.$('#input')) {
      await page.click('#input');
      await page.type('#input', 'T374 module gate smoke');
      await page.keyboard.press('Enter');
      await page.waitForTimeout(400);
    }
    await page.waitForTimeout(600);
  } catch (err) {
    out.harnessError = (err && err.message) || String(err);
  } finally {
    await page.close();
    srv.close();
  }

  out.reportedErrors = logPosts.filter(
    (e) => e && e.level === 'error' && (e.msg === 'window.onerror' || e.msg === 'unhandledrejection'));
  out.gateLogs = logPosts.filter(
    (e) => e && typeof e.msg === 'string' && e.msg.indexOf('module gate') === 0);
  out.sawGenuineError = logPosts.some(
    (e) => e && JSON.stringify(e).indexOf(GENUINE_ERROR) >= 0);
  return out;
}

function describeReport(e) {
  const f = (e && e.fields) || {};
  const where = f.filename ? ` (${f.filename}:${f.lineno}:${f.colno})` : '';
  return `${e.msg}: ${f.message || ''}${where}`;
}

function deadProbes(o) {
  return Object.entries(o.probes || {}).filter(([, v]) => !v.alive).map(([k]) => k);
}

// ── Assertions ───────────────────────────────────────────────────

(async () => {
  const failures = [];
  const note = (s) => failures.push(s);
  const browser = await chromium.launch({ headless: !HEADED });

  try {
    // ── HEALTHY: the gate must be invisible on a working cockpit ──
    const healthy = await observe(browser, { drop: [], gate: 'real' });
    if (healthy.harnessError) note(`HEALTHY harness error: ${healthy.harnessError}`);
    if (!healthy.booted) note('HEALTHY: cockpit did not boot with nothing dropped');
    for (const p of deadProbes(healthy)) note(`HEALTHY: ${p} is in the TDZ on a clean tree`);
    if (healthy.banner) note(`HEALTHY: gate bannered a healthy page — ${healthy.banner}`);
    if (healthy.gateFailures && healthy.gateFailures.length) {
      note(`HEALTHY: gate stood in for present modules — `
        + healthy.gateFailures.map((f) => f.src).join(', '));
    }
    for (const e of healthy.reportedErrors) note(`HEALTHY: product reported ${describeReport(e)}`);
    if (healthy.agentRows < 2) note(`HEALTHY: fleet rendered ${healthy.agentRows} rows, want > 1`);

    // ── CONTAIN: the live fault, contained ────────────────────────
    const contained = await observe(browser, { drop: [DROPPED], gate: 'real' });
    if (contained.harnessError) note(`CONTAIN harness error: ${contained.harnessError}`);
    if (!contained.booted) {
      note(`CONTAIN: boot never completed with ${DROPPED} missing — the inline `
        + 'script still aborts, so this is announcement, not containment');
    }
    for (const p of deadProbes(contained)) {
      note(`CONTAIN: ${p} is permanently in the TDZ with ${DROPPED} missing — `
        + 'the cascade is intact');
    }
    for (const e of contained.reportedErrors) {
      note(`CONTAIN: window.onerror during normal use — ${describeReport(e)}`);
    }
    if (!contained.banner || contained.banner.indexOf(DROPPED) < 0) {
      note(`CONTAIN: banner does not name ${DROPPED} (got ${JSON.stringify(contained.banner)}) `
        + '— a degraded cockpit that does not say what it lost is a silent failure');
    }
    if (!contained.gateLogs.length) {
      note('CONTAIN: nothing reported to /api/log — the daemon eventlog would '
        + 'carry no record that a module went missing');
    }
    if (contained.agentRows < 2) {
      note(`CONTAIN: fleet rendered ${contained.agentRows} rows — an unrelated `
        + 'subsystem died with the missing module');
    }

    // ── M1: without the gate, containment must fail ───────────────
    const m1 = await observe(browser, { drop: [DROPPED], gate: 'M1' });
    const m1Broke = !m1.booted || deadProbes(m1).length > 0 || m1.reportedErrors.length > 0;
    if (!m1Broke) {
      note('M1 SURVIVED: with the gate replaced by a no-op, the page still booted '
        + 'clean with a module missing. The CONTAIN assertions are therefore not '
        + 'measuring the gate, and this suite proves nothing.');
    }

    // ── M2: a blanket swallow must not pass ───────────────────────
    // Control first: with the real gate, a genuine error from a PRESENT
    // module reaches the product's reporting path untouched.
    const genuine = await observe(browser, { drop: [DROPPED], gate: 'real', injectThrow: true });
    if (!genuine.sawGenuineError) {
      note('CONTAIN: a genuine runtime error from a present module did not reach '
        + '/api/log — the gate is swallowing errors it did not cause');
    }
    if (!genuine.banner || genuine.banner.indexOf(DROPPED) < 0) {
      note(`CONTAIN: banner does not name ${DROPPED} when a present module also throws`);
    }

    const m2 = await observe(browser, { drop: [DROPPED], gate: 'M2', injectThrow: true });
    const m2Caught = !m2.sawGenuineError || !m2.banner || m2.banner.indexOf(DROPPED) < 0;
    if (!m2Caught) {
      note('M2 SURVIVED: a gate that swallows every error and names nothing passed '
        + 'the same assertions as the real one. The suite cannot tell containment '
        + 'from a blanket try/catch.');
    }

    // ── M3: standing in over a present module must break something ─
    const m3 = await observe(browser, { drop: [], gate: 'M3' });
    const m3Broke = !m3.booted || deadProbes(m3).length > 0
      || m3.reportedErrors.length > 0 || m3.agentRows < 2;
    if (!m3Broke) {
      note('M3 SURVIVED: stand-ins were installed on top of every healthy module '
        + 'and nothing observable broke. The suite would not notice a gate that '
        + 'made itself "safe" by standing in for everything.');
    }

    // Report the shape of each run so a failure is diagnosable without a rerun.
    console.log('  healthy   booted=%s dead=%j rows=%d banner=%s',
      healthy.booted, deadProbes(healthy), healthy.agentRows, !!healthy.banner);
    console.log('  contained booted=%s dead=%j rows=%d gateLogs=%d errors=%d',
      contained.booted, deadProbes(contained), contained.agentRows,
      contained.gateLogs.length, contained.reportedErrors.length);
    console.log('  M1        booted=%s dead=%j errors=%d -> %s',
      m1.booted, deadProbes(m1), m1.reportedErrors.length, m1Broke ? 'killed' : 'SURVIVED');
    console.log('  M2        genuineSeen=%s banner=%s -> %s',
      m2.sawGenuineError, !!m2.banner, m2Caught ? 'killed' : 'SURVIVED');
    console.log('  M3        booted=%s dead=%j rows=%d -> %s',
      m3.booted, deadProbes(m3), m3.agentRows, m3Broke ? 'killed' : 'SURVIVED');
  } catch (err) {
    note(`harness error: ${(err && err.message) || String(err)}`);
  } finally {
    if (!HEADED) await browser.close();
  }

  if (failures.length) {
    console.error('FAIL t374-module-gate');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('PASS t374-module-gate — a missing module leaves the cockpit booted, '
    + 'its bindings live and its polls quiet; three mutants killed');
})();
