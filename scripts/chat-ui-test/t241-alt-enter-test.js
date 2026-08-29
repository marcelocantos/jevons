// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic UI oracle for 🎯T241: Alt+Enter force-send.
// Both product branches (mandatory — pop_last-only hermetics are reject):
//   1. Real draft while busy → force-send draft with interrupt (not enqueue)
//   2. Seed-only EMPTY_SEED + non-empty queue → sendQueueNow (queue shrinks;
//      no pop_last into composer)
//
// Drives real product path (sendQueueState is script-scoped let, not window).
//
//   node scripts/chat-ui-test/t241-alt-enter-test.js [--headed]

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
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      let rel = u.pathname === '/' ? '/index.html' : u.pathname;
      const file = path.normalize(path.join(webRoot, rel));
      if (!file.startsWith(webRoot)) {
        res.writeHead(403); res.end(); return;
      }
      fs.readFile(file, (err, data) => {
        if (err) { res.writeHead(404); res.end('not found'); return; }
        res.writeHead(200, { 'Content-Type': contentType(file) });
        res.end(data);
      });
    });
    srv.listen(0, '127.0.0.1', () => {
      resolve({ srv, base: `http://127.0.0.1:${srv.address().port}` });
    });
    srv.on('error', reject);
  });
}

// Mock WebSocket: open immediately; record sends; keep turns busy (no end_turn).
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
        // Same product handshake as scripts/chat-ui-test/test.js (🎯T322):
        // status-text === "connected" requires history_meta after open.
        // Chat socket only — /ws/reload onmessage reloads the page.
        if (String(url).indexOf('/ws/chat') !== -1) {
          this._emit({
            type: 'history_meta',
            older: 0,
            start: 0,
            total: 0,
            replay_frames: 0,
            replay_bytes: 0,
            replay_ms: 0,
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
      window.__lastChatSend = data;
      if (typeof data !== 'string') return;
      if (data.startsWith('{')) {
        let o = null;
        try { o = JSON.parse(data); } catch (_) { return; }
        if (!o || typeof o !== 'object') return;
        if (o.type === 'ping') { this._emit({ type: 'pong' }); return; }
        // interrupt / rewind / ux_state stay control (recorded above).
        if (o.type === 'rewind' || o.type === 'interrupt' || o.type === 'ux_state') return;
        const text = (typeof o.content === 'string' && o.content)
          || (typeof o.text === 'string' && o.text)
          || '';
        if (text) this._ownerTurn(text);
        return;
      }
      this._ownerTurn(data);
    }
    // Partial assistant only — leave workingEl up (busy) for Alt+Enter branches.
    _ownerTurn(text) {
      this._emit({
        type: 'user',
        message: { role: 'user', content: text },
      });
      this._emit({
        type: 'assistant',
        message: {
          role: 'assistant',
          content: [{ type: 'text', text: '…' }],
        },
      });
    }
    close() {
      this.readyState = MockWebSocket.CLOSED;
      if (this.onclose) this.onclose({});
    }
    _emit(obj) {
      if (this.onmessage) {
        this.onmessage({ data: JSON.stringify(obj) });
      }
    }
  }
  window.WebSocket = MockWebSocket;

  // Since 50328854 owner text is POST /api/agents/<n>/send; interrupt still
  // rides the socket. Mirror the shipped route so __chatSends sees the text
  // (🎯T557 / same cause as test.js).
  const realFetch = window.fetch.bind(window);
  window.fetch = function (input, init) {
    const url = typeof input === 'string' ? input : (input && input.url) || '';
    const method = String((init && init.method) || (input && input.method) || 'GET').toUpperCase();
    if (method === 'POST' && /\/api\/agents\/[^/]+\/send$/.test(url)) {
      let text = '';
      try { text = JSON.parse(init && init.body || '{}').text || ''; } catch (_) { /* fall through */ }
      window.__chatSends = window.__chatSends || [];
      if (text) window.__chatSends.push(text);
      window.__lastChatSend = text;
      const socks = window.__mockSockets || [];
      const chat = socks.filter((s) => String(s.url).indexOf('/ws/chat') !== -1).pop();
      if (chat && text) setTimeout(() => chat._ownerTurn(text), 0);
      return Promise.resolve(new Response('{"ok":true}', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }));
    }
    return realFetch(input, init);
  };
}

