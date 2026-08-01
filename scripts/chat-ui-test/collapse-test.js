// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic perceptual test for bounded oversized-content rendering
// (🎯T55/T57/T66/T77). Serves the static web/ UI, drives addMsg() and the
// streaming append/seal path with short + huge content for both roles, and
// asserts the WORK is bounded, not just the visuals:
//   * tallness = rendered full height > 1.5 × collapsed-preview height
//     (not char/line proxies)
//   * a tall non-latest bubble renders only a PREVIEW (~14 lines) — its
//     .msg-body node/text count is a small fraction of the full content
//   * it grows a .msg-expand toggle; clicking renders the FULL content
//     lazily (node count jumps to the full size), and re-collapses
//   * a short bubble renders in full with no toggle
//   * T66: latest request/response stay expanded when tall (incl. stream
//     that grows short→tall)
//   * T77: when either role ceases to be latest, auto-expand reverts to preview
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

    // ── Scenario A (T55/T57/T66): short + middle huge (both roles) + latest huge assistant ──
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
        jExpanded: jBig._expanded === true,
        uPreviewLines: uPrevLines, uPreviewLen: uBig._body.textContent.length, uFullLen: window._t.uFullLen,
        uExpanded: uBig._expanded === true,
        uHasBtn: !!uBig._expandBtn,
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
    // Latest message is auto-expanded in full (T66).
    if (!state.latestExpanded) failures.push('latest message is NOT auto-expanded');
    if (state.latestItems < state.jFullN) failures.push(`latest auto-expand rendered ${state.latestItems} of ${state.jFullN} items — not full`);
    if (!/less/i.test(state.latestLabel)) failures.push(`latest toggle label = ${JSON.stringify(state.latestLabel)}, want "Show less"`);
    // T77: non-latest oversized of BOTH roles stay collapsed.
    if (state.jExpanded) failures.push('T77: non-latest huge assistant is expanded (want collapsed preview)');
    if (state.uExpanded) failures.push('T77: non-latest huge user is expanded (want collapsed preview)');
    if (!state.uHasBtn) failures.push('T77: non-latest huge user has no expand toggle');

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

    // ── Scenario B (T66 stream): small first chunk, grow until tall, seal ──
    // Simulates live assistant streaming: first frame is tiny, so the early
    // scheduleLatestExpansion must not lock the bubble as permanently
    // non-expanded when it later measures tall enough to collapse.
    await page.evaluate(() => {
      document.getElementById('messages').innerHTML = '';
      // Reset module-level latest tracking by driving through the real APIs.
      // appendOrAddJevons / sealAssistantStream are in page scope.
      window.appendOrAddJevons('hi'); // small open
    });
    await page.waitForTimeout(50);
    await page.evaluate(() => {
      const big = Array.from({ length: 60 }, (_, i) => `- stream__item_${i}`).join('\n');
      window.appendOrAddJevons('\n### stream\n' + big);
    });
    await page.waitForTimeout(100);
    await page.evaluate(() => { window.sealAssistantStream(); });
    await page.waitForTimeout(200);

    const streamState = await page.evaluate(() => {
      const el = document.querySelector('#messages .msg.jevons');
      if (!el) return { missing: true };
      return {
        expanded: el._expanded === true,
        auto: el._autoExpanded === true,
        items: el._body.querySelectorAll('li').length,
        label: el._expandBtn ? el._expandBtn.textContent : '',
        hasFull: el._fullText != null,
      };
    });
    if (streamState.missing) failures.push('T66 stream: no assistant bubble after seal');
    else {
      if (!streamState.hasFull) failures.push('T66 stream: sealed bubble has no _fullText (not oversized?)');
      if (!streamState.expanded) failures.push('T66 stream: latest assistant after small→large stream is NOT expanded');
      if (!streamState.auto) failures.push('T66 stream: latest assistant missing _autoExpanded');
      if (streamState.items < 60) failures.push(`T66 stream: only ${streamState.items}/60 items rendered after auto-expand`);
      if (!/less/i.test(streamState.label || '')) failures.push(`T66 stream: label = ${JSON.stringify(streamState.label)}, want "Show less"`);
    }

    // ── Scenario C (T77): large user then large assistant — user collapses ──
    await page.evaluate(() => {
      document.getElementById('messages').innerHTML = '';
      const bigUser = Array.from({ length: 80 }, (_, i) => `user line ${i}`).join('\n');
      const bigAsst = Array.from({ length: 60 }, (_, i) => `- asst_${i}`).join('\n');
      const u = window.addMsg('user', bigUser);
      window._els2 = { u, asst: null, bigAsst };
      window._els2.asstText = '### reply\n' + bigAsst;
    });
    await page.waitForTimeout(150);
    const afterUser = await page.evaluate(() => {
      const u = window._els2.u;
      return { expanded: u._expanded === true, auto: u._autoExpanded === true };
    });
    if (!afterUser.expanded) failures.push('T77 setup: latest huge user did not auto-expand');

    await page.evaluate(() => {
      window._els2.asst = window.addMsg('jevons', window._els2.asstText);
    });
    await page.waitForTimeout(200);

    const afterAsst = await page.evaluate(() => {
      const { u, asst } = window._els2;
      return {
        uExpanded: u._expanded === true,
        uAuto: u._autoExpanded === true,
        uPreviewish: u._body.textContent.split('\n').length <= 10,
        aExpanded: asst._expanded === true,
        aItems: asst._body.querySelectorAll('li').length,
        aLabel: asst._expandBtn ? asst._expandBtn.textContent : '',
      };
    });
    if (afterAsst.uExpanded) failures.push('T77: large user still expanded after newer assistant arrived');
    if (afterAsst.uAuto) failures.push('T77: large user still _autoExpanded after ceasing to be latest');
    if (!afterAsst.uPreviewish) failures.push('T77: large user body still full-height after collapse');
    if (!afterAsst.aExpanded) failures.push('T66/T77: latest large assistant not expanded after displacing user');
    if (afterAsst.aItems < 60) failures.push(`T66: latest assistant rendered ${afterAsst.aItems}/60 items`);
    if (!/less/i.test(afterAsst.aLabel || '')) failures.push(`T66: latest assistant label = ${JSON.stringify(afterAsst.aLabel)}`);

    // ── Scenario D (T77): large assistant then large user — assistant collapses ──
    await page.evaluate(() => {
      document.getElementById('messages').innerHTML = '';
      const bigAsst = Array.from({ length: 60 }, (_, i) => `- prior_${i}`).join('\n');
      const bigUser = Array.from({ length: 80 }, (_, i) => `next user ${i}`).join('\n');
      const a = window.addMsg('jevons', '### prior\n' + bigAsst);
      window._els3 = { a, u: null, bigUser };
    });
    await page.waitForTimeout(150);
    await page.evaluate(() => {
      window._els3.u = window.addMsg('user', window._els3.bigUser);
    });
    await page.waitForTimeout(200);

    const afterUser2 = await page.evaluate(() => {
      const { a, u } = window._els3;
      return {
        aExpanded: a._expanded === true,
        aAuto: a._autoExpanded === true,
        aPreviewItems: a._body.querySelectorAll('li').length,
        uExpanded: u._expanded === true,
        uLines: u._body.textContent.split('\n').length,
      };
    });
    if (afterUser2.aExpanded) failures.push('T77: large assistant still expanded after newer user arrived');
    if (afterUser2.aAuto) failures.push('T77: large assistant still _autoExpanded after ceasing to be latest');
    if (afterUser2.aPreviewItems >= 60) failures.push('T77: non-latest assistant still full-rendered');
    if (!afterUser2.uExpanded) failures.push('T77: latest large user not auto-expanded');

    // Manual toggle wins: expand non-latest assistant, then add a new msg —
    // manual expand must survive (not forced closed by auto path on others).
    await page.evaluate(() => {
      const a = window._els3.a;
      // Simulate click: user expanded the non-latest assistant.
      a._expanded = true;
      a._userToggled = true;
      window.renderBody(a, a._fullRole, a._fullText);
      a._expandBtn.textContent = 'Show less ▴';
      window.addMsg('jevons', 'short trailing');
    });
    await page.waitForTimeout(200);
    const manual = await page.evaluate(() => {
      const a = window._els3.a;
      return { expanded: a._expanded === true, userToggled: a._userToggled === true, items: a._body.querySelectorAll('li').length };
    });
    if (!manual.userToggled) failures.push('manual toggle flag lost');
    if (!manual.expanded) failures.push('T77: manual expand of non-latest was undone by later messages');
    if (manual.items < 60) failures.push('T77: manual expand lost full content after later message');

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
  console.log('ok - height-ratio collapse (full > 1.5× preview), T66 latest request/response stay expanded, T77 non-latest collapse');
  console.log('screenshots: artifacts/collapse-preview.png, artifacts/collapse-expanded.png');
})();
