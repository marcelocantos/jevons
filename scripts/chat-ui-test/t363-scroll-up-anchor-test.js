// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Browser oracle for the 🎯T363 scroll-up stutter.
//
// Owner report: wheel-scrolling UP through the transcript stutters — the
// viewport "jumps up randomly" instead of tracking the wheel.
//
// Mechanism (measured here, not guessed — see the STEP diagnostics this
// prints): history pages arrive as LAZY shells frozen at an ESTIMATED height
// (VirtualList.estimateHeightFromText). Wheeling up drags shells into the
// 800px anticipation band ABOVE the viewport top; they paint, and the row
// snap pass then swaps the estimate for the real rendered height — measured
// deltas of −224px and −27px on single rows sitting ~900–1400px above the
// viewport. Everything below them, including the text under the owner's
// eyes, slides by that delta. #messages runs overflow-anchor:none on purpose
// (browser scroll anchoring fights the follow-scroll pin, see the CSS), so
// nothing re-anchors the viewport. Same failure class as a prepend without
// scroll compensation, one layer down.
//
// The owner-visible truth: a wheel tick of N px must move the text on screen
// by exactly N px. Not N ± whatever a shell above happened to remeasure.
//
//     visualDrift = (probeRectTopAfter - probeRectTopBefore) - wheelStepPx
//
// scrollTop is deliberately NOT the yardstick: compensating for an
// above-the-fold height change moves scrollTop precisely so the screen does
// not move, so a scrollTop-based metric would score a correct fix as a
// failure. The rect is what the owner sees.
//
// Pre-fix this FAILS with drifts of many hundreds of px (all negative — the
// jump-up the owner reported). Post-fix (anchor-preserved virtualize / row
// snap / rematerialize / settle writes) drift stays inside integer-scrollTop
// quantization.
//
//   node scripts/chat-ui-test/t363-scroll-up-anchor-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const PAGE_LINES = 40;
// Total history lines the fake server owns; hydrate walks back to 0 and stops.
const HISTORY_LINES = PAGE_LINES * 6;
// Per-step wheel movement and step count for the scroll-up leg.
const WHEEL_STEP_PX = 240;
const WHEEL_STEPS = 22;
// Integer scrollTop quantization: a compensation of 137.4px can only be
// written as 137. Sub-pixel residue per step is the floor, not the bug.
const MAX_DRIFT_PX = 1.5;

// A history page in the real /api/history shape. Body lengths vary a lot so
// the lazy-shell estimate is badly wrong in both directions — that spread is
// what makes the jump owner-visible.
function historyPage(end) {
  const start = Math.max(0, end - PAGE_LINES);
  const lines = [];
  for (let i = start; i < end; i++) {
    const ts = new Date(1754600000000 + i * 60000).toISOString();
    if (i % 2 === 0) {
      lines.push(JSON.stringify({
        type: 'user',
        timestamp: ts,
        message: { role: 'user', content: 'hydrated user line ' + i + ' asking something.' },
      }));
    } else {
      let text = 'hydrated assistant reply ' + i + '.';
      for (let p = 0; p < 1 + (i % 7); p++) {
        text += '\n\nParagraph ' + p + ' of reply ' + i + ' — enough prose that the ' +
          'rendered height lands well away from the one-line estimate the lazy ' +
          'shell froze, which is the whole point of this fixture.';
      }
      lines.push(JSON.stringify({
        type: 'assistant',
        timestamp: ts,
        message: { role: 'assistant', content: [{ type: 'text', text: text }] },
      }));
    }
  }
  return { lines: lines, start: start };
}

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
      if (u.pathname === '/api/history') {
        return json(historyPage(parseInt(u.searchParams.get('end') || '0', 10) || 0));
      }
      if (u.pathname === '/api/agents') return json([]);
      if (u.pathname === '/api/portfolios') return json({ portfolios: [] });
      if (u.pathname === '/api/workers') return json([]);
      if (u.pathname === '/api/cost') return json({ accounting: 'off', billable: false, alerts: [] });
      if (u.pathname === '/health') return json({ version: 't363-scroll-up-anchor', ok: true });
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

// Snapshot every row's rect + shell state, so a failing step can name the
// rows that moved and where they sat relative to the viewport.
const SNAPSHOT = `(() => {
  const m = document.getElementById('messages');
  const rows = [];
  m.querySelectorAll('.msg').forEach((el, i) => {
    el._t363id = el._vIndex != null ? 'r' + el._vIndex : (el._t363id || 'r' + i);
    const r = el.getBoundingClientRect();
    rows.push({ id: el._t363id, top: r.top, h: r.height,
                shell: el.classList.contains('virt-shell') });
  });
  return { rows: rows, st: m.scrollTop, viewTop: m.getBoundingClientRect().top };
})()`;

