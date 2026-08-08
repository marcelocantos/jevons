// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Browser thrash oracle for continuous ~1px text jiggle (🎯T341).
//
// Idle page (track mode, no owner typing / no stream tokens) must not:
//   * rewrite scrollTop on every virtualize pass
//   * oscillate scrollTop / scrollHeight under simulated agents_changed
//     + repeated virtualize while tracking
//
// Measurement (not CSS guess): count __layoutThrash counters and sample
// #messages scrollTop / scrollHeight / a message getBoundingClientRect().top
// over an idle window with fleet refresh + virtualize spam.
//
//   node scripts/chat-ui-test/t341-jiggle-thrash-test.js [--headed]

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

function startStaticServer(agentsPayload, costPayload) {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/api/agents') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(agentsPayload()));
        return;
      }
      if (u.pathname === '/api/portfolios') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ portfolios: [] }));
        return;
      }
      if (u.pathname === '/api/cost') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(costPayload()));
        return;
      }
      if (u.pathname === '/api/workers') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify([]));
        return;
      }
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 't341-jiggle-thrash', ok: true }));
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

const poRepo = '/Users/x/work/github.com/org/po-repo';

function fleetAgents(step) {
  const workers = ['jv-a', 'jv-b', 'jv-c'].map((n, i) => ({
    name: n,
    workdir: poRepo,
    parent: 'jevons-po',
    status: 'running',
    phase: 'working',
    step: (i === step % 3) ? ('Bash: go test #' + step) : 'Bash: idle',
    progress: (i === step % 3) ? ('working · Bash: go test #' + step) : 'working · Bash: idle',
  }));
  return [
    { name: 'jevons', workdir: '/Users/x/.jevons/jevons', parent: '', status: 'running' },
    { name: 'jevons-po', workdir: poRepo, parent: 'jevons', status: 'running' },
  ].concat(workers);
}

function costBody(step) {
  // Vary cents so the ticker text changes (width churn candidate).
  const g = (1.2 + (step % 5) * 0.01).toFixed(2);
  const f = (0.4 + (step % 3) * 0.01).toFixed(2);
  return {
    accounting: 'list_price',
    billable: true,
    global_usd_per_hour: Number(g),
    fleet_usd_per_hour: Number(f),
    alerts: [],
  };
}

