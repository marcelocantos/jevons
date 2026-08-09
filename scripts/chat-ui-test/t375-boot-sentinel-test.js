// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T375 — a mid-edit cockpit fails loudly instead of cascading forever.
//
// THE REPRODUCTION. On 2026-08-09 at 21:47:14 the daily log took four
// window.onerror in half a second: "ReferenceError: isAgentOriginBubble is
// not defined", paintBody@/:4488 -> renderBody@/:4901 -> rematerializeMsg.
// The daemon serves web/ from disk, and a fleet worker had written a call to
// a function it had not yet written the definition for. 🎯T374's serve-time
// guard cannot see that shape: it checks that referenced FILES exist, and
// every file existed. index.html itself was the incomplete artefact.
//
// That is not a decidable condition from outside — nothing can tell a
// finished edit from an unfinished one by inspection, which is why the
// write-side answer is worktree isolation (🎯T254.2) rather than a cleverer
// static check. So the target's other branch is what this asserts: whatever
// the cause, an aborted boot must fail LOUDLY with owner-visible recovery
// and must not degrade into a permanent recurring cascade.
//
// WHY THE CASCADE RECURS, which is the part that actually hurt. index.html is
// one ~9000-line inline script. A top-level throw aborts the remainder, so
// every `let` below it stays in the temporal dead zone — but the intervals
// registered ABOVE it are already running: cost refresh 3s, composer report
// 5s, worker refresh 10s, fleet poll 30s. Each tick then throws against dead
// bindings, forever. One transient fault, a permanent error storm.
//
// THREE SCENARIOS, and the middle one is the mutation evidence:
//
//   broken + sentinel   — banner naming the cause; the error stream STOPS.
//   broken - sentinel   — the pre-fix tree, built by stripping the sentinel
//                         script tag and markBooted() from the same served
//                         HTML. No banner, and errors KEEP arriving. This is
//                         red-against-pre-fix asserted as a standing fact:
//                         it fails the day the cascade stops reproducing,
//                         which is what would make scenario 1 vacuous.
//   healthy             — over-broadness. No banner, no failure flag, and the
//                         polls still tick. A sentinel that tore down a
//                         working cockpit would be worse than the disease.
//
// Hermetic: static server over web/ + mocked agents/WS. No live daemon.
//
//   node scripts/chat-ui-test/t375-boot-sentinel-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const WEB_ROOT = path.join(__dirname, '..', '..', 'web');

// The probe symbol. The incident's own name was `isAgentOriginBubble`, but
// 🎯T381 has since landed that function — and a function declaration hoists,
// so calling it would now succeed and reproduce nothing. A synthetic name
// preserves the shape (top-level call to an identifier the mid-edit file has
// not defined yet) and cannot be accidentally healed by later work.
const MISSING_FN = '__t375MidEditUndefinedFn';

// Accelerates the product's own polling so the oracle can watch many ticks in
// a few seconds. Measured on the pre-fix tree, the recurring TDZ error lands
// on the 30s fleet poll — a faithful but 40-second test. Clamping long
// intervals makes the same cascade fire ~every 500ms instead. Applied
// identically to all three scenarios, so the comparison between them is
// unaffected; it only makes the observation window denser.
const CLOCK_CLAMP = `(function () {
  const real = window.setInterval;
  window.setInterval = function (fn, ms) {
    const args = Array.prototype.slice.call(arguments, 2);
    return real.apply(window, [fn, (ms >= 3000 ? 500 : ms)].concat(args));
  };
})();`;

// Injection anchor: a stable top-level line that sits AFTER all four polling
// intervals are registered. Injecting earlier would abort the script before
// anything recurring exists, and the cascade — the part that hurt — would not
// reproduce at all.
const ANCHOR = '// 🎯T239: durable draft + unacked wire sends';

const BANNER_SEL = '[data-jevons-boot-error]';

const AGENTS = [
  { name: 'jevons', workdir: '/Users/x/.jevons/jevons', parent: '', status: 'running', purpose: 'overseer' },
  { name: 'jevons-po', workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: 'jevons', status: 'running', purpose: 'work' },
  { name: 'jv-t375-consistent-cockpit-snapshot', workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: 'jevons-po', status: 'running', purpose: 'work' },
];

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

// The scripts/… paths named by //go:embed in web/embed.go — exactly what a
// released jevonsd can serve. Gating on it keeps this test on the daily path
// rather than on a working tree the released binary would not reproduce.
function embeddedScripts() {
  const src = fs.readFileSync(path.join(WEB_ROOT, 'embed.go'), 'utf8');
  const out = new Set();
  const re = /^\s*\/\/go:embed\s+(\S+)\s*$/gm;
  let m;
  while ((m = re.exec(src)) !== null) out.add(m[1]);
  if (!out.size) throw new Error('parsed no //go:embed entries from web/embed.go');
  return out;
}

