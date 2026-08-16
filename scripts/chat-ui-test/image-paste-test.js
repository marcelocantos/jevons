// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic 🎯T76/T224: renderUserText turns [image: id] into thumb <img>;
// upload/journal/thumb API covered by Go tests (TestImageUploadJournalHydrateThumb).
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
        // Thumb path ends with /thumb; full-res residual also stubbed.
        res.writeHead(200, { 'Content-Type': 'image/jpeg' });
        res.end(Buffer.from([0xff, 0xd8, 0xff, 0xd9]));
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
      // 🎯T224: browser hydrate uses thumb URL, not full-res / blob:.
      const want = '/api/images/deadbeefcafebabe/thumb';
      if (img.getAttribute('src') !== want) {
        return { ok: false, reason: 'src ' + img.getAttribute('src') + ' want ' + want };
      }
      const html = window.renderUserText('[image: abcdef0123456789]');
      if (html.indexOf('chat-img') < 0) return { ok: false, reason: 'renderUserText' };
      if (html.indexOf('/api/images/abcdef0123456789/thumb') < 0) {
        return { ok: false, reason: 'no thumb in html ' + html };
      }
      if (html.indexOf('blob:') >= 0) return { ok: false, reason: 'blob url in hydrate' };
      if (img.getAttribute('width') !== '160' || img.getAttribute('height') !== '120') {
        return { ok: false, reason: 'reserved box ' + img.getAttribute('width') + 'x' + img.getAttribute('height') };
      }
      return { ok: true };
    });
    if (!ok.ok) {
      console.error('FAIL', ok);
      process.exitCode = 1;
    } else {
      console.log('PASS image-paste-test (T224 thumbs)');
    }
  } finally {
    await browser.close();
    srv.close();
  }
})().catch(e => { console.error(e); process.exit(1); });
