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
      const msgs = [...document.querySelectorAll('#messages > .msg')];
      const shells = msgs.filter(m => m.classList.contains('virt-shell'));
      const heavy = msgs.filter(m => !m.classList.contains('virt-shell'));
      // Bodies of shells should be empty.
      const emptyBodies = shells.filter(m => m._body && m._body.innerHTML === '').length;
      return {
        total: msgs.length,
        shells: shells.length,
        heavy: heavy.length,
        emptyBodies,
        hasVirtualList: typeof window.VirtualList !== 'undefined',
      };
    });

    if (!stats.hasVirtualList) failures.push('VirtualList not loaded');
    if (stats.total < 100) failures.push('expected many msgs, got ' + stats.total);
    if (stats.shells < 20) failures.push('expected many virt-shells, got ' + stats.shells);
    if (stats.heavy > 40) failures.push('too many heavy nodes: ' + stats.heavy);
    if (stats.emptyBodies !== stats.shells) failures.push('shells without empty bodies');

    // Scroll to bottom: shells near top should remain dematerialised; bottom rematerialise.
    const after = await page.evaluate(() => {
      const el = document.getElementById('messages');
      el.scrollTop = el.scrollHeight;
      window.virtualizeMessages();
      const msgs = [...document.querySelectorAll('#messages > .msg')];
      const last = msgs[msgs.length - 1];
      return {
        lastShell: last && last.classList.contains('virt-shell'),
        shells: msgs.filter(m => m.classList.contains('virt-shell')).length,
      };
    });
    if (after.lastShell) failures.push('latest msg should be materialised at bottom');
    if (after.shells < 10) failures.push('still expect off-screen shells at bottom, got ' + after.shells);

    // Height change after collapse/expand must rematerialise in-view shells
    // without a user wheel (blank gap regression).
    const afterCollapse = await page.evaluate(async () => {
      const el = document.getElementById('messages');
      if (window.enterTrackBottom) window.enterTrackBottom();
      const tall = Array.from({ length: 60 }, (_, i) => 'Tall reply line ' + i + '.').join('\n\n');
      const prior = window.addMsg('jevons', tall);
      el.scrollTop = el.scrollHeight;
      window.virtualizeMessages();
      const all = [...document.querySelectorAll('#messages > .msg')];
      all.forEach((m, i) => {
        if (i < all.length - 3 && !m.classList.contains('virt-shell') && typeof window.dematerializeMsg === 'function') {
          if (m.offsetTop + m.offsetHeight < el.scrollTop - 20) window.dematerializeMsg(m);
        }
      });
      const shellsBefore = document.querySelectorAll('#messages > .msg.virt-shell').length;
      window.addMsg('user', 'new request that may push prior up');
      if (window.scrollDown) window.scrollDown();
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => requestAnimationFrame(r))));
      if (window.refreshLatestExpansion) window.refreshLatestExpansion();
      window.virtualizeMessages();
      const viewTop = el.scrollTop;
      const viewBot = viewTop + el.clientHeight;
      let blankInView = 0;
      let heavyInView = 0;
      document.querySelectorAll('#messages > .msg').forEach((m) => {
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
        shellsAfter: document.querySelectorAll('#messages > .msg.virt-shell').length,
        priorClipped: prior.classList.contains('msg-clipped'),
      };
    });
    if (!afterCollapse.ok) {
      failures.push(
        'after height-change/T246 path, in-view virt-shells not rematerialised ' +
        JSON.stringify(afterCollapse),
      );
    }

    // 🎯T246: stay material while partially on-screen; collapse/dematerialize only when fully above fold.
    // Controlled free-scroll geometry (short viewport would pin-collapse tall on new msg).
    const t246Virt = await page.evaluate(async () => {
      const el = document.getElementById('messages');
      // Clear for a short controlled transcript.
      el.innerHTML = '';
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

    console.log(JSON.stringify({ stats, after, afterCollapse, t246Virt, failures }, null, 2));
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
