// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic 🎯T56: long transcript → off-screen bodies dematerialised.
//   node scripts/chat-ui-test/virtual-list-test.js

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  return 'application/octet-stream';
}

function startServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
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
  });
}

(async () => {
  const failures = [];
  const { srv, base } = await startServer();
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 900, height: 400 } });
  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.addMsg === 'function' && typeof window.virtualizeMessages === 'function', null, { timeout: 10000 });
    await page.evaluate(() => {
      window.__virtOnErrors = [];
      window.addEventListener('error', function (ev) {
        window.__virtOnErrors.push(String((ev && ev.message) || ev));
      });
    });

    const stats = await page.evaluate(() => {
      const N = 80;
      for (let i = 0; i < N; i++) {
        window.addMsg('user', 'message number ' + i + ' with enough text to have height ' + 'x'.repeat(40));
        window.addMsg('jevons', 'reply ' + i + '\n\n' + 'paragraph '.repeat(20));
      }
      // Pin top so lower half is off-screen.
      document.getElementById('messages').scrollTop = 0;
      // 🎯T349: demat is capped per frame (DEMATERIALIZE_PER_FRAME) so one
      // pass no longer clears everything — the product re-arms itself via
      // rAF; this loop is the deterministic stand-in. Convergence within
      // ~n/cap passes is part of the assertion.
      const cap = (window.VirtualList && window.VirtualList.DEMATERIALIZE_PER_FRAME) || 40;
      const passes = Math.ceil((2 * N) / cap) + 2;
      for (let p = 0; p < passes; p++) window.virtualizeMessages();
      const attached = [...document.querySelectorAll('#messages-canvas > .msg')];
      const rows = window.__transcriptRows || [];
      return {
        attached: attached.length,
        records: rows.length,
        heavy: attached.filter(m => !m.classList.contains('virt-shell')).length,
        hasVirtualList: typeof window.VirtualList !== 'undefined',
        hasLayout: !!(window.VirtualList && window.VirtualList.layoutAttachRange),
        onerrors: (window.__virtOnErrors || []).slice(),
      };
    });

    if (!stats.hasVirtualList) failures.push('VirtualList not loaded');
    if (!stats.hasLayout) failures.push('TranscriptLayout not loaded');
    if (stats.records < 100) failures.push('expected many records, got ' + stats.records);
    if (stats.attached >= stats.records) failures.push('attached should be a viewport band, got ' + stats.attached + ' of ' + stats.records);
    if (stats.attached > 80) failures.push('attached band too large: ' + stats.attached);
    if (stats.heavy > 40) failures.push('too many heavy nodes: ' + stats.heavy);
    if (stats.onerrors && stats.onerrors.length) {
      failures.push('window.onerror during virtualize: ' + JSON.stringify(stats.onerrors));
    }

    // Scroll to bottom: last row attaches and is material; prefix stays off-DOM.
    const after = await page.evaluate(() => {
      const el = document.getElementById('messages');
      el.scrollTop = el.scrollHeight;
      window.virtualizeMessages();
      const attached = [...document.querySelectorAll('#messages-canvas > .msg')];
      const last = attached.sort((a, b) => (a._vIndex | 0) - (b._vIndex | 0)).pop();
      const rows = window.__transcriptRows || [];
      return {
        lastShell: last && last.classList.contains('virt-shell'),
        attached: attached.length,
        records: rows.length,
      };
    });
    if (after.lastShell) failures.push('latest msg should be materialised at bottom');
    if (after.attached >= after.records) failures.push('bottom still must not attach the whole journal, got ' + after.attached);

    // Height change after collapse/expand must rematerialise in-view shells
    // without a user wheel (blank gap regression).
    const afterCollapse = await page.evaluate(async () => {
      const el = document.getElementById('messages');
      if (window.enterTrackBottom) window.enterTrackBottom();
      const tall = Array.from({ length: 60 }, (_, i) => 'Tall reply line ' + i + '.').join('\n\n');
      const prior = window.addMsg('jevons', tall);
      el.scrollTop = el.scrollHeight;
      window.virtualizeMessages();
      const all = [...document.querySelectorAll('#messages-canvas > .msg')];
      all.forEach((m, i) => {
        if (i < all.length - 3 && !m.classList.contains('virt-shell') && typeof window.dematerializeMsg === 'function') {
          if (m.offsetTop + m.offsetHeight < el.scrollTop - 20) window.dematerializeMsg(m);
        }
      });
      const shellsBefore = document.querySelectorAll('#messages-canvas > .msg.virt-shell').length;
      window.addMsg('user', 'new request that may push prior up');
      if (window.scrollDown) window.scrollDown();
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => requestAnimationFrame(r))));
      if (window.refreshLatestExpansion) window.refreshLatestExpansion();
      window.virtualizeMessages();
      const viewTop = el.scrollTop;
      const viewBot = viewTop + el.clientHeight;
      let blankInView = 0;
      let heavyInView = 0;
      document.querySelectorAll('#messages-canvas > .msg').forEach((m) => {
        const top = m.offsetTop;
        const bot = top + m.offsetHeight;
        if (bot < viewTop + 2 || top > viewBot - 2) return;
        if (m.classList.contains('virt-shell') && m._body && m._body.innerHTML === '') blankInView++;
        else if (!m.classList.contains('virt-shell')) heavyInView++;
      });
      return {
        ok: blankInView === 0 && heavyInView > 0,
        blankInView,
        heavyInView,
        shellsBefore,
        shellsAfter: document.querySelectorAll('#messages-canvas > .msg.virt-shell').length,
        priorClipped: prior.classList.contains('msg-clipped'),
      };
    });
    if (!afterCollapse.ok) {
      failures.push(
        'after height-change/T246 path, in-view virt-shells not rematerialised ' +
        JSON.stringify(afterCollapse),
      );
    }

    // Turn-markers are list rows. Page-up must re-home the real node,
    // not mint an empty .msg.turn-marker (the 72px whitespace + vanished
    // "⋯ n steps"). Bubble height includes the outside timestamp chrome.
    const marker = await page.evaluate(() => {
      if (typeof window.resetTranscript === 'function') window.resetTranscript();
      const probe = window.addMsg('jevons', '🎯T119.3 context chrome must survive page-up\n' + 'x'.repeat(40));
      if (typeof window.layoutMsg === 'function') window.layoutMsg(probe);
      window._t1193Probe = probe;
      if (typeof window.startTurn === 'function') window.startTurn();
      if (typeof window.addTurnItem === 'function') {
        window.addTurnItem('tool-use', 'Read');
        window.addTurnItem('tool-result', 'ok');
      }
      window.addMsg('user', 'after marker\n' + 'y'.repeat(40));
      const el = document.getElementById('messages');
      el.scrollTop = 0;
      for (let p = 0; p < 8; p++) window.virtualizeMessages();
      el.scrollTop = el.scrollHeight;
      for (let p = 0; p < 8; p++) window.virtualizeMessages();
      const rows = window.__transcriptRows || [];
      const markers = rows.filter((r) => r && r.role === 'turn-marker');
      const fake = document.querySelectorAll('#messages-canvas > .msg.turn-marker').length;
      const real = [];
      for (let i = 0; i < rows.length; i++) {
        const r = rows[i];
        if (!r || r.role !== 'turn-marker') continue;
        real.push({
          hasEl: !!(r.el),
          connected: !!(r.el && r.el.isConnected),
          cls: r.el ? r.el.className : '',
          label: r.el && r.el._label ? r.el._label.textContent : (r.text || ''),
          storedH: window.__transcriptLayout.heights[i],
        });
      }
      const user = rows.find((r) => r && r.role === 'user' && r.el && r.el.isConnected);
      const ui = user ? user.el._vIndex : -1;
      const userH = ui >= 0 ? window.__transcriptLayout.heights[ui] : 0;
      const userBox = user && user.el ? user.el.getBoundingClientRect().height : 0;
      const parked = window._t1193Probe;
      const probeRow = rows.find((r) => r && r.el === parked);
      return {
        markerRows: markers.length,
        fakeMsgMarkers: fake,
        real: real,
        userH: userH,
        userBox: userBox,
        chrome: window.VirtualList && window.VirtualList.BUBBLE_BOTTOM_CHROME_PX,
        parkedSameNode: !!(parked && probeRow && probeRow.el === parked),
        probeConnected: !!(parked && parked.isConnected),
        probeIsMsgRebuild: !!(parked && probeRow && probeRow.el && probeRow.el !== parked),
      };
    });
    if (marker.markerRows < 1) failures.push('expected a turn-marker row, got ' + marker.markerRows);
    if (marker.fakeMsgMarkers > 0) {
      failures.push('page-up rebuilt turn-markers as .msg shells: ' + marker.fakeMsgMarkers);
    }
    if (!marker.real.some((r) => r.connected && /turn-marker/.test(r.cls) && !/\bmsg\b/.test(r.cls)
        && /step/.test(r.label))) {
      failures.push('real ⋯ n steps marker missing after page-up ' + JSON.stringify(marker.real));
    }
    if (marker.chrome && marker.userBox > 0 && marker.userH < marker.userBox + marker.chrome - 1) {
      failures.push('user row height missing timestamp chrome ' + JSON.stringify({
        userH: marker.userH, userBox: marker.userBox, chrome: marker.chrome,
      }));
    }
    if (!marker.parkedSameNode || marker.probeIsMsgRebuild) {
      failures.push('page-up rebuilt the jevons bubble instead of parking it ' + JSON.stringify({
        parkedSameNode: marker.parkedSameNode, probeIsMsgRebuild: marker.probeIsMsgRebuild,
      }));
    }

    // 🎯T246: stay material while partially on-screen; collapse/dematerialize only when fully above fold.
    // Controlled free-scroll geometry (short viewport would pin-collapse tall on new msg).
    const t246Virt = await page.evaluate(async () => {
      const el = document.getElementById('messages');
      // Clear for a short controlled transcript (keep the canvas host).
      if (typeof window.resetTranscript === 'function') window.resetTranscript();
      else el.innerHTML = '';
      if (window.enterTrackBottom) window.enterTrackBottom();
      const body = Array.from({ length: 25 }, (_, i) => 'T246 line ' + i + ' with padding text xx').join('\n\n');
      const msg = window.addMsg('jevons', body);
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      if (window.refreshLatestExpansion) window.refreshLatestExpansion();
      // Short trailing without enough bulk to clear prior when free-scrolled mid-list.
      if (window.leaveTrackBottom) window.leaveTrackBottom();
      window.addMsg('user', 'short');
      // Filler below so we can later scroll prior fully out.
      for (let i = 0; i < 10; i++) {
        window.addMsg('jevons', 'filler ' + i + '\n' + 'x'.repeat(80));
      }
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      // Keep prior auto-expanded for the on-screen clause.
      msg._autoExpanded = true;
      msg._expanded = true;
      msg._userToggled = false;
      if (msg._fullText != null) window.renderBody(msg, msg._fullRole, msg._fullText);
      if (window.leaveTrackBottom) window.leaveTrackBottom();
      // Partial view: prior top slightly above fold, bottom still visible.
      el.scrollTop = Math.max(0, msg.offsetTop + Math.floor(msg.offsetHeight * 0.3));
      window.virtualizeMessages();
      if (window.collapseAutoExpandedOffScreen) window.collapseAutoExpandedOffScreen();
      const viewTop = el.scrollTop;
      const viewBot = viewTop + el.clientHeight;
      const top = msg.offsetTop;
      const bot = top + msg.offsetHeight;
      const onScreen = {
        anyVisible: bot > viewTop && top < viewBot,
        fullyAbove: bot <= viewTop,
        expanded: msg._expanded === true,
        auto: msg._autoExpanded === true,
        shell: msg.classList.contains('virt-shell'),
        clipped: msg.classList.contains('msg-clipped'),
      };
      // Fully above fold.
      el.scrollTop = msg.offsetTop + msg.offsetHeight + 80;
      if (msg.offsetTop + msg.offsetHeight > el.scrollTop) el.scrollTop = el.scrollHeight;
      window.virtualizeMessages();
      if (window.collapseAutoExpandedOffScreen) window.collapseAutoExpandedOffScreen();
      window.virtualizeMessages();
      const off = {
        fullyAbove: msg.offsetTop + msg.offsetHeight <= el.scrollTop,
        expanded: msg._expanded === true,
        auto: msg._autoExpanded === true,
        shell: msg.classList.contains('virt-shell'),
        clipped: msg.classList.contains('msg-clipped'),
      };
      return { onScreen, off };
    });
    if (!t246Virt.onScreen.anyVisible || t246Virt.onScreen.fullyAbove) {
      failures.push('T246 virt on-screen setup failed ' + JSON.stringify(t246Virt.onScreen));
    } else if (!t246Virt.onScreen.expanded || t246Virt.onScreen.shell || t246Virt.onScreen.clipped) {
      failures.push('T246: on-screen message not material+expanded ' + JSON.stringify(t246Virt.onScreen));
    }
    if (!t246Virt.off.fullyAbove) {
      failures.push('T246 virt: failed to scroll message fully above fold ' + JSON.stringify(t246Virt.off));
    } else if (t246Virt.off.expanded && t246Virt.off.auto) {
      failures.push('T246: fully above fold still auto-expanded ' + JSON.stringify(t246Virt.off));
    }

    const leftoverErrors = await page.evaluate(() => (window.__virtOnErrors || []).slice());
    if (leftoverErrors.length) {
      failures.push('window.onerror leftover: ' + JSON.stringify(leftoverErrors));
    }

    console.log(JSON.stringify({ stats, after, afterCollapse, t246Virt, leftoverErrors, failures }, null, 2));
    if (failures.length) {
      console.error('FAIL', failures);
      process.exitCode = 1;
    } else {
      console.log('PASS virtual-list-test');
    }
  } finally {
    await browser.close();
    srv.close();
  }
})().catch(e => { console.error(e); process.exit(1); });
