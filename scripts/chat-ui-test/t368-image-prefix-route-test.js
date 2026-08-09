// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic real-render oracle for 🎯T368: an attached image must not defeat a
// composer command. The composer prepends "[image: id]" markers before the
// draft, which used to push "target:" / "aside:" / "capture:" off the start of
// the string, so PREFIX_RE missed and the filing went to main chat instead.
//
// Drives the shipped product path in a browser: real paste event → real
// /api/images upload → real chip → real Enter, and asserts on the wire text
// and on what main actually paints.
//
//   node scripts/chat-ui-test/t368-image-prefix-route-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);
const HEADED = process.argv.includes('--headed');

const IMAGE_ID = 'd592b0380b1a9e9b';
const MARKER = '[image: ' + IMAGE_ID + ']';
const BODY = 'T368 image prefixed filing body';

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

// Static web/ plus the two POST endpoints this path touches: image upload and
// aside register. Both answer as the daemon does, so the page takes the same
// branches it takes live.
function startServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  const asidePosts = [];
  return new Promise((resolve) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (req.method === 'POST' && u.pathname === '/api/images') {
        req.resume();
        req.on('end', () => {
          res.writeHead(200, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({
            id: IMAGE_ID,
            ext: '.png',
            url: '/api/images/' + IMAGE_ID,
            thumb_url: '/api/images/' + IMAGE_ID + '/thumb',
            marker: MARKER,
          }));
        });
        return;
      }
      if (req.method === 'POST' && u.pathname === '/api/asides') {
        let raw = '';
        req.on('data', (c) => { raw += c; });
        req.on('end', () => {
          try { asidePosts.push(JSON.parse(raw)); } catch (_) { asidePosts.push({ raw: raw }); }
          res.writeHead(200, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ ok: true, name: 'aside-t368' }));
        });
        return;
      }
      if (u.pathname.startsWith('/api/images/')) {
        res.writeHead(200, { 'Content-Type': 'image/jpeg' });
        res.end(Buffer.from([0xff, 0xd8, 0xff, 0xd9]));
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
    srv.listen(0, '127.0.0.1', () => {
      resolve({ srv, base: `http://127.0.0.1:${srv.address().port}`, asidePosts });
    });
  });
}

// Mock chat socket: opens, records sends, echoes the owner turn back the way
// the daemon does so main painting is exercised for real.
function installMockWebSocket() {
  class MockWebSocket {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;
    constructor(url) {
      this.url = url;
      this.readyState = MockWebSocket.CONNECTING;
      this.onopen = null;
      this.onclose = null;
      this.onerror = null;
      this.onmessage = null;
      queueMicrotask(() => {
        this.readyState = MockWebSocket.OPEN;
        if (this.onopen) this.onopen({});
        if (String(url).indexOf('/ws/chat') !== -1) {
          this._emit({
            type: 'history_meta',
            older: 0, start: 0, total: 0,
            replay_frames: 0, replay_bytes: 0, replay_ms: 0,
          });
        }
      });
      window.__mockSockets = window.__mockSockets || [];
      window.__mockSockets.push(this);
      window.__chatSends = window.__chatSends || [];
    }
    send(data) {
      window.__chatSends = window.__chatSends || [];
      window.__chatSends.push(data);
      if (typeof data !== 'string') return;
      if (data === '{"type":"ping"}') { this._emit({ type: 'pong' }); return; }
      if (data.startsWith('{')) return;
      this._emit({ type: 'user', message: { role: 'user', content: data } });
      this._emit({ type: 'result', subtype: 'success' });
    }
    close() {
      this.readyState = MockWebSocket.CLOSED;
      if (this.onclose) this.onclose({});
    }
    _emit(obj) {
      if (this.onmessage) this.onmessage({ data: JSON.stringify(obj) });
    }
  }
  window.WebSocket = MockWebSocket;
}

// Real paste of a real PNG through the product's paste listener.
async function pasteImage(page) {
  await page.evaluate(() => {
    // 1×1 PNG.
    const b64 = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    const file = new File([bytes], 'paste.png', { type: 'image/png' });
    const dt = new DataTransfer();
    dt.items.add(file);
    const input = document.getElementById('input');
    input.focus();
    input.dispatchEvent(new ClipboardEvent('paste', {
      clipboardData: dt, bubbles: true, cancelable: true,
    }));
  });
  await page.waitForFunction(
    () => document.querySelectorAll('#composer-images .img-chip').length > 0,
    null,
    { timeout: 5000 }
  );
}

