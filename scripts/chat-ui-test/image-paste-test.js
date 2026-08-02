// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic 🎯T76: renderUserText turns [image: id] into <img>; upload API
// covered by Go tests. This checks the shipped UI render path.
//   node scripts/chat-ui-test/image-paste-test.js

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

function startServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname.startsWith('/api/images/')) {
        res.writeHead(200, { 'Content-Type': 'image/png' });
        res.end(Buffer.from([0x89, 0x50, 0x4e, 0x47]));
        return;
      }
      const rel = u.pathname === '/' ? '/index.html' : u.pathname;
      const file = path.normalize(path.join(webRoot, rel));
      if (!file.startsWith(webRoot)) { res.writeHead(403); res.end(); return; }
      fs.readFile(file, (err, data) => {
        if (err) { res.writeHead(404); res.end(); return; }
        const ct = file.endsWith('.js') ? 'application/javascript' : 'text/html; charset=utf-8';
        res.writeHead(200, { 'Content-Type': ct });
        res.end(data);
      });
    });
    srv.listen(0, '127.0.0.1', () => resolve({ srv, base: `http://127.0.0.1:${srv.address().port}` }));
  });
}

(async () => {
  const { srv, base } = await startServer();
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.addMsg === 'function' && typeof window.renderUserText === 'function');

    const ok = await page.evaluate(() => {
      window.addMsg('user', '[image: deadbeefcafebabe] look at this');
      const img = document.querySelector('#messages .msg.user img.chat-img');
      if (!img) return { ok: false, reason: 'no img' };
      if (img.getAttribute('src') !== '/api/images/deadbeefcafebabe') {
        return { ok: false, reason: 'src ' + img.getAttribute('src') };
      }
      const html = window.renderUserText('[image: abcdef0123456789]');
      if (html.indexOf('chat-img') < 0) return { ok: false, reason: 'renderUserText' };
      return { ok: true };
    });
    if (!ok.ok) {
      console.error('FAIL', ok);
      process.exitCode = 1;
    } else {
      console.log('PASS image-paste-test');
    }
  } finally {
    await browser.close();
    srv.close();
  }
})().catch(e => { console.error(e); process.exit(1); });
