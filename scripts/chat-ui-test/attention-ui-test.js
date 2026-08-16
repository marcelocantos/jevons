// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic UI oracle for 🎯T65 / 🎯T105.2 prefix-first attention threads.
// Asserts: no Capture/Aside/Main button bar; no "Focus: main" chrome;
// main placeholder clean; side focus uses [aside: title] placeholder form.
//
//   node scripts/chat-ui-test/attention-ui-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);
const HEADED = process.argv.includes('--headed');

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  return 'application/octet-stream';
}

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      const rel = u.pathname === '/' ? '/index.html' : u.pathname;
      const file = path.normalize(path.join(webRoot, rel));
      if (!file.startsWith(webRoot)) { res.writeHead(403); res.end(); return; }
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

(async () => {
  const failures = [];
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1100, height: 800 } });
  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.AttentionThreads !== 'undefined' || typeof AttentionThreads !== 'undefined', null, { timeout: 10000 });

    const snap = await page.evaluate(() => {
      const body = document.body.innerText || '';
      const html = document.body.innerHTML || '';
      const input = document.getElementById('input');
      const ph = input ? (input.placeholder || '') : '';
      // Buttons that must NOT exist as primary chrome (prefix-first).
      const banned = [];
      document.querySelectorAll('button').forEach(b => {
        const t = (b.textContent || '').trim();
        if (/^(Capture|Aside|Main|Park|Pursue)$/i.test(t)) banned.push(t);
      });
      const focusChrome = /Focus:\s*main/i.test(body) || /Focus:\s*main/i.test(html);
      // Drive placeholder via AttentionThreads API (shipped module).
      const AT = window.AttentionThreads || AttentionThreads;
      const mainPh = AT.composerPlaceholder(AT.emptyState(), 'Message...');
      let sideState = AT.emptyState();
      const cap = AT.handleComposer(sideState, 'aside: billing nit');
      // handleComposer may return new state
      if (cap && cap.state) sideState = cap.state;
      // force side focus if needed
      if (AT.isMainFocus(sideState) && sideState.threads && sideState.threads[0]) {
        sideState = Object.assign({}, sideState, { focusId: sideState.threads[0].id });
      }
      // pursue/open path: open aside then focus
      let st2 = AT.emptyState();
      const r = AT.handleComposer(st2, 'aside: ship checklist');
      st2 = (r && r.state) || st2;
      if (r && r.kind === 'send' && st2.threads && st2.threads.length) {
        // some implementations keep main focus after aside send — open thread focus for placeholder form
        const open = st2.threads.find(t => t.status !== 'parked') || st2.threads[0];
        if (open) st2 = Object.assign({}, st2, { focusId: open.id });
      }
      const sidePh = AT.composerPlaceholder(st2, 'Message...');
      return {
        banned,
        focusChrome,
        mainPh,
        sidePh,
        inputPh: ph,
      };
    });

    if (snap.banned.length) failures.push('button-primary chrome present: ' + snap.banned.join(','));
    if (snap.focusChrome) failures.push('Focus: main chrome visible');
    if (snap.mainPh !== 'Message...' || /\[aside:/i.test(snap.mainPh)) {
      failures.push('main placeholder not clean: ' + JSON.stringify(snap.mainPh));
    }
    if (!/\[aside:/i.test(snap.sidePh)) {
      failures.push('side placeholder missing [aside: title] form: ' + JSON.stringify(snap.sidePh));
    }
    // Live input should not advertise Focus: main
    if (/Focus:\s*main/i.test(snap.inputPh)) {
      failures.push('input placeholder has Focus: main');
    }
  } catch (e) {
    failures.push(String(e && e.stack || e));
  } finally {
    await browser.close();
    srv.close();
  }
  if (failures.length) {
    console.error('FAIL attention-ui-test:');
    for (const f of failures) console.error('  -', f);
    process.exit(1);
  }
  console.log('ok - T65 prefix-first UI: no command buttons; clean main ph; [aside:] side ph (🎯T105.2)');
})();
