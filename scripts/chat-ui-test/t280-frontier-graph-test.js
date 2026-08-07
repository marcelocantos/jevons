// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic Playwright smoke for 🎯T280: Frontier Graph owner-readable default.
//
// SUPERSEDED IN PART BY 🎯T294. T280's single-primary default was a false fix:
// opening one component of many left a wide flat graph as an illegible strip in
// a mostly empty pane, which T280's own cover oracle happily passed. The owner
// default is now the full multi-component pack (fill + legibility gated by
// scripts/chat-ui-test/t294-frontier-graph-test.js).
//
// What survives here, and what this file still gates: whatever the default
// path renders, it must never be the tall-empty-Component-column layout.
//
//   node scripts/chat-ui-test/t280-frontier-graph-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const OUT_DIR = path.join(__dirname, 'artifacts');
fs.mkdirSync(OUT_DIR, { recursive: true });

// Orthograph-shaped multi-component payload (owner evidence: 3 diagrams).
// Each diagram is a small valid flowchart so Mermaid can render quickly.
function multiDiagramPayload() {
  const d0 = {
    id: 'c0',
    kind: 'component',
    title: 'Component (24 nodes)',
    mermaid:
      "%%{init: {'flowchart': {'useMaxWidth': true}}}%%\n" +
      'flowchart TB\n' +
      '  T1["T1 · Alpha"]\n' +
      '  T2["T2 · Beta"]\n' +
      '  T3["T3 · Gamma"]\n' +
      '  T4["T4 · Delta"]\n' +
      '  T1 -.->|needs| T2\n' +
      '  T2 -.->|needs| T3\n' +
      '  T3 -.->|needs| T4\n',
    node_count: 24,
    edge_count: 40,
  };
  const d1 = {
    id: 'c1',
    kind: 'component',
    title: 'Component (12 nodes)',
    mermaid:
      "%%{init: {'flowchart': {'useMaxWidth': true}}}%%\n" +
      'flowchart TB\n' +
      '  T10["T10 · Small"]\n' +
      '  T11["T11 · Peer"]\n' +
      '  T10 -.->|needs| T11\n',
    node_count: 12,
    edge_count: 8,
  };
  const d2 = {
    id: 'orphans',
    kind: 'orphans',
    title: 'Orphans (8)',
    mermaid:
      "%%{init: {'flowchart': {'useMaxWidth': true}}}%%\n" +
      'flowchart TB\n' +
      '  T99["T99 · Orphan"]\n',
    node_count: 8,
    edge_count: 0,
  };
  return {
    available: true,
    pack: 'wrap-grid',
    node_count: 64,
    edge_count: 129,
    diagrams: [d0, d1, d2],
    mermaid:
      '%% jevons-frontier-pack pack=wrap-grid diagrams=3 %%\n' +
      d0.mermaid + '\n' + d1.mermaid + '\n' + d2.mermaid + '\n',
  };
}

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  const payload = multiDiagramPayload();
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 't280-frontier-graph-test', ok: true }));
        return;
      }
      // Stub frontier graph API (multi-component orthograph shape).
      if (u.pathname === '/api/frontier/graph') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(payload));
        return;
      }
      // Quiet other APIs the shell may probe.
      if (u.pathname.startsWith('/api/')) {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ available: false, agents: [], rows: [] }));
        return;
      }
      const rel = u.pathname === '/' ? '/index.html' : u.pathname;
      const file = path.normalize(path.join(webRoot, rel));
      if (!file.startsWith(webRoot)) {
        res.writeHead(403);
        res.end();
        return;
      }
      fs.readFile(file, (err, data) => {
        if (err) {
          res.writeHead(404);
          res.end('not found');
          return;
        }
        res.writeHead(200, { 'Content-Type': contentType(file) });
        res.end(data);
      });
    });
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      resolve({ srv, base: `http://127.0.0.1:${port}` });
    });
    srv.on('error', reject);
  });
}

