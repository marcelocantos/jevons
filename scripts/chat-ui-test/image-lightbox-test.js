// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic 🎯T258: click chat-img → full-res lightbox; Esc/backdrop close;
// multi-id carousel (arrows / prev-next) among same-message siblings.
//   node scripts/chat-ui-test/image-lightbox-test.js

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
        // Distinguish full-res vs thumb in Content-Type for assertions.
        const isThumb = /\/thumb$/.test(u.pathname);
        res.writeHead(200, {
          'Content-Type': isThumb ? 'image/jpeg' : 'image/png',
          'X-Jevons-Image-Path': u.pathname,
        });
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
    await page.waitForFunction(() =>
      typeof window.addMsg === 'function' &&
      typeof window.renderUserText === 'function' &&
      typeof window.ImageLightbox !== 'undefined',
    );

    // ── Open single image → full-res src (not thumb) ───────────
    const openSingle = await page.evaluate(() => {
      window.addMsg('user', '[image: deadbeefcafebabe] look');
      const img = document.querySelector('#messages .msg.user img.chat-img');
      if (!img) return { ok: false, reason: 'no chat-img' };
      if (img.getAttribute('data-image-id') !== 'deadbeefcafebabe') {
        return { ok: false, reason: 'missing data-image-id' };
      }
      img.click();
      const lb = document.getElementById('img-lightbox');
      if (!lb || lb.hidden) return { ok: false, reason: 'lightbox not open' };
      const full = document.getElementById('ilb-img');
      if (!full || !full.getAttribute('src')) return { ok: false, reason: 'no full img src' };
      const src = full.getAttribute('src');
      if (src !== '/api/images/deadbeefcafebabe') {
        return { ok: false, reason: 'want full-res got ' + src };
      }
      if (src.indexOf('/thumb') >= 0) return { ok: false, reason: 'thumb in overlay' };
      return { ok: true };
    });
    if (!openSingle.ok) {
      console.error('FAIL open-single', openSingle);
      process.exitCode = 1;
      return;
    }

    // ── Esc closes ─────────────────────────────────────────────
    await page.keyboard.press('Escape');
    const closedEsc = await page.evaluate(() => {
      const lb = document.getElementById('img-lightbox');
      return !!(lb && lb.hidden);
    });
    if (!closedEsc) {
      console.error('FAIL esc-close');
      process.exitCode = 1;
      return;
    }

    // ── Multi-id carousel: arrows + counter ────────────────────
    const multi = await page.evaluate(() => {
      // Clear messages for a clean multi case.
      const msgs = document.getElementById('messages');
      if (msgs) msgs.innerHTML = '';
      window.addMsg('user',
        '[image: aaa111aaa111aaaa] first [image: bbb222bbb222bbbb] second [image: ccc333ccc333cccc] third');
      const imgs = document.querySelectorAll('#messages .msg.user img.chat-img');
      if (imgs.length !== 3) return { ok: false, reason: 'want 3 imgs got ' + imgs.length };
      // Click middle image.
      imgs[1].click();
      const lb = document.getElementById('img-lightbox');
      if (!lb || lb.hidden) return { ok: false, reason: 'multi not open' };
      const full = document.getElementById('ilb-img');
      const src = full && full.getAttribute('src');
      if (src !== '/api/images/bbb222bbb222bbbb') {
        return { ok: false, reason: 'start id src ' + src };
      }
      const counter = document.getElementById('ilb-counter');
      const ct = counter ? counter.textContent.trim() : '';
      if (ct !== '2 / 3') return { ok: false, reason: 'counter ' + ct };
      return { ok: true };
    });
    if (!multi.ok) {
      console.error('FAIL multi-open', multi);
      process.exitCode = 1;
      return;
    }

    await page.keyboard.press('ArrowRight');
    const afterRight = await page.evaluate(() => {
      const full = document.getElementById('ilb-img');
      const src = full && full.getAttribute('src');
      const ct = (document.getElementById('ilb-counter') || {}).textContent || '';
      if (src !== '/api/images/ccc333ccc333cccc') return { ok: false, reason: 'src ' + src };
      if (ct.trim() !== '3 / 3') return { ok: false, reason: 'counter ' + ct };
      return { ok: true };
    });
    if (!afterRight.ok) {
      console.error('FAIL arrow-right', afterRight);
      process.exitCode = 1;
      return;
    }

    await page.keyboard.press('ArrowLeft');
    await page.keyboard.press('ArrowLeft');
    const afterLeft = await page.evaluate(() => {
      const full = document.getElementById('ilb-img');
      const src = full && full.getAttribute('src');
      if (src !== '/api/images/aaa111aaa111aaaa') return { ok: false, reason: 'src ' + src };
      return { ok: true };
    });
    if (!afterLeft.ok) {
      console.error('FAIL arrow-left', afterLeft);
      process.exitCode = 1;
      return;
    }

    // ── Backdrop click closes ──────────────────────────────────
    await page.evaluate(() => {
      const lb = document.getElementById('img-lightbox');
      if (lb) lb.click(); // click on root = backdrop
    });
    const closedBackdrop = await page.evaluate(() => {
      const lb = document.getElementById('img-lightbox');
      return !!(lb && lb.hidden);
    });
    if (!closedBackdrop) {
      console.error('FAIL backdrop-close');
      process.exitCode = 1;
      return;
    }

    // ── Pure model still available (open/close) ────────────────
    const model = await page.evaluate(() => {
      let s = window.ImageLightbox.open({ ids: ['aa', 'bb'], id: 'bb' });
      if (!window.ImageLightbox.isOpen(s)) return { ok: false, reason: 'open' };
      if (window.ImageLightbox.currentId(s) !== 'bb') return { ok: false, reason: 'id' };
      s = window.ImageLightbox.next(s);
      if (window.ImageLightbox.currentId(s) !== 'aa') return { ok: false, reason: 'wrap' };
      const r = window.ImageLightbox.handleKey(s, 'Escape');
      if (r.action !== 'close' || r.session.open) return { ok: false, reason: 'esc' };
      return { ok: true };
    });
    if (!model.ok) {
      console.error('FAIL model', model);
      process.exitCode = 1;
      return;
    }

    console.log('PASS image-lightbox-test (T258 open/close + multi carousel)');
  } finally {
    await browser.close();
    srv.close();
  }
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
