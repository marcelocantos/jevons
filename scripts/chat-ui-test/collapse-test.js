// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic perceptual test for oversized-content collapse (🎯T55).
// Serves the static web/ UI, drives addMsg() with short + huge content
// for both roles, and asserts:
//   * a huge bubble's .msg-body is .clamped and bounded (< 32em tall)
//   * it grows a .msg-expand toggle; clicking removes the clamp
//   * a short bubble is neither clamped nor given a toggle
// Screenshots the clamped and expanded states into artifacts/.
//
//   node scripts/chat-ui-test/collapse-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const OUT_DIR = path.join(__dirname, 'artifacts');
fs.mkdirSync(OUT_DIR, { recursive: true });

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 'collapse-test', ok: true }));
        return;
      }
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
  const page = await browser.newPage({ viewport: { width: 1200, height: 900 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.addMsg === 'function' && !!window.marked, null, { timeout: 10000 });

    // Build a huge markdown list (like a big tool dump) + a huge user block.
    const state = await page.evaluate(() => {
      const bigList = Array.from({ length: 60 }, (_, i) => `- bullseye__bullseye_tool_${i}`).join('\n');
      const bigUser = Array.from({ length: 80 }, (_, i) => `recap line ${i}: the durable conversation record continues here`).join('\n');
      window.addMsg('jevons', 'Short reply, nothing to collapse.');
      const jBig = window.addMsg('jevons', '### bullseye\n' + bigList);
      const uBig = window.addMsg('user', bigUser);
      const shortBody = document.querySelectorAll('#messages .msg.jevons .msg-body')[0];
      const jBody = jBig._body, uBody = uBig._body;
      const emPx = parseFloat(getComputedStyle(document.body).fontSize);
      return {
        shortClamped: shortBody.classList.contains('clamped'),
        shortHasBtn: !!document.querySelectorAll('#messages .msg.jevons')[0].querySelector('.msg-expand'),
        jClamped: jBody.classList.contains('clamped'),
        jClientPx: jBody.clientHeight,
        jScrollPx: jBody.scrollHeight,
        uClamped: uBody.classList.contains('clamped'),
        capPx: 30 * emPx,
      };
    });

    if (state.shortClamped) failures.push('short bubble was clamped (should not be)');
    if (state.shortHasBtn) failures.push('short bubble sprouted an expand toggle (should not)');
    if (!state.jClamped) failures.push('huge assistant bubble was NOT clamped');
    if (!state.uClamped) failures.push('huge user bubble was NOT clamped');
    if (state.jScrollPx <= state.jClientPx + 8) failures.push('huge assistant bubble did not actually overflow the clamp');
    if (state.jClientPx > state.capPx + 24) failures.push(`clamped height ${state.jClientPx}px exceeds cap ~${Math.round(state.capPx)}px`);

    // Toggles present on both huge bubbles.
    const jBtns = await page.locator('#messages .msg.jevons .msg-expand').count();
    const uBtns = await page.locator('#messages .msg.user .msg-expand').count();
    if (jBtns !== 1) failures.push(`expected 1 assistant expand toggle, got ${jBtns}`);
    if (uBtns !== 1) failures.push(`expected 1 user expand toggle, got ${uBtns}`);

    await page.screenshot({ path: path.join(OUT_DIR, 'collapse-clamped.png'), fullPage: true });

    // Expand the assistant bubble and confirm the clamp lifts.
    await page.locator('#messages .msg.jevons .msg-expand').click();
    const expanded = await page.evaluate(() => {
      const j = document.querySelectorAll('#messages .msg.jevons .msg-body')[1];
      const btn = document.querySelectorAll('#messages .msg.jevons .msg-expand')[0];
      return { clamped: j.classList.contains('clamped'), label: btn.textContent };
    });
    if (expanded.clamped) failures.push('clicking Show more did not lift the clamp');
    if (!/less/i.test(expanded.label)) failures.push(`toggle label after expand = ${JSON.stringify(expanded.label)}, want "Show less"`);

    await page.screenshot({ path: path.join(OUT_DIR, 'collapse-expanded.png'), fullPage: true });
  } catch (e) {
    failures.push('exception: ' + e.message);
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL collapse-test:');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('ok - oversized content clamps with an expand toggle; short content untouched');
  console.log('screenshots: artifacts/collapse-clamped.png, artifacts/collapse-expanded.png');
})();