(async () => {
  const failures = [];
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  // Large viewport mirrors owner desktop panel (~90vw/90vh of big screen).
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });

  try {
    await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.openFrontierGraph === 'function'
        && typeof window.FrontierTable !== 'undefined'
        && typeof window.MermaidActions !== 'undefined'
        && !!window.mermaid,
      null,
      { timeout: 25000 }
    );

    // Pure plan: multi payload → pack of all components before DOM (🎯T294).
    const planCheck = await page.evaluate(() => {
      const payload = {
        available: true,
        pack: 'wrap-grid',
        node_count: 64,
        edge_count: 129,
        diagrams: [
          { id: 'c0', kind: 'component', title: 'Large', mermaid: 'flowchart TB\nA-->B\n', node_count: 24, edge_count: 40 },
          { id: 'c1', kind: 'component', title: 'Small', mermaid: 'flowchart TB\nC-->D\n', node_count: 12, edge_count: 8 },
          { id: 'orphans', kind: 'orphans', title: 'Orphans', mermaid: 'flowchart TB\nE\n', node_count: 8, edge_count: 0 },
        ],
        mermaid: '%% jevons-frontier-pack %%',
      };
      const model = window.FrontierTable.normalizeGraphPayload(payload);
      const plan = window.FrontierTable.resolveFrontierGraphOpenPlan(model);
      return {
        mode: plan.mode,
        primaryId: plan.primary && plan.primary.id,
        diagramCount: plan.diagramCount,
      };
    });
    // 🎯T294: default is the full pack; the primary is still identified (status/pin).
    if (planCheck.mode !== 'pack') {
      failures.push('resolveFrontierGraphOpenPlan mode=' + planCheck.mode + ' (want pack — T294)');
    }
    if (planCheck.primaryId !== 'c0') {
      failures.push('primary id=' + planCheck.primaryId + ' (want c0 largest)');
    }

    // Product path: open Frontier Graph with stubbed multi-diagram API.
    await page.evaluate(() => {
      try { localStorage.removeItem(window.MermaidActions.PIN_STORAGE_KEY); } catch (_) {}
      if (typeof window.closeMermaidPanel === 'function') window.closeMermaidPanel();
    });

    await page.evaluate(() => window.openFrontierGraph({ pin: false }));

    // Wait for the component SVGs to render.
    await page.waitForFunction(
      () => {
        const body = document.getElementById('mvp-body');
        if (!body) return false;
        const packBlocks = body.querySelectorAll('.mvp-pack-block');
        if (packBlocks.length >= 2) return true; // fail path detected
        const svg = body.querySelector('svg');
        return !!(svg && (svg.clientWidth > 0 || svg.getBoundingClientRect().width > 0));
      },
      null,
      { timeout: 20000 }
    ).catch(() => {});

    // Give scale-to-fill rAF a beat.
    await page.waitForTimeout(300);

    const layout = await page.evaluate(() => {
      const body = document.getElementById('mvp-body');
      const panel = document.getElementById('mermaid-viz-panel');
      if (!body || !panel) return { ok: false, reason: 'no panel body' };

      const packBlocks = Array.from(body.querySelectorAll('.mvp-pack-block'));
      const svgs = Array.from(body.querySelectorAll('svg'));
      const isPack = body.classList.contains('mvp-pack');
      const isScaleFill = body.classList.contains('mvp-scale-fill');

      // Measure pack blocks if present (trainwreck path).
      const blockMetrics = packBlocks.map((block) => {
        const br = block.getBoundingClientRect();
        const svg = block.querySelector('svg');
        const sr = svg ? svg.getBoundingClientRect() : { width: 0, height: 0 };
        return {
          blockW: br.width,
          blockH: br.height,
          svgW: sr.width,
          svgH: sr.height,
        };
      });

      const tall = window.MermaidActions.assessTallEmptyColumnLayout(blockMetrics);

      // Single-pane cover (product path).
      const pane = body.getBoundingClientRect();
      let svgDisplayW = 0;
      let svgDisplayH = 0;
      if (svgs.length === 1) {
        const r = svgs[0].getBoundingClientRect();
        svgDisplayW = r.width;
        svgDisplayH = r.height;
      }
      const cover = window.MermaidActions.assessSingleGraphPaneCover({
        paneW: pane.width,
        paneH: pane.height,
        svgDisplayW: svgDisplayW,
        svgDisplayH: svgDisplayH,
      });

      return {
        ok: true,
        open: panel.classList.contains('open') && !panel.hidden,
        large: panel.classList.contains('mvp-large'),
        isPack: isPack,
        isScaleFill: isScaleFill,
        packBlockCount: packBlocks.length,
        svgCount: svgs.length,
        tallEmpty: tall,
        cover: cover,
        status: (document.getElementById('mvp-status') || {}).textContent || '',
        bodyText: (body.textContent || '').slice(0, 200),
      };
    });

    if (!layout.ok) {
      failures.push('layout probe: ' + layout.reason);
    } else {
      if (!layout.open) failures.push('panel not open');
      if (!layout.large) failures.push('panel not mvp-large');

      // The surviving T280 gate: never tall empty Component columns.
      if (layout.tallEmpty && layout.tallEmpty.isTallEmpty) {
        failures.push(
          'tall-empty-column layout detected (count=' +
          layout.tallEmpty.tallEmptyCount + ') — owner trainwreck'
        );
      }
      // 🎯T294: every component is rendered, not just the primary.
      if (layout.svgCount !== 3) {
        failures.push('expected 3 component SVGs, got ' + layout.svgCount);
      }
      if (layout.svgCount === 1 && layout.cover && !layout.cover.ok) {
        failures.push(
          'single SVG cover too low (' +
          (layout.cover.cover != null ? layout.cover.cover.toFixed(3) : '?') +
          ') — micro island / not scale-to-fill'
        );
      }
      // Status should mention primary of N when multi payload.
      if (layout.status && !/primary of 3|nodes/.test(layout.status)) {
        // Not fatal if wording differs slightly; prefer node meta presence.
        if (!/nodes/.test(layout.status)) {
          failures.push('status missing graph meta: ' + layout.status);
        }
      }
    }

    // Negative oracle: synthetic tall-empty metrics MUST fail (oracle armed).
    const oracleArmed = await page.evaluate(() => {
      const bad = window.MermaidActions.assessTallEmptyColumnLayout([
        { blockW: 200, blockH: 700, svgW: 60, svgH: 40 },
        { blockW: 200, blockH: 700, svgW: 50, svgH: 45 },
        { blockW: 200, blockH: 700, svgW: 55, svgH: 50 },
      ]);
      return bad.isTallEmpty === true;
    });
    if (!oracleArmed) {
      failures.push('tall-empty oracle not armed (synthetic trainwreck did not fail)');
    }

    await page.screenshot({
      path: path.join(OUT_DIR, 't280-frontier-graph.png'),
      fullPage: true,
    });
  } catch (e) {
    failures.push('exception: ' + (e && e.message ? e.message : e));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL t280-frontier-graph-test:');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('ok - T280 Frontier Graph default has no tall-empty columns (fill+legibility: T294)');
  console.log('screenshot: artifacts/t280-frontier-graph.png');
})();