async function queueRowCount(page) {
  return page.evaluate(() => {
    const el = document.getElementById('send-queue');
    if (!el) return 0;
    return el.querySelectorAll('.send-queue-item, [data-queue-id], .queue-row').length
      || el.querySelectorAll('button').length / 2 // send-now + cancel per row fallback
      || el.children.length;
  });
}

async function textSends(page) {
  return page.evaluate(() => {
    const sends = window.__chatSends || [];
    return sends.filter(function (s) {
      return typeof s === 'string' && !s.startsWith('{');
    });
  });
}

async function allSends(page) {
  return page.evaluate(() => (window.__chatSends || []).slice());
}

(async () => {
  const failures = [];
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1100, height: 800 } });
  try {
    await page.addInitScript(installMockWebSocket);
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.ComposerKeys !== 'undefined'
        && typeof window.SendQueue !== 'undefined'
        && typeof window.WisprContext !== 'undefined'
        && document.getElementById('input'),
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

    // ── Pure policy (classifyEnterAction) ────────────────────────────
    const pure = await page.evaluate(() => {
      const WC = window.WisprContext;
      const CK = window.ComposerKeys;
      const seed = WC.EMPTY_SEED;
      return {
        seedEmpty: WC.isEffectivelyEmpty(seed),
        seedQueue: CK.classifyEnterAction('Enter', { altKey: true }, {
          composerEmpty: WC.isEffectivelyEmpty(seed),
          queueLen: 1,
        }),
        draft: CK.classifyEnterAction('Enter', { altKey: true }, {
          composerEmpty: WC.isEffectivelyEmpty('hello'),
          queueLen: 1,
        }),
        neverPop: CK.classifyEnterAction('Enter', { altKey: true }, {
          composerEmpty: true,
          queueLen: 0,
        }),
      };
    });
    if (!pure.seedEmpty) failures.push('EMPTY_SEED not effectively empty');
    if (pure.seedQueue !== 'send_queue_now') {
      failures.push('pure seed+queue: ' + pure.seedQueue);
    }
    if (pure.draft !== 'force_send') failures.push('pure draft: ' + pure.draft);
    if (pure.neverPop === 'pop_last') failures.push('pure empty still pop_last');
    if (pure.neverPop !== 'noop') failures.push('pure empty no queue want noop got ' + pure.neverPop);

    // ── Branch A: enqueue-while-busy → seed-only → Alt+Enter → send-now ──
    await page.evaluate(() => { window.__chatSends = []; });
    await input.click();
    await input.fill('busy-starter');
    await input.press('Enter');
    await page.waitForSelector('.working-indicator', { timeout: 3000 });

    await input.fill('queued-follow-up-A');
    await input.press('Enter'); // plain Enter while busy → enqueue
    await page.waitForFunction(() => {
      const el = document.getElementById('send-queue');
      return el && el.children.length > 0;
    }, null, { timeout: 3000 });

    const qBeforeA = await page.evaluate(() => {
      const el = document.getElementById('send-queue');
      return el ? el.children.length : 0;
    });
    if (qBeforeA < 1) failures.push('branch A: queue strip empty after busy enqueue');

    // Seed-only (isEffectivelyEmpty) — product EMPTY_SEED.
    await page.evaluate(() => {
      const input = document.getElementById('input');
      const WC = window.WisprContext;
      input.value = WC.EMPTY_SEED;
      if (typeof window.syncComposerSeedClass === 'function') window.syncComposerSeedClass();
    });
    const seedCheck = await page.evaluate(() => {
      const WC = window.WisprContext;
      const input = document.getElementById('input');
      return {
        empty: WC.isEffectivelyEmpty(input.value),
        value: input.value,
      };
    });
    if (!seedCheck.empty) {
      failures.push('branch A: seed not effectively empty: ' + JSON.stringify(seedCheck));
    }

    await page.evaluate(() => { window.__chatSends = []; });
    await input.press('Alt+Enter');

    // Wait a tick for sendQueueNow wire.
    await page.waitForTimeout(50);

    const afterA = await page.evaluate(() => {
      const WC = window.WisprContext;
      const input = document.getElementById('input');
      const qEl = document.getElementById('send-queue');
      const sends = window.__chatSends || [];
      const textSends = sends.filter(function (s) {
        return typeof s === 'string' && !s.startsWith('{');
      });
      const interrupts = sends.filter(function (s) {
        return typeof s === 'string' && s.indexOf('interrupt') >= 0;
      });
      return {
        qAfter: qEl ? qEl.children.length : 0,
        textSends: textSends,
        interrupts: interrupts.length,
        inputAfter: input.value,
        inputEmpty: WC.isEffectivelyEmpty(input.value),
        hasQueuedText: textSends.some(function (s) {
          return String(s).indexOf('queued-follow-up-A') >= 0;
        }),
      };
    });

    if (afterA.qAfter !== qBeforeA - 1) {
      failures.push('branch A: queue did not shrink qBefore=' + qBeforeA + ' after=' + JSON.stringify(afterA));
    }
    if (!afterA.hasQueuedText) {
      failures.push('branch A: did not send queued text: ' + JSON.stringify(afterA));
    }
    // Must not load last owner into composer (pop_last).
    if (!afterA.inputEmpty && String(afterA.inputAfter).indexOf('busy-starter') >= 0) {
      failures.push('branch A: pop_last side effect — last owner in composer');
    }

    // ── Branch B: real draft while busy → Alt+Enter force-send draft ──
    // Ensure still busy (or re-busy).
    const busyB = await page.evaluate(() => !!document.querySelector('.working-indicator'));
    if (!busyB) {
      await input.fill('rebusy');
      await input.press('Enter');
      await page.waitForSelector('.working-indicator', { timeout: 3000 });
    }

    // Enqueue a decoy so draft must win over send_queue_now.
    await input.fill('should-stay-queued');
    await input.press('Enter');
    await page.waitForFunction(() => {
      const el = document.getElementById('send-queue');
      return el && el.children.length > 0
        && /should-stay-queued/.test(el.textContent || '');
    }, null, { timeout: 3000 }).catch(() => {});

    const qBeforeB = await page.evaluate(() => {
      const el = document.getElementById('send-queue');
      return el ? el.children.length : 0;
    });

    await input.fill('force-send-draft-B');
    const draftEmpty = await page.evaluate(() => {
      return window.WisprContext.isEffectivelyEmpty(document.getElementById('input').value);
    });
    if (draftEmpty) failures.push('branch B: draft classified empty');

    await page.evaluate(() => { window.__chatSends = []; });
    await input.press('Alt+Enter');
    await page.waitForTimeout(50);

    const afterB = await page.evaluate(() => {
      const qEl = document.getElementById('send-queue');
      const sends = window.__chatSends || [];
      const textSends = sends.filter(function (s) {
        return typeof s === 'string' && !s.startsWith('{');
      });
      const hasInterrupt = sends.some(function (s) {
        return typeof s === 'string' && s.indexOf('interrupt') >= 0;
      });
      return {
        textSends: textSends,
        hasInterrupt: hasInterrupt,
        qAfter: qEl ? qEl.children.length : 0,
        draftSent: textSends.some(function (s) {
          return String(s).indexOf('force-send-draft-B') >= 0;
        }),
        queueStillHasDecoy: qEl && /should-stay-queued/.test(qEl.textContent || ''),
      };
    });

    if (!afterB.draftSent) {
      failures.push('branch B: draft not force-sent: ' + JSON.stringify(afterB));
    }
    // While busy, force_send should interrupt.
    if (!afterB.hasInterrupt) {
      failures.push('branch B: missing interrupt on busy force-send: ' + JSON.stringify(afterB));
    }
    // Decoy queue item must not be the send-now victim of draft Alt+Enter.
    if (qBeforeB >= 1 && afterB.qAfter === qBeforeB - 1 && !afterB.queueStillHasDecoy
        && afterB.textSends.some(function (s) { return String(s).indexOf('should-stay-queued') >= 0; })) {
      failures.push('branch B: sent queue decoy instead of draft');
    }

    // Placeholder must document force-send, not pop last.
    const ph = await page.evaluate(() => {
      const input = document.getElementById('input');
      return input ? (input.placeholder || '') : '';
    });
    if (/pops last/i.test(ph)) {
      failures.push('placeholder still advertises pop last: ' + ph);
    }
    if (/force-send|Ctrl\+Enter|interject/i.test(ph)) {
      failures.push('placeholder must not document chords: ' + ph);
    }
    if (!/^Message\.\.\.$/.test(ph.trim())) {
      failures.push('placeholder should be Message..., got: ' + JSON.stringify(ph));
    }
  } catch (e) {
    failures.push(String(e && e.stack || e));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL t241-alt-enter-test:');
    for (const f of failures) console.error('  -', f);
    process.exit(1);
  }
  console.log('ok - T241 Alt+Enter force-send: seed+queue send-now; draft force-send; never pop_last');
})();
