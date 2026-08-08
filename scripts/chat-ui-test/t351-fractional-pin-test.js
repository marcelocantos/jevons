// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Browser oracle for the residual post-T350 ~1px jiggle (🎯T351).
//
// Measured on the daily UI (not guessed): every scrollTop write is
// integer-quantized — scrollDown/pinToEndGated pin to
// finalPinScrollTop(sh, ch) = sh − ch with INTEGER scrollHeight /
// clientHeight, and loadEarlier compensates page inserts with an INTEGER
// scrollHeight delta — while the true content geometry is FRACTIONAL
// (turn-markers 13.1875px, context tabs, 22.4px text lines, and a
// fractional container clientHeight ~790.7px). Whenever the fractional
// part of total content height drifts (live turn appends, hydrate pages),
// the integer-pinned viewport sits at a NEW sub-pixel offset from the true
// content bottom → all visible text shifts by ±(0..1)px. On a hard reload
// the progressive hydrate loop repeats this every ~420ms for minutes.
//
// Why the T341/T350 oracle greenwashed (maxTopDelta=0 while product moved):
// it never ran loadEarlier/progressive hydrate, and its seeded transcript's
// total-height FRACTION never changed after seeding — so the integer pin sat
// at a constant sub-pixel offset and every probe was exactly still. The
// jiggle needs fraction DRIFT, which only the live/hydrate paths produce.
//
// This oracle measures the owner-visible truth directly: the gap between
// the container's viewport bottom edge and the last message's rendered
// bottom edge (both fractional rects). While pinned in track mode that gap
// must be EXACTLY constant across:
//   Leg A — live appends whose heights change the total-height fraction;
//   Leg B — real loadEarlier hydrate pages interleaved with such appends.
//
// Pre-fix this test FAILS (gap takes multiple sub-pixel values); post-fix
// (clamp-exact pins via over-assign + rect-exact hydrate compensation) the
// gap is a single constant.
//
//   node scripts/chat-ui-test/t351-fractional-pin-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const PAGE_LINES = 40;

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

