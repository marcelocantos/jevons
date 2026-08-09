// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Browser oracle for the client half of the 🎯T355 UX-degrade path (🎯T361).
//
// The server already observes owner interaction and actuates. What was
// missing is the other end of every one of those wires, and each half fails
// silently in a way unit tests on the server cannot see:
//
//   (a) ux:"yield_hydrate" — the server's UX-coordinate step. Before this,
//       the client read the frame as chrome text and the degraded tab stayed
//       exactly as stuck; the whole point of the step is that the page
//       releases the main thread and repaints from shells (🎯T349).
//   (b) the "owner interaction degraded: …" alert — one more error bubble in
//       a transcript the owner may have scrolled away from, because
//       web/index.html only banners ChatReconnect.isOverseerDownError.
//   (c) composer-blocked — converge.OwnerObservation.ComposerBlocked had no
//       reporter, so it was always false and a live tab whose composer
//       refuses to submit was invisible to the server.
//
// Hermetic: static web/ + a mocked WebSocket that records what the page
// sends. Each leg drives the REAL frames the server emits (copied from
// internal/server/owner_health.go) through the product's own handle().
//
//   node scripts/chat-ui-test/t361-owner-ux-test.js [--headed]

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
      const json = (body) => {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(body));
      };
      if (u.pathname === '/api/history') return json({ lines: [], start: 0 });
      if (u.pathname === '/api/agents') return json([]);
      if (u.pathname === '/api/workers') return json([]);
      if (u.pathname === '/api/portfolios') return json({ portfolios: [] });
      if (u.pathname === '/api/cost') return json({ accounting: 'off', billable: false, alerts: [] });
      if (u.pathname === '/health') return json({ version: 't361-owner-ux', ok: true });
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

// Mock chat socket: records every client→server frame so leg (c) can assert
// that a blocked composer actually reaches the wire, not just the DOM.
function installMockWebSocket() {
  class MockWebSocket {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;
    constructor(url) {
      this.url = url;
      this.readyState = MockWebSocket.CONNECTING;
      this.onopen = null; this.onclose = null; this.onerror = null; this.onmessage = null;
      window.__sent = window.__sent || [];
      queueMicrotask(() => {
        this.readyState = MockWebSocket.OPEN;
        if (this.onopen) this.onopen({});
        if (String(url).indexOf('/ws/chat') !== -1) {
          window.__chatSock = this;
          // Server hello, then the history_meta the product gates on.
          this._emit({ type: 'conn', conn_id: 't361' });
          this._emit({ type: 'history_meta', older: 0, start: 0, total: 0 });
        }
      });
    }
    send(data) {
      if (typeof data !== 'string') return;
      if (String(this.url).indexOf('/ws/chat') !== -1) window.__sent.push(data);
      if (data === '{"type":"ping"}') this._emit({ type: 'pong' });
    }
    close() {
      this.readyState = MockWebSocket.CLOSED;
      if (this.onclose) this.onclose({});
    }
    _emit(obj) {
      const payload = typeof obj === 'string' ? obj : JSON.stringify(obj);
      if (this.onmessage) this.onmessage({ data: payload });
    }
  }
  window.WebSocket = MockWebSocket;
}

// The exact frames internal/server/owner_health.go puts on the wire.
const UX_COORDINATE_FRAME = { type: 'status', state: 'idle', text: 'chat settled — no turn in flight', ux: 'yield_hydrate' };
const ALERT_FRAME = {
  type: 'error',
  error: 'owner interaction degraded: interaction_usable (ux_degraded) — unresolved for 1m30s after 5 recovery steps',
};
const RECOVERED_FRAME = { type: 'status', ux: 'recovered', text: 'owner interaction recovered: interaction_usable (interaction_usable)' };

