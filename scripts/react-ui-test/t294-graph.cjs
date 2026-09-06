// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright product path for 🎯T294: React Frontier Graph packs diagrams[]
// and fails loudly. Hermetics cover the plan; this drives the built app.
'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { chromium } = require('../browser-loop-test/node_modules/playwright');

const dist = path.resolve(__dirname, '../../ui/dist');
const INIT = "%%{init: {'flowchart': {'useMaxWidth': false}}}%%\n";

function widePrimaryMermaid() {
  const roots = [
    'T27.3 provider registry', 'T31.2 oracle elicit', 'T104 delivery planes',
    'T148 pluggable backends', 'T194 daily path', 'T243 ambient RSI coach',
  ];
  const leaves = [
    'T125 PO spawn-only', 'T129 hierarchy', 'T155 auto-spawn',
    'T165 deregister', 'T176 status language', 'T188 daemon bounce',
    'T193 file to spawn', 'T197 literal dots', 'T200 portfolios',
    'T268 scale to fill', 'T276 pane aspect pack', 'T277 natural aspect',
    'T280 single primary', 'T289 paint thrash', 'T293 grok logo',
    'T294 graph fill', 'T283 provider classify', 'T287 model prefix',
  ];
  let src = INIT + 'flowchart TB\n';
  roots.forEach((label, i) => { src += '  R' + i + '["' + label + '"]\n'; });
  leaves.forEach((label, i) => { src += '  L' + i + '["' + label + '"]\n'; });
  leaves.forEach((_, i) => {
    src += '  R' + (i % roots.length) + ' -.->|needs| L' + i + '\n';
  });
  return src;
}

function smallComponentMermaid(i) {
  return INIT + 'flowchart TB\n  A' + i + '["T' + (300 + i) + ' · alpha"]\n  B' + i + '["T' + (400 + i) + ' · beta"]\n  A' + i + ' -.->|needs| B' + i + '\n';
}

function multiDiagramPayload() {
  const diagrams = [{
    id: 'c0', kind: 'component', title: 'Component (24 nodes)',
    mermaid: widePrimaryMermaid(), node_count: 24, edge_count: 18,
  }];
  for (let i = 1; i < 7; i++) {
    diagrams.push({
      id: 'c' + i,
      kind: i === 6 ? 'orphans' : 'component',
      title: i === 6 ? 'Orphans (4)' : 'Component (4 nodes)',
      mermaid: smallComponentMermaid(i),
      node_count: 4,
      edge_count: 2,
    });
  }
  return {
    available: true,
    pack: 'wrap-grid',
    node_count: 42,
    edge_count: 29,
    diagrams,
    mermaid: '%% jevons-frontier-pack pack=wrap-grid diagrams=7 %%\n' + diagrams.map((d) => d.mermaid).join('\n'),
  };
}

const PANIC_MESSAGE = 'bullseye open: exit status 101 — panic graph.rs:704';