// midEditIndex reproduces the 21:47 working tree: a call to a function that
// has not been written yet, at top level in the giant inline script.
function midEditIndex(html) {
  if (html.indexOf(ANCHOR) < 0) {
    throw new Error('injection anchor is gone from index.html; the reproduction is no longer faithful: ' + ANCHOR);
  }
  return html.replace(ANCHOR,
    MISSING_FN + '(null); // 🎯T375 injected mid-edit fault\n' + ANCHOR);
}

// preFixIndex strips the sentinel back out of whatever HTML it is given —
// the control tree. Both the load and the mark have to go: leaving either
// would make the control test something other than "before this change".
function preFixIndex(html) {
  const withoutTag = html
    .replace(/<script src="scripts\/boot_sentinel\.js"><\/script>\s*/g, '')
    .replace(/<script>if \(typeof BootSentinel[^<]*<\/script>\s*/g, '');
  const withoutMark = withoutTag.replace(/^.*__jevonsBoot\.markBooted\(\);.*$/m, '');
  if (withoutMark.indexOf('boot_sentinel.js') >= 0 || withoutMark.indexOf('markBooted') >= 0) {
    throw new Error('control tree still contains the sentinel; the mutation proves nothing');
  }
  return withoutMark;
}

function startServer(state) {
  const embedded = embeddedScripts();
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');

      if (u.pathname === '/api/log' && req.method === 'POST') {
        let body = '';
        req.on('data', (c) => { body += c; });
        req.on('end', () => {
          try {
            const e = JSON.parse(body);
            if (e && e.level === 'error' &&
                (e.msg === 'window.onerror' || e.msg === 'unhandledrejection')) {
              state.errors.push({ at: Date.now(), e: e });
            }
          } catch (_) { /* unparseable bodies are not error reports */ }
          res.writeHead(200, { 'Content-Type': 'application/json' });
          res.end('{}');
        });
        return;
      }
      if (u.pathname === '/api/agents') {
        state.apiHits.push(Date.now());
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(AGENTS));
        return;
      }
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 't375-boot-sentinel-test', ok: true }));
        return;
      }
      if (u.pathname === '/api/history' || u.pathname === '/api/chatlog') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ turns: [], meta: { working: false } }));
        return;
      }
      if (u.pathname.startsWith('/api/')) {
        state.apiHits.push(Date.now());
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end('{}');
        return;
      }

      const rel = u.pathname === '/' ? '/index.html' : u.pathname;
      const file = path.normalize(path.join(WEB_ROOT, rel));
      if (!file.startsWith(WEB_ROOT)) { res.writeHead(403); res.end(); return; }

      if (rel === '/index.html') {
        let html = fs.readFileSync(file, 'utf8');
        html = state.transform ? state.transform(html) : html;
        res.writeHead(200, { 'Content-Type': contentType(file) });
        res.end(html);
        return;
      }
      const relPosix = rel.replace(/^\//, '');
      if (relPosix.startsWith('scripts/') && !embedded.has(relPosix)) {
        res.writeHead(404);
        res.end('not embedded');
        return;
      }
      fs.readFile(file, (err, data) => {
        if (err) { res.writeHead(404); res.end('not found'); return; }
        res.writeHead(200, { 'Content-Type': contentType(file) });
        res.end(data);
      });
    });
    srv.listen(0, '127.0.0.1', () => resolve({ srv, base: `http://127.0.0.1:${srv.address().port}` }));
    srv.on('error', reject);
  });
}

// SETTLE_MS clears the sentinel's 1200ms grace with room to spare; OBSERVE_MS
// then spans ~10 ticks of the clamped 500ms polls. A cascade that is still
// running cannot hide inside that.
const SETTLE_MS = 2500;
const OBSERVE_MS = 5000;

const failures = [];
function check(cond, msg) { if (!cond) failures.push(msg); }

async function scenario(browser, state, transform, run) {
  state.errors.length = 0;
  state.apiHits.length = 0;
  state.transform = transform;
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  try {
    await page.addInitScript(CLOCK_CLAMP);
    await run(page);
  } finally {
    await page.close();
  }
}