// A history page: alternating user/assistant journal lines (the real
// /api/history shape loadEarlier fetches and coalesces into lazy shells).
function historyPage(end) {
  const start = Math.max(0, end - PAGE_LINES);
  const lines = [];
  for (let i = start; i < end; i++) {
    const ts = new Date(1754600000000 + i * 60000).toISOString();
    if (i % 2 === 0) {
      lines.push(JSON.stringify({
        type: 'user',
        timestamp: ts,
        message: { role: 'user', content: 'hydrated user line ' + i + ' with enough words to take real width.' },
      }));
    } else {
      lines.push(JSON.stringify({
        type: 'assistant',
        timestamp: ts,
        message: { role: 'assistant', content: [{ type: 'text', text: 'hydrated assistant reply ' + i + '.\nSecond line keeps shells realistic.' }] },
      }));
    }
  }
  return { lines: lines, start: start };
}

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/api/history') {
        const end = parseInt(u.searchParams.get('end') || '0', 10) || 0;
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(historyPage(end)));
        return;
      }
      if (u.pathname === '/api/agents') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify([]));
        return;
      }
      if (u.pathname === '/api/portfolios') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ portfolios: [] }));
        return;
      }
      if (u.pathname === '/api/cost') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ accounting: 'off', billable: false, alerts: [] }));
        return;
      }
      if (u.pathname === '/api/workers') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify([]));
        return;
      }
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 't351-fractional-pin', ok: true }));
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
  const page = await browser.newPage({ viewport: { width: 1200, height: 803 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.scrollDown === 'function' && typeof window.addMsg === 'function' &&
        typeof window.loadEarlier === 'function' && typeof window.handle === 'function',
      null, { timeout: 10000 });

    // Seed a tall transcript through the real append path, then pin.
    await page.evaluate(() => {
      for (let i = 0; i < 60; i++) {
        window.addMsg(i % 2 === 0 ? 'user' : 'jevons',
          'seed bubble ' + i + ' line one of chat text.\n'.repeat(1 + (i % 4)));
      }
      window.enterTrackBottom();
      window.scrollDown({ force: true });
    });
    await page.waitForTimeout(250);

    // The owner-visible truth: fractional gap between the container's
    // viewport bottom edge and the last message's rendered bottom edge.
    // While pinned it must never change (it is the "did the text under my
    // eyes move?" number — sub-pixel changes rasterize as the ~1px jiggle).
    const readGap = `(() => {
      const m = document.getElementById('messages');
      const all = m.querySelectorAll('.msg');
      const last = all[all.length - 1];
      const gap = m.getBoundingClientRect().bottom - last.getBoundingClientRect().bottom;
      return { gap: gap, st: m.scrollTop, sh: m.scrollHeight, ch: m.clientHeight,
               mode: window.followMode, n: all.length };
    })()`;

    const boot = await page.evaluate(readGap);
    if (boot.mode !== 'track') failures.push('expected track mode after seed, got ' + boot.mode);
    console.log('  boot:', JSON.stringify(boot));

    // ── Leg A: live appends drift the total-height fraction ──────────
    // Heights 1..4 text lines (22.4px each + fractional padding): each
    // append moves frac(total). Integer pins land at a different sub-pixel
    // offset from the true bottom each time → gap varies pre-fix.
    const legA = await page.evaluate(async (readSrc) => {
      const read = () => eval(readSrc);
      const gaps = [];
      for (let i = 0; i < 12; i++) {
        window.addMsg(i % 2 === 0 ? 'jevons' : 'user',
          'live turn ' + i + ' text.\n'.repeat(1 + (i % 3)));
        // scrollDownThrottled rAF + scrollDown's double-rAF pin.
        for (let f = 0; f < 4; f++) await new Promise((r) => requestAnimationFrame(r));
        gaps.push(read());
      }
      return gaps;
    }, readGap);

    // ── Leg B: real hydrate pages + drifting appends ─────────────────
    // history_meta arms oldestIndex; drive the REAL loadEarlier (fetch,
    // lazy-shell insert above, scroll compensation) interleaved with live
    // fraction drift — the daily post-reload situation the owner sees.
    const legB = await page.evaluate(async (arg) => {
      const read = () => eval(arg.readSrc);
      window.handle({ type: 'history_meta', older: arg.pages * arg.pageLines, start: arg.pages * arg.pageLines, total: 4000 });
      for (let f = 0; f < 6; f++) await new Promise((r) => requestAnimationFrame(r));
      const gaps = [read()];
      for (let i = 0; i < arg.pages; i++) {
        window.addMsg('jevons', 'drift turn ' + i + ' text.\n'.repeat(1 + (i % 3)));
        for (let f = 0; f < 4; f++) await new Promise((r) => requestAnimationFrame(r));
        await window.loadEarlier();
        for (let f = 0; f < 4; f++) await new Promise((r) => requestAnimationFrame(r));
        gaps.push(read());
      }
      return gaps;
    }, { readSrc: readGap, pages: 8, pageLines: PAGE_LINES });

    const analyze = (name, gaps) => {
      let maxDelta = 0;
      const distinct = new Set();
      for (let i = 0; i < gaps.length; i++) {
        distinct.add(Math.round(gaps[i].gap * 64) / 64); // 1/64px layout units
        if (i > 0) maxDelta = Math.max(maxDelta, Math.abs(gaps[i].gap - gaps[i - 1].gap));
      }
      console.log('  ' + name + ':', JSON.stringify({
        samples: gaps.length,
        maxDelta: maxDelta,
        distinctGaps: distinct.size,
        gaps: gaps.map((g) => Math.round(g.gap * 1000) / 1000),
      }));
      if (gaps.some((g) => g.mode !== 'track')) {
        failures.push(name + ': left track mode mid-run — pin path broken');
      }
      // EXACT stillness: any sub-pixel gap change is the owner's jiggle.
      if (maxDelta !== 0) {
        failures.push(name + ': pinned bottom gap moved (maxDelta=' + maxDelta +
          ', distinct=' + distinct.size + ') — integer-quantized pin/compensation ' +
          'vs fractional content (🎯T351 residual jiggle)');
      }
      return distinct;
    };

    analyze('legA-live-drift', legA);
    analyze('legB-hydrate', legB);

    // Churn guards — the stress must actually exercise the paths.
    const after = await page.evaluate(readGap);
    if (legB.length < 5) failures.push('legB did not run enough hydrate cycles');
    if (after.n < 60 + 12 + 8 + 100) {
      failures.push('hydrate pages did not land (' + after.n + ' msgs) — loadEarlier no-op, oracle would greenwash');
    }

    // Pure-layer gate: pin writes must be clamp-exact (over-assign) and
    // hydrate compensation rect-exact.
    const pure = await page.evaluate(() => {
      if (typeof VirtualList === 'undefined') return null;
      return {
        pinWrite: VirtualList.pinWriteScrollTop ? VirtualList.pinWriteScrollTop(1000.5) : null,
        pinned: VirtualList.isPinnedAtEnd ? VirtualList.isPinnedAtEnd(699.296875, 1490, 791) : null,
        notPinned: VirtualList.isPinnedAtEnd ? VirtualList.isPinnedAtEnd(690, 1490, 791) : null,
        hydrate: VirtualList.hydrateCompensatedScrollTop
          ? VirtualList.hydrateCompensatedScrollTop(100, 50, 63.1875, 1000, 1013)
          : null,
      };
    });
    if (!pure || pure.pinWrite == null || pure.pinned == null || pure.hydrate == null) {
      failures.push('VirtualList T351 helpers missing (pinWriteScrollTop / isPinnedAtEnd / hydrateCompensatedScrollTop)');
    } else {
      if (pure.pinWrite !== 1000.5) failures.push('pinWriteScrollTop must over-assign scrollHeight, got ' + pure.pinWrite);
      if (pure.pinned !== true) failures.push('isPinnedAtEnd must accept fractional-max scrollTop, got ' + pure.pinned);
      if (pure.notPinned !== false) failures.push('isPinnedAtEnd must reject a clearly-unpinned scrollTop, got ' + pure.notPinned);
      if (Math.abs(pure.hydrate - 113.1875) > 1e-9) {
        failures.push('hydrateCompensatedScrollTop must be rect-exact (want 113.1875, got ' + pure.hydrate + ')');
      }
    }
  } catch (e) {
    failures.push('exception: ' + (e && e.message ? e.message : String(e)));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL t351-fractional-pin-test');
    failures.forEach((f) => console.error('  - ' + f));
    process.exit(1);
  }
  console.log('PASS t351-fractional-pin-test');
})();