function diffRows(before, after) {
  const bmap = new Map(before.rows.map((r) => [r.id, r]));
  const changed = [];
  after.rows.forEach((r) => {
    const b = bmap.get(r.id);
    if (!b) {
      if (r.top < before.viewTop) {
        changed.push({ id: r.id, dh: Math.round(r.h * 10) / 10, above: true, shellBefore: true });
      }
      return;
    }
    if (Math.abs(b.h - r.h) <= 0.01) return;
    changed.push({
      id: r.id,
      dh: Math.round((r.h - b.h) * 10) / 10,
      above: b.top < before.viewTop,
      shellBefore: b.shell,
    });
  });
  return changed;
}

(async () => {
  const failures = [];
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1200, height: 803 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.addMsg === 'function' && typeof window.handle === 'function' &&
        typeof window.loadEarlier === 'function' && typeof VirtualList !== 'undefined',
      null, { timeout: 10000 });

    // Seed live turns, then let the REAL progressive hydrate walk the fake
    // journal back to line 0 — the daily post-reload state: a long stack of
    // lazy shells above a pinned viewport.
    await page.evaluate((n) => {
      for (let i = 0; i < 30; i++) {
        window.addMsg(i % 2 === 0 ? 'user' : 'jevons',
          'seed bubble ' + i + ' of live chat text.\n'.repeat(1 + (i % 3)));
      }
      window.enterTrackBottom();
      window.scrollDown({ force: true });
      window.handle({ type: 'history_meta', older: n, start: n, total: n + 60 });
    }, HISTORY_LINES);

    // Hydrate is rate-limited (HISTORY_PAGE_GAP_MS) — wait for it to drain.
    await page.waitForFunction(
      () => !document.querySelector('.history-sentinel') &&
        window.__transcriptRows && window.__transcriptRows.length > 150,
      null, { timeout: 30000 });
    await page.waitForTimeout(600);

    const boot = await page.evaluate(() => {
      const m = document.getElementById('messages');
      const rows = window.__transcriptRows || [];
      const attached = document.querySelectorAll('#messages-canvas > .msg');
      let shells = 0;
      rows.forEach((row) => {
        if (row && row.el && row.el.classList && row.el.classList.contains('virt-shell')) {
          row.el._t363WasShell = true;
          shells++;
        } else if (row && !row.el) {
          row._t363WasDetached = true;
        }
      });
      return { n: rows.length, attached: attached.length, shells: shells, st: m.scrollTop,
               sh: m.scrollHeight, ch: m.clientHeight };
    });
    console.log('  boot:', JSON.stringify(boot));
    if (boot.n < 150) {
      failures.push('fixture too small: only ' + boot.n + ' records — hydrate did not land');
    }
    if (boot.attached >= boot.n) {
      failures.push('hydrate attached the whole journal (' + boot.attached + ') — T119.3 band failed');
    }

    // ── Wheel up, step by step, measuring visual drift ────────────────
    // Real wheel events (the owner's input): they also latch free mode via
    // the wheel sensor, exactly as a hand on the mouse does.
    await page.locator('#messages').hover();
    await page.evaluate(() => {
      if (typeof window.leaveTrackBottom === 'function') window.leaveTrackBottom();
    });
    await page.mouse.move(500, 400);
    const steps = [];
    for (let i = 0; i < WHEEL_STEPS; i++) {
      const before = await page.evaluate(`(() => {
        const m = document.getElementById('messages');
        const viewTop = m.getBoundingClientRect().top;
        const viewBot = viewTop + m.clientHeight;
        const all = m.querySelectorAll('.msg');
        // Probe = visually top intersecting row. DOM order is attach
        // order, not prefix order, so a linear first-match is the oldest
        // attached node (often the live end, below the fold).
        let probe = null;
        let best = Infinity;
        for (let k = 0; k < all.length; k++) {
          const r = all[k].getBoundingClientRect();
          if (r.bottom <= viewTop + 1 || r.top >= viewBot) continue;
          if (r.top < best) { best = r.top; probe = all[k]; }
        }
        window.__t363probe = probe;
        window.__t363probeIdx = probe && probe._vIndex;
        const snap = ${SNAPSHOT};
        return probe ? { ok: true, rect: probe.getBoundingClientRect().top, snap: snap } : { ok: false };
      })()`);
      if (!before.ok) { failures.push('step ' + i + ': no probe row at the viewport top'); break; }

      await page.mouse.wheel(0, -WHEEL_STEP_PX);
      // Let the scroll handler, the capped rematerialize queue, the row-snap
      // pass and the deferred settle pass all run out.
      await page.waitForTimeout(220);

      const after = await page.evaluate(`(() => {
        let probe = window.__t363probe;
        const idx = window.__t363probeIdx;
        const rows = window.__transcriptRows || [];
        if ((!probe || !probe.isConnected) && idx != null && rows[idx] && rows[idx].el) {
          probe = rows[idx].el;
          window.__t363probe = probe;
        }
        if (!probe || !probe.isConnected) {
          return {
            ok: false, idx: idx, hasRow: !!(rows[idx]), hasEl: !!(rows[idx] && rows[idx].el),
            mode: window.followMode, st: document.getElementById('messages').scrollTop,
          };
        }
        return { ok: true, rect: probe.getBoundingClientRect().top,
                 mode: window.followMode, snap: ${SNAPSHOT} };
      })()`);
      if (!after.ok) {
        failures.push('step ' + i + ': probe row left the DOM ' + JSON.stringify(after));
        break;
      }

      steps.push({
        i: i,
        scrolled: before.snap.st - after.snap.st,
        // The wheel asked for WHEEL_STEP_PX of content movement. Anything
        // else on screen is content the owner did not ask to move.
        drift: (after.rect - before.rect) - WHEEL_STEP_PX,
        mode: after.mode,
        clamped: after.snap.st <= 0,
        changed: diffRows(before.snap, after.snap),
      });
      if (after.snap.st <= 0) break; // hit the top of the transcript
    }

    // Guard: the run must actually have materialized shells, or a green
    // result means nothing (the T341/T350 greenwash lesson).
    const grew = await page.evaluate(() => {
      let n = 0;
      const rows = window.__transcriptRows || [];
      for (let i = 0; i < rows.length; i++) {
        const row = rows[i];
        if (row && row._t363WasDetached && row.el) n++;
        else if (row && row.el && row.el._t363WasShell && row.el._virtSize) n++;
      }
      return n;
    });

    const scored = steps.filter((s) => !s.clamped);
    const maxDrift = scored.reduce((a, s) => Math.max(a, Math.abs(s.drift)), 0);
    const stressed = steps.reduce((a, s) => a + s.changed.filter((c) => c.above).length, 0);
    const worst = scored.slice().sort((a, b) => Math.abs(b.drift) - Math.abs(a.drift))[0];
    console.log('  scroll-up:', JSON.stringify({
      steps: steps.length,
      scoredSteps: scored.length,
      materializedFormerShells: grew,
      aboveViewportHeightChanges: stressed,
      maxDriftPx: Math.round(maxDrift * 100) / 100,
      drifts: scored.map((s) => Math.round(s.drift)),
    }));
    if (worst && Math.abs(worst.drift) > MAX_DRIFT_PX) {
      console.log('  worst step ' + worst.i + ':', JSON.stringify({
        drift: Math.round(worst.drift * 10) / 10,
        scrolled: worst.scrolled,
        rowsChanged: worst.changed,
      }));
    }

    if (scored.length < 8) failures.push('only ' + scored.length + ' scored wheel steps — leg too short to trust');
    if (grew < 8) {
      failures.push('only ' + grew + ' detached rows attached during the scroll — the ' +
        'estimate→natural growth never fired, so a pass here would greenwash');
    }
    if (stressed < 5) {
      failures.push('only ' + stressed + ' above-the-viewport height changes occurred — ' +
        'the stutter mechanism was not exercised');
    }
    if (steps.some((s) => s.mode !== 'free')) {
      failures.push('wheel-up did not latch free mode — the owner scenario was not reproduced');
    }
    if (maxDrift > MAX_DRIFT_PX) {
      failures.push('the text moved ' + Math.round(maxDrift) + 'px more than the wheel asked ' +
        '(limit ' + MAX_DRIFT_PX + 'px) — a row above the viewport changed height without ' +
        'scroll compensation (🎯T363 stutter)');
    }

    // Pure-layer gate: the helpers the wiring depends on must exist and be exact.
    const pure = await page.evaluate(() => {
      if (typeof VirtualList === 'undefined') return null;
      return {
        anchored: VirtualList.anchorPreservedScrollTop
          ? VirtualList.anchorPreservedScrollTop(300, 120, 452.5) : null,
        idx: VirtualList.pickScrollAnchorIndex
          ? VirtualList.pickScrollAnchorIndex(
            [{ top: 0, height: 100 }, { top: 100, height: 100 }, { top: 200, height: 100 }], 150)
          : null,
        uncompensated: VirtualList.scrollUpAnchorTrace
          ? VirtualList.scrollUpAnchorTrace({ compensate: false }).maxAnchorDriftPx : null,
        compensated: VirtualList.scrollUpAnchorTrace
          ? VirtualList.scrollUpAnchorTrace({ compensate: true }).maxAnchorDriftPx : null,
      };
    });
    if (!pure || pure.anchored == null || pure.idx == null || pure.compensated == null) {
      failures.push('VirtualList T363 helpers missing (anchorPreservedScrollTop / ' +
        'pickScrollAnchorIndex / scrollUpAnchorTrace)');
    } else {
      if (Math.abs(pure.anchored - 632.5) > 1e-9) {
        failures.push('anchorPreservedScrollTop must be rect-exact (want 632.5, got ' + pure.anchored + ')');
      }
      if (pure.idx !== 1) failures.push('pickScrollAnchorIndex must pick the row crossing the viewport top, got ' + pure.idx);
      if (!(pure.uncompensated > 0)) failures.push('trace must expose drift when compensation is off');
      if (pure.compensated !== 0) failures.push('compensated trace must be drift-free, got ' + pure.compensated);
    }
  } catch (e) {
    failures.push('exception: ' + (e && e.message ? e.message : String(e)));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL t363-scroll-up-anchor-test');
    failures.forEach((f) => console.error('  - ' + f));
    process.exit(1);
  }
  console.log('PASS t363-scroll-up-anchor-test');
})();