(async () => {
  const failures = [];
  let step = 0;
  const { srv, base } = await startStaticServer(
    () => fleetAgents(step),
    () => costBody(step),
  );
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.scrollDown === 'function' && typeof window.virtualizeMessages === 'function',
      null, { timeout: 10000 });

    // Seed a tall transcript so virtualize + track pin are meaningful.
    await page.evaluate(() => {
      const msgs = document.getElementById('messages');
      if (!msgs) throw new Error('no #messages');
      // Clear any boot chrome noise we can; append synthetic bubbles.
      for (let i = 0; i < 40; i++) {
        const d = document.createElement('div');
        d.className = 'msg ' + (i % 2 === 0 ? 'user' : 'jevons');
        d._layoutRole = i % 2 === 0 ? 'user' : 'jevons';
        const body = document.createElement('div');
        body.className = 'msg-body';
        // Vary height; a few tall bubbles near the end stress expand/collapse.
        const lines = (i > 34) ? 24 : (2 + (i % 5));
        body.textContent = ('Line of chat text for bubble ' + i + '.\n').repeat(lines);
        d.appendChild(body);
        d._body = body;
        d._fullText = body.textContent;
        d._fullRole = d._layoutRole;
        d._layoutText = body.textContent;
        if (i > 34) {
          d._autoExpanded = true;
          d._expanded = true;
        }
        msgs.appendChild(d);
      }
      if (typeof window.enterTrackBottom === 'function') window.enterTrackBottom();
      window.scrollDown({ force: true });
      if (typeof window.virtualizeMessages === 'function') window.virtualizeMessages();
    });
    await page.waitForTimeout(200);

    // Confirm track + at bottom.
    const boot = await page.evaluate(() => {
      const msgs = document.getElementById('messages');
      return {
        mode: window.followMode,
        dist: msgs.scrollHeight - msgs.clientHeight - msgs.scrollTop,
        n: msgs.querySelectorAll('.msg').length,
        gutter: getComputedStyle(msgs).scrollbarGutter,
      };
    });
    if (boot.mode !== 'track') failures.push('expected track mode after seed, got ' + boot.mode);
    if (boot.n < 30) failures.push('expected seeded messages, got ' + boot.n);
    if (boot.gutter && boot.gutter !== 'stable' && boot.gutter !== 'auto') {
      // Some engines report 'stable'; fail only if clearly wrong when supported.
    }
    console.log('  boot:', JSON.stringify(boot));

    // Reset thrash counters; sample geometry while idle under stress.
    await page.evaluate(() => {
      window.__layoutThrash = { virtualize: 0, scrollDown: 0, scrollDownPinned: 0, refreshAgents: 0 };
      window.__t341Samples = [];
      const msgs = document.getElementById('messages');
      const probe = msgs.querySelector('.msg.jevons') || msgs.querySelector('.msg');
      window.__t341Probe = probe;
      window.__t341Timer = setInterval(() => {
        const m = document.getElementById('messages');
        const p = window.__t341Probe;
        const r = p ? p.getBoundingClientRect() : null;
        window.__t341Samples.push({
          t: performance.now(),
          scrollTop: m.scrollTop,
          scrollHeight: m.scrollHeight,
          clientHeight: m.clientHeight,
          probeTop: r ? r.top : null,
        });
      }, 16);
    });

    // Stress: fleet refresh (agents_changed path) + virtualize spam while idle.
    const STRESS_MS = 1200;
    const PUSHES = 40;
    for (let i = 0; i < PUSHES; i++) {
      step = i + 1;
      await page.evaluate(() => {
        if (typeof window.scheduleRefreshAgents === 'function') window.scheduleRefreshAgents();
        else if (typeof window.refreshAgents === 'function') window.refreshAgents();
        if (typeof window.scheduleVirtualize === 'function') window.scheduleVirtualize();
        else if (typeof window.virtualizeMessages === 'function') window.virtualizeMessages();
        // Micro height noise: toggle minHeight on an off-screen shell if any.
        if (typeof window.scrollDown === 'function') window.scrollDown();
      });
      await page.waitForTimeout(Math.floor(STRESS_MS / PUSHES));
    }
    // Cost ticker text churn
    for (let i = 0; i < 5; i++) {
      step = 100 + i;
      await page.evaluate(() => {
        if (typeof window.refreshCost === 'function') window.refreshCost();
      });
      await page.waitForTimeout(40);
    }
    await page.waitForTimeout(300);

    const report = await page.evaluate(() => {
      clearInterval(window.__t341Timer);
      const samples = window.__t341Samples || [];
      let maxTopDelta = 0;
      let maxHDelta = 0;
      let maxProbeDelta = 0;
      for (let i = 1; i < samples.length; i++) {
        maxTopDelta = Math.max(maxTopDelta, Math.abs(samples[i].scrollTop - samples[i - 1].scrollTop));
        maxHDelta = Math.max(maxHDelta, Math.abs(samples[i].scrollHeight - samples[i - 1].scrollHeight));
        if (samples[i].probeTop != null && samples[i - 1].probeTop != null) {
          maxProbeDelta = Math.max(maxProbeDelta,
            Math.abs(samples[i].probeTop - samples[i - 1].probeTop));
        }
      }
      // Count distinct scrollTop values in the second half (steady-state idle).
      const half = samples.slice(Math.floor(samples.length / 2));
      const tops = new Set(half.map((s) => Math.round(s.scrollTop * 10) / 10));
      const probes = new Set(half.filter((s) => s.probeTop != null)
        .map((s) => Math.round(s.probeTop * 10) / 10));
      return {
        thrash: Object.assign({}, window.__layoutThrash),
        samples: samples.length,
        maxTopDelta,
        maxHDelta,
        maxProbeDelta,
        distinctScrollTopsHalf: tops.size,
        distinctProbeTopsHalf: probes.size,
        last: samples[samples.length - 1] || null,
      };
    });

    console.log('  thrash:', JSON.stringify(report.thrash));
    console.log('  geometry:', JSON.stringify({
      samples: report.samples,
      maxTopDelta: report.maxTopDelta,
      maxHDelta: report.maxHDelta,
      maxProbeDelta: report.maxProbeDelta,
      distinctScrollTopsHalf: report.distinctScrollTopsHalf,
      distinctProbeTopsHalf: report.distinctProbeTopsHalf,
    }));

    // Pure gate: shouldPinScroll must exist and reject 1px.
    const pure = await page.evaluate(() => {
      if (typeof VirtualList === 'undefined' || !VirtualList.shouldPinScroll) return null;
      return {
        micro: VirtualList.shouldPinScroll({
          prevScrollHeight: 1000, nextScrollHeight: 1001,
          clientHeight: 300, scrollTop: 700,
        }),
        real: VirtualList.shouldPinScroll({
          prevScrollHeight: 1000, nextScrollHeight: 1020,
          clientHeight: 300, scrollTop: 700,
        }),
        thr: VirtualList.PIN_HEIGHT_THRESHOLD_PX,
      };
    });
    if (!pure) failures.push('VirtualList.shouldPinScroll missing in page');
    else {
      if (pure.micro !== false) failures.push('shouldPinScroll must reject 1px height delta, got ' + pure.micro);
      if (pure.real !== true) failures.push('shouldPinScroll must accept real growth, got ' + pure.real);
    }

    // Idle steady-state: second half of samples should not oscillate scrollTop.
    // Allow at most a couple of distinct tops (rounding / one settle), not a cadence.
    if (report.distinctScrollTopsHalf > 3) {
      failures.push('idle scrollTop oscillated: ' + report.distinctScrollTopsHalf +
        ' distinct values in steady-state half (expected ≤3)');
    }
    if (report.distinctProbeTopsHalf > 3) {
      failures.push('idle message rect.top oscillated: ' + report.distinctProbeTopsHalf +
        ' distinct values (expected ≤3) — owner-visible jiggle');
    }
    // Continuous pin writes are the smoking gun; allow a small settle budget.
    const pinned = report.thrash.scrollDownPinned || 0;
    if (pinned > 12) {
      failures.push('scrollDown pinned ' + pinned + ' times under idle stress ' +
        '(expected ≤12 settle pins; continuous pin = jiggle)');
    }
    // virtualize may run often if we spam scheduleVirtualize; that is OK if pin is gated.
    // refreshAgents should run (we pushed) but not imply main jiggle alone.

    // CSS smoke: scrollbar-gutter stable present in product CSS (render engines vary).
    const cssOk = await page.evaluate(() => {
      const m = document.getElementById('messages');
      const ss = getComputedStyle(m);
      // Not all engines expose the longhand; also accept style attribute / sheet text.
      return {
        computed: ss.scrollbarGutter || '',
        hasRule: !!document.querySelector('style') &&
          /scrollbar-gutter\s*:\s*stable/.test(
            Array.from(document.querySelectorAll('style')).map((s) => s.textContent).join('\n')),
      };
    });
    if (!cssOk.hasRule) failures.push('product CSS missing scrollbar-gutter: stable on #messages');
    console.log('  scrollbar-gutter:', JSON.stringify(cssOk));

  } catch (e) {
    failures.push('exception: ' + (e && e.message ? e.message : String(e)));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL t341-jiggle-thrash-test');
    failures.forEach((f) => console.error('  - ' + f));
    process.exit(1);
  }
  console.log('PASS t341-jiggle-thrash-test');
})();