function textSendsOf(sends) {
  return sends.filter((s) => typeof s === 'string' && !s.startsWith('{'));
}

(async () => {
  const failures = [];
  const { srv, base, asidePosts } = await startServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1100, height: 800 } });
  try {
    await page.addInitScript(installMockWebSocket);
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.AttentionThreads !== 'undefined' && document.getElementById('input'),
      null,
      { timeout: 10000 }
    );
    await page.waitForFunction(
      () => window.__mockSockets && window.__mockSockets.length
        && window.__mockSockets[0].readyState === 1,
      null,
      { timeout: 5000 }
    );

    const input = page.locator('#input');

    // ── Control: text-only "target:" (the behaviour images must match) ──
    await page.evaluate(() => { window.__chatSends = []; });
    await input.click();
    await input.fill('target: ' + BODY + ' control');
    await input.press('Enter');
    await page.waitForTimeout(150);
    const controlSends = textSendsOf(await page.evaluate(() => (window.__chatSends || []).slice()));
    const controlWire = controlSends[0] || '';
    if (controlWire.indexOf('[target-aside:') !== 0) {
      failures.push('control (no image): expected target-aside wire, got ' + JSON.stringify(controlWire));
    }

    // ── Subject: same command with an image attached ──
    await page.evaluate(() => { window.__chatSends = []; });
    await pasteImage(page);
    await input.click();
    await input.fill('target: ' + BODY);
    await input.press('Enter');
    await page.waitForTimeout(250);

    const after = await page.evaluate((body) => {
      const sends = (window.__chatSends || []).slice();
      const texts = sends.filter((s) => typeof s === 'string' && !s.startsWith('{'));
      const mainUserBubbles = Array.from(document.querySelectorAll('#messages .msg.user'))
        .map((el) => el.textContent || '')
        .filter((t) => t.indexOf(body) >= 0);
      return {
        texts: texts,
        mainUserBubbles: mainUserBubbles,
        chips: document.querySelectorAll('#composer-images .img-chip').length,
        composer: document.getElementById('input').value,
      };
    }, BODY);

    const wire = after.texts[0] || '';
    if (wire.indexOf('[target-aside:') !== 0) {
      failures.push('image + target: did not route to a filing aside — wire ' + JSON.stringify(wire));
    }
    if (wire.indexOf(BODY) < 0) {
      failures.push('filing wire lost the owner body: ' + JSON.stringify(wire));
    }
    if (wire.indexOf(MARKER) < 0) {
      failures.push('filing wire lost the attached image marker: ' + JSON.stringify(wire));
    }
    if (after.mainUserBubbles.length) {
      failures.push('body painted as a main-chat turn instead of a filing aside: '
        + JSON.stringify(after.mainUserBubbles));
    }
    if (after.chips !== 0) {
      failures.push('image chips left attached after send: ' + after.chips);
    }
    if (after.composer.indexOf(BODY) >= 0) {
      failures.push('composer not cleared after routed send: ' + JSON.stringify(after.composer));
    }

    // The aside register carries the same body + marker (RHS filing path).
    await page.waitForTimeout(150);
    const filing = asidePosts.find((p) => p && typeof p.text === 'string' && p.text.indexOf(BODY) >= 0)
      || asidePosts.find((p) => p && p.kind === 'target');
    if (!filing) {
      failures.push('no /api/asides register for the image-prefixed filing: '
        + JSON.stringify(asidePosts));
    } else {
      if (filing.kind !== 'target') {
        failures.push('aside registered with kind ' + JSON.stringify(filing.kind) + ' want target');
      }
      if (!/^T368 image prefixed/.test(String(filing.title || ''))) {
        failures.push('aside title taken from the image marker, not the owner words: '
          + JSON.stringify(filing.title));
      }
    }
  } catch (e) {
    failures.push(String((e && e.stack) || e));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL t368-image-prefix-route-test:');
    for (const f of failures) console.error('  -', f);
    process.exit(1);
  }
  console.log('PASS t368-image-prefix-route-test (image + target: files an aside, not main)');
})();