(async () => {
  const state = { errors: [], apiHits: [], transform: null };
  const { srv, base } = await startServer(state);
  const browser = await chromium.launch({ headless: !HEADED });

  try {
    // ── 1. broken tree WITH the sentinel ─────────────────────────
    await scenario(browser, state, midEditIndex, async (page) => {
      await page.goto(base, { waitUntil: 'domcontentloaded' });
      await page.waitForSelector(BANNER_SEL, { timeout: 8000 }).catch(() => {});

      const banner = await page.$(BANNER_SEL);
      check(banner, 'broken cockpit served no recovery banner — the fault is silent, which is the whole defect');
      if (banner) {
        const text = await banner.innerText();
        check(/did not finish loading/i.test(text), 'banner does not say the cockpit is unusable: ' + text);
        check(text.indexOf(MISSING_FN) >= 0, 'banner does not name the cause ' + MISSING_FN + ': ' + text);
        check(/reload/i.test(text), 'banner offers no recovery affordance: ' + text);
      }
      const flagged = await page.evaluate(() => window.__jevonsBootFailed === true);
      check(flagged, 'window.__jevonsBootFailed not set, so later code cannot decline work on a dead page');

      // The cascade must be over, not merely quieter. Errors before the
      // sentinel settles are expected — failing loudly is the point.
      await page.waitForTimeout(SETTLE_MS);
      const mark = state.errors.length;
      await page.waitForTimeout(OBSERVE_MS);
      const after = state.errors.length - mark;
      check(after === 0,
        'cascade still recurring with the sentinel: ' + after + ' further error reports in ' +
        OBSERVE_MS + 'ms (first: ' + JSON.stringify(state.errors.slice(mark, mark + 1)) + ')');
    });

    // ── 2. the SAME broken tree WITHOUT the sentinel (control) ────
    // If this passes silently, scenario 1 proves nothing.
    let controlAfter = [];
    let controlBanner = true;
    await scenario(browser, state, (html) => preFixIndex(midEditIndex(html)), async (page) => {
      await page.goto(base, { waitUntil: 'domcontentloaded' });
      await page.waitForTimeout(SETTLE_MS);
      controlBanner = !!(await page.$(BANNER_SEL));
      const mark = state.errors.length;
      await page.waitForTimeout(OBSERVE_MS);
      controlAfter = state.errors.slice(mark);
    });
    check(!controlBanner, 'the pre-fix tree produced a banner; the control is contaminated');
    check(controlAfter.length > 0,
      'pre-fix tree emitted no recurring errors — the cascade no longer reproduces, ' +
      'so the guarded scenario is vacuous');
    // And it must be the RIGHT cascade. The recurring errors are the temporal
    // dead zone fallout (workingEl and friends, declared below the abort
    // point), not the original ReferenceError repeated. If this stops
    // holding, the scenario has drifted into reproducing something else.
    const tdz = controlAfter.filter((r) =>
      /before initialization/i.test(String((r.e.fields && r.e.fields.message) || '')));
    check(tdz.length > 0,
      'pre-fix recurrence is not the TDZ fallout the incident showed; got: ' +
      JSON.stringify(controlAfter.slice(0, 2).map((r) => r.e.fields && r.e.fields.message)));

    // ── 3. healthy tree — over-broadness ─────────────────────────
    await scenario(browser, state, null, async (page) => {
      await page.goto(base, { waitUntil: 'domcontentloaded' });
      await page.waitForFunction(
        () => typeof window.jLog === 'function' && document.getElementById('input'),
        null, { timeout: 10000 });
      await page.waitForTimeout(SETTLE_MS);

      check(!(await page.$(BANNER_SEL)), 'healthy cockpit was given a failure banner');
      const st = await page.evaluate(() => ({
        failed: window.__jevonsBootFailed === true,
        booted: !!(window.__jevonsBoot && window.__jevonsBoot.state().booted),
      }));
      check(!st.failed, 'healthy cockpit flagged as a failed boot');
      check(st.booted, 'healthy cockpit never reached markBooted() — the sentinel would misfire on every load');

      // The teardown must not have happened: the product's own polls keep
      // hitting the server. This is the assertion that a sentinel which
      // cleared a working cockpit's timers would fail.
      const mark = state.apiHits.length;
      await page.waitForTimeout(OBSERVE_MS);
      const polled = state.apiHits.length - mark;
      check(polled > 0, 'no API polling after boot — the sentinel tore down a healthy cockpit');

      check(state.errors.length === 0,
        'healthy cockpit reported ' + state.errors.length + ' uncaught errors: ' +
        JSON.stringify(state.errors.slice(0, 2)));
    });
  } catch (err) {
    failures.push('harness error: ' + (err && err.stack ? err.stack : err));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL t375-boot-sentinel-test');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('PASS t375-boot-sentinel-test');
})();