function contentType(file) {
  return ({ '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }[path.extname(file)]) || 'application/octet-stream';
}

async function main() {
  await fs.access(path.join(dist, 'index.html'));
  const state = { mode: 'graph' };
  const server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, 'http://127.0.0.1');
      if (url.pathname === '/__mode') {
        state.mode = url.searchParams.get('m') || 'graph';
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ mode: state.mode }));
        return;
      }
      if (url.pathname === '/api/frontier/graph') {
        const body = state.mode === 'panic'
          ? { available: false, error: PANIC_MESSAGE, updated_at: '2026-08-08T00:00:00Z' }
          : multiDiagramPayload();
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(body));
        return;
      }
      if (url.pathname.startsWith('/api/')) {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(url.pathname === '/api/agents'
          ? [{ name: 'jevons', purpose: 'overseer', running: true, status: 'running' }, { name: 'jevons-po', purpose: 'po', parent: 'jevons', running: true, status: 'running' }]
          : url.pathname === '/api/rsi/dispositions'
            ? { judgments: [], total: 0, pending: 0, count: 0 }
            : { targets: [], windows: [] }));
        return;
      }
      const file = path.resolve(dist, '.' + decodeURIComponent(url.pathname === '/' ? '/index.html' : url.pathname));
      assert(file.startsWith(dist + path.sep));
      const data = await fs.readFile(file);
      res.setHeader('Content-Type', contentType(file));
      res.end(data);
    } catch {
      res.writeHead(404).end();
    }
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(25000);
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.route('https://fonts.**/*', (route) => route.abort());
  await page.routeWebSocket('**/ws/mux', (socket) => {
    socket.onMessage((payload) => {
      try {
        const frame = JSON.parse(String(payload));
        if (frame.type === 'ping') socket.send(JSON.stringify({ type: 'pong' }));
      } catch { /* ignore */ }
    });
  });

  const failures = [];
  const shots = await fs.mkdtemp(path.join(os.tmpdir(), 'jevons-t294-'));
  try {
    await page.goto(base + '/');
    await page.locator('#frontier-graph').click();
    await page.waitForFunction(() => document.querySelectorAll('#mvp-body .mvp-pack-block svg').length >= 7, null, { timeout: 25000 });
    await page.waitForTimeout(400);

    const layout = await page.evaluate(() => {
      function measureVisualLabelPx(svg) {
        if (!svg) return 0;
        const rect = svg.getBoundingClientRect();
        const vb = (svg.getAttribute('viewBox') || '').trim().split(/[\s,]+/);
        const vbW = vb.length >= 4 ? parseFloat(vb[2]) : 0;
        if (!(vbW > 0) || !(rect.width > 0)) return 0;
        const label = svg.querySelector('.nodeLabel, foreignObject span, text');
        if (!label) return 0;
        const fs = parseFloat(getComputedStyle(label).fontSize);
        if (!(fs > 0)) return 0;
        return fs * (rect.width / vbW);
      }
      const body = document.getElementById('mvp-body');
      const panel = document.getElementById('mermaid-viz-panel');
      if (!body || !panel) return { ok: false, reason: 'no panel body' };
      const pane = body.getBoundingClientRect();
      const packBlocks = Array.from(body.querySelectorAll('.mvp-pack-block'));
      const svgs = Array.from(body.querySelectorAll('svg'));
      let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
      packBlocks.forEach((b) => {
        const r = b.getBoundingClientRect();
        minX = Math.min(minX, r.left); minY = Math.min(minY, r.top);
        maxX = Math.max(maxX, r.right); maxY = Math.max(maxY, r.bottom);
      });
      const labelPxs = svgs.map((s) => measureVisualLabelPx(s));
      const ink = svgs.reduce((n, s) => {
        const r = s.getBoundingClientRect();
        return n + r.width * r.height;
      }, 0);
      const paneArea = pane.width * pane.height;
      const coverW = isFinite(minX) ? (maxX - minX) / pane.width : 0;
      const coverH = isFinite(minY) ? (maxY - minY) / pane.height : 0;
      return {
        ok: true,
        open: panel.classList.contains('open') && !panel.hidden,
        large: panel.classList.contains('mvp-large'),
        pack: body.classList.contains('mvp-pack'),
        blockCount: packBlocks.length,
        svgCount: svgs.length,
        fitMode: body.getAttribute('data-mvp-fit-mode') || '',
        minLabelPx: Math.min.apply(null, labelPxs.length ? labelPxs : [0]),
        inkCover: paneArea > 0 ? Math.min(1, ink / paneArea) : 0,
        coverW,
        coverH,
        status: (document.getElementById('mvp-status') || {}).textContent || '',
        mermaidErrs: window.__jevonsMermaidErrs || [],
      };
    });
    if (!layout.ok) failures.push('layout probe: ' + layout.reason);
    else {
      if (!layout.open) failures.push('panel not open');
      if (!layout.large) failures.push('panel not mvp-large');
      if (!layout.pack) failures.push('body missing mvp-pack');
      if (layout.blockCount !== 7) failures.push('rendered ' + layout.blockCount + ' packed components, want 7');
      if (layout.svgCount < 7) failures.push('svg count ' + layout.svgCount + ', want 7');
      if (layout.fitMode !== 'pack-scale-to-fill' && layout.fitMode !== 'reflow-readable') {
        failures.push('fit mode=' + (layout.fitMode || '(none)'));
      }
      if (!(layout.minLabelPx >= 10.5)) {
        failures.push('ILLEGIBLE: smallest label ' + layout.minLabelPx.toFixed(2) + 'px');
      }
      if (Math.max(layout.coverW, layout.coverH) >= 0.9 && Math.min(layout.coverW, layout.coverH) < 0.35 && layout.minLabelPx < 11) {
        failures.push('composite is an illegible thin strip');
      }
      if (layout.inkCover < 0.25) failures.push('pane mostly empty: ink cover ' + layout.inkCover.toFixed(3));
      if (!/nodes/.test(layout.status)) failures.push('status missing graph meta: ' + layout.status);
      if (layout.mermaidErrs.length) failures.push('mermaid errors: ' + layout.mermaidErrs.join(' | '));
    }
    await page.screenshot({ path: path.join(shots, 't294-frontier-graph-fill.png'), fullPage: false });

    await page.evaluate((b) => fetch(b + '/__mode?m=panic'), base);
    await page.locator('#mvp-close').click();
    await page.locator('#frontier-graph').click();
    await page.waitForFunction(() => {
      const body = document.getElementById('mvp-body');
      return !!(body && (body.querySelector('[data-mvp-fetch-error]') || /could not load|panic/.test(body.textContent || '')));
    });
    const errView = await page.evaluate(() => {
      const body = document.getElementById('mvp-body');
      const status = document.getElementById('mvp-status');
      const panel = document.getElementById('mermaid-viz-panel');
      return {
        open: !!(panel && panel.classList.contains('open') && !panel.hidden),
        hasLoudError: !!(body && body.querySelector('[data-mvp-fetch-error]')),
        errorKind: body && body.querySelector('[data-mvp-fetch-error]')
          ? body.querySelector('[data-mvp-fetch-error]').getAttribute('data-mvp-error-kind')
          : '',
        hasEmptyShell: !!(body && body.querySelector('[data-mvp-empty]')),
        text: (body ? body.textContent || '' : '').replace(/\s+/g, ' ').trim(),
        status: (status ? status.textContent || '' : '').replace(/\s+/g, ' ').trim(),
      };
    });
    if (!errView.open) failures.push('(b) panel not open on graph error');
    if (errView.hasEmptyShell) failures.push('(b) EMPTY PASTE SHELL rendered for a graph error');
    if (!errView.hasLoudError) failures.push('(b) no loud error panel after backend panic');
    if (errView.errorKind !== 'panic') failures.push('(b) error kind=' + (errView.errorKind || '(none)'));
    if (!/graph\.rs:704/.test(errView.text)) failures.push('(b) panic detail missing: ' + errView.text.slice(0, 120));
    if (/No graph loaded/.test(errView.text)) failures.push('(b) body still reads "No graph loaded"');
    if (!/exit status 101|Backend panic/.test(errView.status)) failures.push('(b) status line does not carry the failure: ' + errView.status);
    await page.screenshot({ path: path.join(shots, 't294-frontier-graph-error.png'), fullPage: false });
    if (errors.length) failures.push('pageerror: ' + errors.join(' | '));
  } catch (e) {
    failures.push('exception: ' + (e && e.message ? e.message : e));
  } finally {
    await browser.close();
    server.close();
  }
  if (failures.length) {
    console.error('FAIL t294-graph:');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('ok - T294 React graph packs all components with legible labels; backend panic is loud');
  console.log('screenshots: ' + shots);
}

main();
