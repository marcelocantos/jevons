// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright real-render smoke for 🎯T374: the owner cockpit raises no
// uncaught errors under normal use, and therefore posts no
// event:error:window / unhandledrejection to the daemon eventlog.
//
// Why a browser test and not a unit test: every failure this guards is an
// *evaluation-order* fault that only exists in a real document.
//   - `let` module state (workingEl, workingProgress, …) sits in the temporal
//     dead zone until its declaration executes. Function declarations hoist
//     over it, so an early caller — the first fleet poll running
//     refreshAgents → updateFleetProgress — read workingEl before line ~8030
//     ran and threw "can't access lexical declaration 'workingEl' before
//     initialization" on every single load.
//   - A missing/failed <script> leaves its global undefined, and the next
//     script's factory call dies (PendingTurns undefined → composer_persist
//     emptyPending TypeError). Script *availability* is covered separately and
//     hermetically by TestEmbedCoversIndexScripts in web/embed_test.go, which
//     catches the embedded-FS gap this disk-served test cannot see.
//
// The assertion is on the product's own reporting path, not just Playwright's
// view: jlog.js forwards window 'error' and 'unhandledrejection' to
// POST /api/log, which is what accumulates in the daemon eventlog. This server
// captures those posts, so a green run means the owner-visible symptom is
// absent at its source.
//
// The server serves scripts/ *only* for files web/embed.go actually embeds,
// and 404s the rest. That is deliberate: the live fault was invisible to a
// serve-from-disk test because dev mode reads the working tree, while the
// released daemon reads the embedded FS. Gating on the embed list makes this
// test reproduce the daily path, so a script tag added without its //go:embed
// line fails here as a cascade of real errors rather than passing quietly.
//
// Hermetic: static server over web/ + mocked agents/WS. No live daemon.
//
//   node scripts/chat-ui-test/t374-no-onerror-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const WORKER = 'jv-t374-window-onerror';
const SIBLING = 'jv-t372-parity';

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

// More than one agent on purpose: updateFleetProgress only reaches its
// rollup branch when agents.length > 1, and that branch is where the
// workingEl/workingProgress reads live.
const AGENTS = [
  { name: 'jevons', workdir: '/Users/x/.jevons/jevons', parent: '', status: 'running', purpose: 'overseer' },
  { name: 'jevons-po', workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: 'jevons', status: 'running', purpose: 'work' },
  { name: WORKER, workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: 'jevons-po', status: 'running', purpose: 'work' },
  { name: SIBLING, workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: 'jevons-po', status: 'stopped', purpose: 'work' },
];

// The set of scripts/… paths named by //go:embed lines in web/embed.go —
// i.e. exactly what a released jevonsd can serve.
function embeddedScripts(webRoot) {
  const src = fs.readFileSync(path.join(webRoot, 'embed.go'), 'utf8');
  const out = new Set();
  const re = /^\s*\/\/go:embed\s+(\S+)\s*$/gm;
  let m;
  while ((m = re.exec(src)) !== null) out.add(m[1]);
  if (!out.size) throw new Error('parsed no //go:embed entries from web/embed.go');
  return out;
}

function startStaticServer(logPosts) {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  const embedded = embeddedScripts(webRoot);
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');

      // The product's uncaught-error sink. Capture, don't just 404 it —
      // a 404 here would hide the very reports we are asserting about.
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
        res.end(JSON.stringify({ version: 't374-no-onerror-test', ok: true }));
        return;
      }
      if (u.pathname.startsWith('/api/agents/') && u.pathname.endsWith('/transcript')) {
        const who = decodeURIComponent(u.pathname.split('/')[3] || WORKER);
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
          name: who,
          turns: [{ role: 'assistant', text: 'transcript stub for ' + who, when: Date.now() - 1000 }],
        }));
        return;
      }
      if (u.pathname === '/api/history' || u.pathname === '/api/chatlog') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ turns: [], meta: { working: false } }));
        return;
      }
      // Everything else under /api answers empty rather than 404 so a stray
      // fetch cannot masquerade as a product error.
      if (u.pathname.startsWith('/api/')) {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end('{}');
        return;
      }

      const rel = u.pathname === '/' ? '/index.html' : u.pathname;
      const file = path.normalize(path.join(webRoot, rel));
      if (!file.startsWith(webRoot)) { res.writeHead(403); res.end(); return; }
      // Embedded-FS fidelity: a script the released binary cannot serve must
      // 404 here too, exactly as it does on the daily daemon.
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

