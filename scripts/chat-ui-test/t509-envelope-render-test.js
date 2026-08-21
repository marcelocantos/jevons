// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T509 — cockpit paints a jevons envelope as a compact header, not a
// raw fence dump. YAML front matter is the format we refused because
// mainstream markdown renderers hide it; this fence must survive paint.
//
//   node scripts/chat-ui-test/t509-envelope-render-test.js [--headed]

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
        res.end(JSON.stringify({ version: 't509-envelope-render', ok: true }));
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

const REPORT = [
  '```jevons',
  'jevons: kind finish-report',
  'jevons: target T509',
  'jevons: oracle sha=abcdef0123456',
  'jevons: verdict GREEN',
  'jevons: status in-progress',
  '```',
  '',
  'Work landed. SHA abcdef0123456.',
].join('\n');

(async () => {
  const failures = [];
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1000, height: 800 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.handle === 'function' && typeof window.JevonsEnvelope !== 'undefined' && !!window.marked,
      null, { timeout: 10000 },
    );

    await page.evaluate((report) => {
      window.handle({
        type: 'user',
        turn_origin: 'agent',
        message: { role: 'user', content: report },
      });
    }, REPORT);

    await page.waitForTimeout(250);

    const state = await page.evaluate(() => {
      const msgs = document.getElementById('messages');
      const bubble = msgs && msgs.querySelector('.msg.user');
      const body = bubble && (bubble.querySelector('.msg-body') || bubble);
      const header = body && body.querySelector('.jevons-envelope');
      const pres = body ? Array.from(body.querySelectorAll('pre')) : [];
      const headerRect = header ? header.getBoundingClientRect() : null;
      return {
        hasBubble: !!bubble,
        isAgentReport: !!(bubble && bubble.classList.contains('agent-report')),
        headerText: header ? (header.textContent || '') : '',
        headerVisible: !!(headerRect && headerRect.height > 0 && headerRect.width > 0),
        bodyText: body ? (body.textContent || '') : '',
        bodyHTML: body ? (body.innerHTML || '') : '',
        preDumpingSlots: pres.some((p) => (p.textContent || '').indexOf('jevons: kind') >= 0),
        payloadVisible: body ? (body.textContent || '').indexOf('Work landed') >= 0 : false,
      };
    });

    if (!state.hasBubble) failures.push('no user bubble painted');
    if (!state.isAgentReport) failures.push('bubble missing .agent-report — provenance did not reach paint');
    if (!state.headerVisible) failures.push('compact .jevons-envelope header is not visible (height/width)');
    if (state.headerText.indexOf('finish-report') < 0) failures.push('header missing kind text, got: ' + JSON.stringify(state.headerText));
    if (state.headerText.indexOf('T509') < 0) failures.push('header missing target');
    if (state.headerText.indexOf('GREEN') < 0) failures.push('header missing verdict');
    if (state.preDumpingSlots) failures.push('slot lines dumped as a <pre> code block — compact header required');
    if (!state.payloadVisible) failures.push('payload prose not visible');
    if (state.bodyText.indexOf('```jevons') >= 0) failures.push('raw fence opener still visible in the bubble');

    await page.screenshot({ path: path.join(OUT_DIR, 't509-envelope-render.png') });
  } catch (err) {
    failures.push(String(err && err.stack ? err.stack : err));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL t509 envelope render:');
    failures.forEach((f) => console.error('  -', f));
    process.exit(1);
  }
  console.log('ok  - t509 envelope renders as a compact visible header');
})();
