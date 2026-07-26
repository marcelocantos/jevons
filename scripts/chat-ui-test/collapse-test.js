// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic perceptual test for bounded oversized-content rendering
// (🎯T57). Serves the static web/ UI, drives addMsg() with short + huge
// content for both roles, and asserts the WORK is bounded, not just the
// visuals:
//   * a huge bubble renders only a PREVIEW (~14 lines) up front — its
//     .msg-body node/text count is a small fraction of the full content
//   * it grows a .msg-expand toggle; clicking renders the FULL content
//     lazily (node count jumps to the full size), and re-collapses
//   * a short bubble renders in full with no toggle
// Screenshots the collapsed and expanded states into artifacts/.
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

    // Build: short reply, a huge assistant list, a huge user block, then
    // a huge assistant "latest". The two middle huge ones are NOT the last
    // message → collapsed previews; the LAST one auto-expands (owner req).
    await page.evaluate(() => {
      const N = 60;
      const bigList = Array.from({ length: N }, (_, i) => `- bullseye__bullseye_tool_${i}`).join('\n');
      const bigUser = Array.from({ length: 80 }, (_, i) => `recap line ${i}: durable conversation record continues`).join('\n');
      const latestList = Array.from({ length: N }, (_, i) => `- latest__item_${i}`).join('\n');
      window.addMsg('jevons', 'Short reply, nothing to collapse.');
      const jBig = window.addMsg('jevons', '### bullseye\n' + bigList);
      const uBig = window.addMsg('user', bigUser);
      const latest = window.addMsg('jevons', '### latest\n' + latestList);
      window._t = { N, uFullLen: bigUser.length, USER_PREVIEW_LINES: (window.USER_PREVIEW_LINES || 7) };
      window._els = { jBig, uBig, latest, short: document.querySelectorAll('#messages .msg.jevons')[0] };
    });
    // Auto-expand-latest is debounced (rAF) — let it settle.
    await page.waitForTimeout(200);

    const state = await page.evaluate(() => {
      const { jBig, uBig, latest, short } = window._els;
      const uPrevLines = uBig._body.textContent.split('\n').length;
      return {
        shortHasBtn: !!short.querySelector('.msg-expand'),
        // middle huge ones: collapsed previews, far fewer than full
        jPreviewItems: jBig._body.querySelectorAll('li').length,
        jHasBtn: !!jBig._expandBtn, jFullN: window._t.N,
        uPreviewLines: uPrevLines, uPreviewLen: uBig._body.textContent.length, uFullLen: window._t.uFullLen,
        // request preview should be ~half (USER_PREVIEW_LINES) of assistant
        userPreviewCap: 7,
        // latest huge assistant: auto-EXPANDED (full items), toggle says "less"
        latestItems: latest._body.querySelectorAll('li').length,
        latestExpanded: latest._expanded === true,
        latestLabel: latest._expandBtn ? latest._expandBtn.textContent : '',
      };
    });

    if (state.shortHasBtn) failures.push('short bubble sprouted an expand toggle (should not)');
    if (!state.jHasBtn) failures.push('huge assistant bubble has no expand toggle');
    if (state.jPreviewItems === 0) failures.push('huge assistant preview rendered no items');
    if (state.jPreviewItems >= state.jFullN) failures.push(`assistant preview rendered ${state.jPreviewItems} of ${state.jFullN} items — not bounded`);
    if (state.jPreviewItems > 20) failures.push(`assistant preview too large: ${state.jPreviewItems} items (want ~14)`);
    if (state.uPreviewLen >= state.uFullLen) failures.push('user preview is the full text — not bounded');
    // Request preview is halved (~7 lines, not ~14).
    if (state.uPreviewLines > state.userPreviewCap + 1) failures.push(`request preview ${state.uPreviewLines} lines — want ~${state.userPreviewCap} (halved)`);
    // Latest message is auto-expanded in full.
    if (!state.latestExpanded) failures.push('latest message is NOT auto-expanded');
    if (state.latestItems < state.jFullN) failures.push(`latest auto-expand rendered ${state.latestItems} of ${state.jFullN} items — not full`);
    if (!/less/i.test(state.latestLabel)) failures.push(`latest toggle label = ${JSON.stringify(state.latestLabel)}, want "Show less"`);

    await page.screenshot({ path: path.join(OUT_DIR, 'collapse-preview.png'), fullPage: true });

    // Expand the (collapsed, non-latest) assistant bubble and confirm the
    // FULL content is now built lazily.
    await page.locator('#messages .msg.jevons').nth(1).locator('.msg-expand').click();
    const expanded = await page.evaluate(() => {
      const j = window._els.jBig;
      return { items: j._body.querySelectorAll('li').length, label: j._expandBtn.textContent };
    });
    if (expanded.items < 60) failures.push(`after expand, only ${expanded.items} of 60 items rendered — full content not lazily built`);
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
  console.log('ok - oversized content renders a bounded preview + lazy full-on-expand; short content untouched');
  console.log('screenshots: artifacts/collapse-preview.png, artifacts/collapse-expanded.png');
})();
