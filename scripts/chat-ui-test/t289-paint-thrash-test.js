// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Browser thrash oracle for RHS fleet paint (🎯T289).
//
// Counts REAL DOM mutations (MutationObserver) while a busy fleet streams
// progress updates, which is what the owner feels as stutter and what
// Firefox burns CPU on. Pre-T289 each update rebuilt the whole tree:
// N removed + N added nodes per push. Post-T289 an unchanged tree costs
// zero mutations and a single moving row costs one subtree write.
//
//   node scripts/chat-ui-test/t289-paint-thrash-test.js [--headed]

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

function startStaticServer(agentsPayload) {
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
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 't289-paint-thrash', ok: true }));
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
  // Overseer → PO → six workers: an ordinary busy fleet, not a storm.
  const workers = ['jv-a', 'jv-b', 'jv-c', 'jv-d', 'jv-e', 'jv-f'].map((n, i) => ({
    name: n,
    workdir: poRepo,
    parent: 'jevons-po',
    status: 'running',
    phase: 'working',
    // Only ONE worker's progress line moves per push — the dominant real case.
    step: (i === step % 6) ? ('Bash: go test #' + step) : 'Bash: idle',
    progress: (i === step % 6) ? ('working · Bash: go test #' + step) : 'working · Bash: idle',
  }));
  return [
    { name: 'jevons', workdir: '/Users/x/.jevons/jevons', parent: '', status: 'running' },
    { name: 'jevons-po', workdir: poRepo, parent: 'jevons', status: 'running' },
  ].concat(workers);
}

(async () => {
  const failures = [];
  let step = 0;
  const { srv, base } = await startStaticServer(() => fleetAgents(step));
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.refreshAgents === 'function' || typeof refreshAgents === 'function',
      null, { timeout: 10000 });
    await page.waitForFunction(
      () => document.querySelectorAll('#agents .agent-node').length >= 8,
      null, { timeout: 10000 });

    // Install the mutation counter on the fleet subtree.
    await page.evaluate(() => {
      window.__t289 = { added: 0, removed: 0, records: 0 };
      const target = document.getElementById('agents');
      window.__t289obs = new MutationObserver((records) => {
        for (const r of records) {
          window.__t289.records++;
          window.__t289.added += r.addedNodes.length;
          window.__t289.removed += r.removedNodes.length;
        }
      });
      window.__t289obs.observe(target, { childList: true, subtree: true, attributes: true, characterData: true });
    });

    // ── Case 1: identical snapshot, repeated pushes → zero mutations ──
    const REDUNDANT = 15;
    for (let i = 0; i < REDUNDANT; i++) {
      await page.evaluate(() => window.refreshAgents());
      await page.waitForTimeout(30);
    }
    await page.waitForTimeout(400);
    const idle = await page.evaluate(() => Object.assign({}, window.__t289));
    if (idle.added !== 0 || idle.removed !== 0) {
      failures.push(`redundant pushes mutated the tree: +${idle.added}/-${idle.removed} nodes ` +
        `(pre-T289 rebuilt all rows every push)`);
    }
    console.log(`  redundant x${REDUNDANT}: +${idle.added} / -${idle.removed} nodes, ` +
      `${idle.records} records`);

    // ── Case 2: busy fleet, one row moving per push ──────────────────
    await page.evaluate(() => { window.__t289 = { added: 0, removed: 0, records: 0 }; });
    const PUSHES = 60;
    for (let i = 0; i < PUSHES; i++) {
      step = i + 1;
      // Drive the real coalesced push path, as agents_changed does.
      await page.evaluate(() => {
        if (typeof window.scheduleRefreshAgents === 'function') window.scheduleRefreshAgents();
        else window.refreshAgents();
      });
      await page.waitForTimeout(20);
    }
    await page.waitForTimeout(1000);
    const busy = await page.evaluate(() => Object.assign({}, window.__t289));
    const rows = await page.evaluate(() => document.querySelectorAll('#agents .agent-node').length);

    // Pre-T289 upper bound: one full teardown+rebuild per push.
    const preT289Removed = PUSHES * rows;
    console.log(`  busy x${PUSHES} (${rows} rows): +${busy.added} / -${busy.removed} nodes, ` +
      `${busy.records} records  [pre-T289 ≈ -${preT289Removed}]`);

    // Coalescing alone should keep full rebuilds near zero; row-level patches
    // replace innerHTML of one row, so some adds/removes are expected.
    if (busy.removed > PUSHES) {
      failures.push(`busy fleet removed ${busy.removed} nodes for ${PUSHES} pushes ` +
        `(expected far below one full rebuild per push; pre-T289 ≈ ${preT289Removed})`);
    }
    if (busy.removed >= preT289Removed * 0.25) {
      failures.push(`no meaningful reduction vs pre-T289 rebuild cost ` +
        `(${busy.removed} vs ${preT289Removed})`);
    }

    // ── Case 3: correctness — the tree still tracks the data ─────────
    // A shape change must still repaint, and content must be current.
    step = 999;
    await page.evaluate(() => window.refreshAgents());
    await page.waitForTimeout(300);
    const shown = await page.evaluate(() => {
      const out = [];
      document.querySelectorAll('#agents .agent-node').forEach((n) => {
        out.push({ agent: n.dataset.agent, text: n.textContent });
      });
      return out;
    });
    const moving = shown.filter((r) => /go test #999/.test(r.text));
    if (moving.length !== 1) {
      failures.push(`expected exactly one row showing the current step, got ${moving.length} ` +
        `(diff paint dropped an update)`);
    }
    if (shown.length !== rows) {
      failures.push(`row count drifted after patches: ${shown.length} vs ${rows}`);
    }
    // Rows must still be selectable after patch repaints (handler rebinding).
    await page.evaluate(() => {
      const n = document.querySelector('#agents .agent-node[data-agent="jv-c"]');
      if (n) n.click();
    });
    await page.waitForTimeout(200);
    const selected = await page.evaluate(() =>
      !!document.querySelector('#agents .agent-node[data-agent="jv-c"].selected'));
    if (!selected) failures.push('row click stopped selecting after diff paint (handler lost)');

  } catch (e) {
    failures.push('exception: ' + (e && e.message ? e.message : String(e)));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('t289-paint-thrash-test: FAILED');
    failures.forEach((f) => console.error('  ✗ ' + f));
    process.exit(1);
  }
  console.log('ok - t289-paint-thrash-test (🎯T289 fleet paint coalesce + diff)');
})();