(async () => {
  const failures = [];
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });

  try {
    await page.addInitScript(installMockWebSocket);
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.handle === 'function' && typeof window.yieldAndHydrate === 'function' &&
        typeof window.reportComposerState === 'function' && typeof window.addMsg === 'function',
      null, { timeout: 10000 });

    // ── Leg A: ux:"yield_hydrate" makes the page yield AND re-hydrate ──
    // Seed a transcript so there is real paint work to yield, and queue it.
    const legA = await page.evaluate(async (frame) => {
      for (let i = 0; i < 40; i++) {
        window.addMsg(i % 2 === 0 ? 'user' : 'jevons',
          'seed bubble ' + i + ' with a few lines of text.\n'.repeat(2 + (i % 3)));
      }
      // Arm the T349 paint queues, then hand the page the server's hint
      // before they can drain: the yield must cancel real pending work.
      window.scheduleVirtualize();
      const before = Object.assign({}, window.__uxYieldHydrate);
      window.handle(frame);
      const afterYield = Object.assign({}, window.__uxYieldHydrate);
      // Hydrate is deliberately a LATER task — it must not have run yet.
      const hydratedSynchronously = afterYield.hydrates > before.hydrates;
      await new Promise((r) => setTimeout(r, 60));
      for (let f = 0; f < 4; f++) await new Promise((r) => requestAnimationFrame(r));
      return {
        before: before,
        afterYield: afterYield,
        hydratedSynchronously: hydratedSynchronously,
        after: Object.assign({}, window.__uxYieldHydrate),
        painted: document.querySelectorAll('#messages .msg').length,
      };
    }, UX_COORDINATE_FRAME);
    console.log('  legA:', JSON.stringify(legA));
    if (legA.afterYield.yields !== legA.before.yields + 1) {
      failures.push('ux:"yield_hydrate" did not make the page yield (yields ' +
        legA.before.yields + ' → ' + legA.afterYield.yields + ') — the hint was ignored');
    }
    if (legA.hydratedSynchronously) {
      failures.push('hydrate ran inside the frame handler — the yield gives the main thread back to nobody');
    }
    if (legA.after.hydrates !== legA.before.hydrates + 1) {
      failures.push('page never re-hydrated after the yield (hydrates ' +
        legA.before.hydrates + ' → ' + legA.after.hydrates + ')');
    }
    if (legA.after.lastReason !== 'ux_coordinate') {
      failures.push('yield reason = ' + legA.after.lastReason + ', want ux_coordinate');
    }
    if (legA.painted < 40) {
      failures.push('transcript did not survive the yield/hydrate (' + legA.painted + ' msgs)');
    }

    // A plain level frame must NOT trigger a yield — otherwise every idle
    // sample would tear down the paint queues.
    const plain = await page.evaluate(async () => {
      const before = Object.assign({}, window.__uxYieldHydrate);
      window.handle({ type: 'status', state: 'idle', text: 'chat settled — no turn in flight' });
      await new Promise((r) => setTimeout(r, 30));
      return { before: before, after: Object.assign({}, window.__uxYieldHydrate) };
    });
    if (plain.after.yields !== plain.before.yields) {
      failures.push('a plain level frame triggered a yield — the hint is not what gates it');
    }

    // ── Leg B: the alert raises a STICKY banner, and only recovery clears ──
    const legB = await page.evaluate(async (arg) => {
      const banner = document.getElementById('degraded-banner');
      const read = () => ({
        visible: banner.classList.contains('visible'),
        text: banner.textContent,
      });
      const before = read();
      window.handle(arg.alert);
      const raised = read();
      // Survives unrelated traffic: a reply, a level sample, a fleet frame.
      window.addMsg('jevons', 'an ordinary reply lands while degraded.');
      window.handle({ type: 'status', state: 'thinking', text: 'turn in flight' });
      window.handle({ type: 'status', state: 'idle', text: 'chat settled — no turn in flight' });
      await new Promise((r) => setTimeout(r, 40));
      const sticky = read();
      window.handle(arg.recovered);
      await new Promise((r) => setTimeout(r, 20));
      return { before: before, raised: raised, sticky: sticky, cleared: read() };
    }, { alert: ALERT_FRAME, recovered: RECOVERED_FRAME });
    console.log('  legB:', JSON.stringify(legB));
    if (legB.before.visible) failures.push('banner was already up before the alert');
    if (!legB.raised.visible) {
      failures.push('owner-interaction alert did not raise the degraded banner — ' +
        'it landed as a plain chat error bubble only (🎯T361 (b))');
    }
    if (legB.raised.text.toLowerCase().indexOf('owner interaction degraded') < 0) {
      failures.push('banner text = ' + JSON.stringify(legB.raised.text) + ', want the owner-interaction class');
    }
    if (!legB.sticky.visible) {
      failures.push('banner cleared on unrelated traffic — the class banner must be sticky');
    }
    if (legB.cleared.visible) {
      failures.push('recovery frame did not clear the banner — a stale banner outlives the gap');
    }

    // A journaled alert replayed on reconnect must not re-raise the banner.
    const replay = await page.evaluate(async (alert) => {
      const banner = document.getElementById('degraded-banner');
      window.beginHistoryReplay();
      window.handle(alert);
      const during = banner.classList.contains('visible');
      window.handle({ type: 'history_meta', older: 0, start: 0, total: 0 });
      await new Promise((r) => setTimeout(r, 20));
      return { during: during, after: banner.classList.contains('visible') };
    }, ALERT_FRAME);
    console.log('  replay:', JSON.stringify(replay));
    if (replay.during || replay.after) {
      failures.push('a replayed (journaled) alert re-raised the banner for a gap that may be long closed');
    }

    // ── Leg C: a blocked composer reaches the server ──────────────────
    const legC = await page.evaluate(async (alert) => {
      const isUX = (s) => { try { return JSON.parse(s).type === 'ux_state'; } catch (_) { return false; } };
      const uxFrames = () => window.__sent.filter(isUX).map((s) => JSON.parse(s));
      const onConnect = uxFrames();
      // Block the composer the way the product does (overseer-down degrade).
      window.setDegraded(true, 'overseer is not running');
      await new Promise((r) => setTimeout(r, 20));
      const whileBlocked = uxFrames();
      // Edge-triggered: a re-sample of the same level sends nothing more.
      window.reportComposerState();
      window.reportComposerState();
      const afterResample = uxFrames();
      window.setDegraded(false);
      await new Promise((r) => setTimeout(r, 20));
      const afterUnblock = uxFrames();
      window.handle(alert); // banner state must not disturb the report path
      await new Promise((r) => setTimeout(r, 20));
      // 🎯T362 guard: these frames are protocol, not conversation. Reporting
      // the composer must never paint a bubble in the owner's transcript.
      const leaked = Array.prototype.slice
        .call(document.querySelectorAll('#messages .msg'))
        .filter((el) => (el.textContent || '').indexOf('ux_state') !== -1)
        .map((el) => el.className + ': ' + (el.textContent || '').slice(0, 120));
      return {
        onConnect: onConnect,
        whileBlocked: whileBlocked,
        afterResample: afterResample,
        afterUnblock: afterUnblock,
        leaked: leaked,
      };
    }, ALERT_FRAME);
    console.log('  legC:', JSON.stringify(legC));
    if (!legC.onConnect.length || legC.onConnect[0].composer_blocked !== false) {
      failures.push('client never reported its composer level on connect — ' +
        'ComposerBlocked stays server-side-false (🎯T361 (c))');
    }
    const blockedFrame = legC.whileBlocked.filter((f) => f.composer_blocked === true);
    if (!blockedFrame.length) {
      failures.push('a blocked composer never reached the server');
    } else if (blockedFrame[0].reason !== 'overseer_down') {
      failures.push('blocked report reason = ' + blockedFrame[0].reason + ', want overseer_down');
    }
    if (legC.afterResample.length !== legC.whileBlocked.length) {
      failures.push('composer level re-reported without an edge — ' +
        (legC.afterResample.length - legC.whileBlocked.length) + ' redundant frames');
    }
    const unblocked = legC.afterUnblock.slice(legC.afterResample.length);
    if (!unblocked.length || unblocked[unblocked.length - 1].composer_blocked !== false) {
      failures.push('recovery of the composer was never reported — the gap could never be satisfied');
    }
    if (legC.leaked.length) {
      failures.push('composer reporting painted its own protocol frames into the transcript (🎯T362): ' +
        legC.leaked.join(' | '));
    }
  } catch (e) {
    failures.push('exception: ' + (e && e.message ? e.message : String(e)));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL t361-owner-ux-test');
    failures.forEach((f) => console.error('  - ' + f));
    process.exit(1);
  }
  console.log('PASS t361-owner-ux-test');
})();