function describe(e) {
  const f = (e && e.fields) || {};
  const where = f.filename ? ` (${f.filename}:${f.lineno}:${f.colno})` : '';
  return `${e.msg}: ${f.message || ''}${where}`;
}

(async () => {
  const failures = [];
  const logPosts = [];          // everything the product POSTed to /api/log
  const pageErrors = [];        // uncaught exceptions as Playwright sees them
  const { srv, base } = await startStaticServer(logPosts);
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });

  page.on('pageerror', (err) => { pageErrors.push(String(err && err.stack || err)); });

  try {
    // Step 1 — plain load. The TDZ fault fired here, before any interaction.
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.jLog === 'function' && document.getElementById('input'),
      null, { timeout: 10000 });

    // The scripts whose absence produced the live TypeError must actually
    // have defined their globals. Asserted directly so a load failure is
    // reported as itself rather than as a downstream mystery.
    const globals = await page.evaluate(() => ({
      PendingTurns: typeof window.PendingTurns,
      ComposerPersist: typeof window.ComposerPersist,
    }));
    for (const [name, t] of Object.entries(globals)) {
      if (t === 'undefined') failures.push(`global ${name} is undefined after load (script missing or threw)`);
    }

    // Step 2 — let the fleet poll land. This is the path that ran
    // refreshAgents → updateFleetProgress → read workingEl in the TDZ.
    await page.waitForFunction(
      () => document.querySelectorAll('#agents [data-agent]').length > 1,
      null, { timeout: 10000 });

    // Step 3 — main chat: type and send.
    await page.click('#input');
    await page.type('#input', 'T374 smoke: owner turn');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(400);

    // Step 4 — agent sidebar: select a worker (opens the aside + transcript).
    await page.click(`#agents [data-agent="${WORKER}"]`);
    await page.waitForTimeout(400);

    // Step 5 — switch asides. Selecting a second agent exercises the draft
    // stash / composer sync that the pending-send state feeds.
    await page.click(`#agents [data-agent="${SIBLING}"]`);
    await page.waitForTimeout(400);

    // Step 6 — RHS bottom tabs (rhsBottomTab is the sibling TDZ binding).
    for (const tab of ['transcript', 'frontier']) {
      const sel = `#rhs-tab-${tab}`;
      if (await page.$(sel)) {
        await page.click(sel);
        await page.waitForTimeout(300);
      }
    }

    // Step 7 — reload. Restore paths (durable draft / pending re-queue) run
    // against populated localStorage, which the first load did not exercise.
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.jLog === 'function' && document.getElementById('input'),
      null, { timeout: 10000 });
    await page.waitForTimeout(800);

    // ── Assertions ────────────────────────────────────────────────────
    // Third-party extension noise is an accepted residual (🎯T374), but
    // nothing loads extensions in this hermetic browser, so any report here
    // is ours.
    const reported = logPosts.filter(
      (e) => e && e.level === 'error' &&
             (e.msg === 'window.onerror' || e.msg === 'unhandledrejection'));
    for (const e of reported) {
      failures.push(`product reported to /api/log — ${describe(e)}`);
    }
    for (const e of pageErrors) {
      failures.push(`uncaught page error — ${e.split('\n')[0]}`);
    }
  } catch (err) {
    failures.push(`harness error: ${err && err.message ? err.message : String(err)}`);
  } finally {
    if (!HEADED) await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL t374-no-onerror');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('PASS t374-no-onerror — no window.onerror/unhandledrejection across load, send, agent select, aside switch, tab switch, reload');
})();
