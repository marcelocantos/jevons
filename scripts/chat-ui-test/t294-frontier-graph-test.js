// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic Playwright gate for 🎯T294: Frontier Graph fill + legibility + loud errors.
//
// Two owner screenshot failures this test refuses to let back in:
//
//   (a) micro horizontal illegible strip — T280 opened one component of seven
//       and contain-scaled it. A wide flat mermaid fills the width, collapses
//       to a sliver of height, and every label shrinks below readable. The
//       T280 cover oracle (≥95% of ONE axis) passes this happily.
//   (b) empty paste shell after a graph error — the API answers HTTP 200 with
//       an `error` field when the bullseye CLI panics, and the panel rendered
//       "Project graph viz / No graph loaded", burying the panic in a status line.
//
// Both classes are armed in-page: the test asserts the PRE-FIX behaviour is
// still detected as a failure by the same oracles that gate the fixed path.
// Label legibility is measured off the real rendered SVG (computed font-size
// scaled by the applied viewBox ratio), not just the pure plan.
//
//   node scripts/chat-ui-test/t294-frontier-graph-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const OUT_DIR = path.join(__dirname, 'artifacts');
fs.mkdirSync(OUT_DIR, { recursive: true });

const INIT = "%%{init: {'flowchart': {'useMaxWidth': false}}}%%\n";

// Wide flat primary — the shape that breaks contain-scaling: many siblings on
// a rank, only a few ranks deep, with real-length target labels. Branchy on
// purpose (a single chain is pathologically wide and unrepresentative).
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
  return INIT +
    'flowchart TB\n' +
    '  A' + i + '["T' + (300 + i) + ' · alpha"]\n' +
    '  B' + i + '["T' + (400 + i) + ' · beta"]\n' +
    '  A' + i + ' -.->|needs| B' + i + '\n';
}

// Seven components, one of them the wide flat primary (owner: 7 diagrams).
function multiDiagramPayload() {
  const diagrams = [{
    id: 'c0',
    kind: 'component',
    title: 'Component (24 nodes)',
    mermaid: widePrimaryMermaid(),
    node_count: 24,
    edge_count: 18,
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
    diagrams: diagrams,
    mermaid: '%% jevons-frontier-pack pack=wrap-grid diagrams=7 %%\n' +
      diagrams.map(function (d) { return d.mermaid; }).join('\n'),
  };
}

// Fail class (b) fixture: bullseye CLI panic surfaced as HTTP 200 + error.
const PANIC_MESSAGE = 'bullseye open: exit status 101 — panic graph.rs:704';
function panicPayload() {
  return {
    available: false,
    error: PANIC_MESSAGE,
    updated_at: '2026-08-08T00:00:00Z',
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
  const state = { mode: 'graph' };
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 't294-frontier-graph-test', ok: true }));
        return;
      }
      if (u.pathname === '/__mode') {
        state.mode = u.searchParams.get('m') || 'graph';
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ mode: state.mode }));
        return;
      }
      if (u.pathname === '/api/frontier/graph') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(state.mode === 'panic' ? panicPayload() : multiDiagramPayload()));
        return;
      }
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

// Injected into the page: measure a rendered SVG's on-screen label size.
// Mermaid draws labels at a fixed font-size inside the viewBox, so the visual
// size is that font-size times the viewBox→display ratio the fit applied.
const MEASURE_FN = `
function measureVisualLabelPx(svg) {
  if (!svg) return 0;
  var rect = svg.getBoundingClientRect();
  var vb = (svg.getAttribute('viewBox') || '').trim().split(/[\\s,]+/);
  var vbW = vb.length >= 4 ? parseFloat(vb[2]) : 0;
  if (!(vbW > 0) || !(rect.width > 0)) return 0;
  var ratio = rect.width / vbW;
  var label = svg.querySelector('.nodeLabel, foreignObject span, text');
  if (!label) return 0;
  var fs = parseFloat(getComputedStyle(label).fontSize);
  if (!(fs > 0)) return 0;
  return fs * ratio;
}
`;

(async () => {
  const failures = [];
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
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
    await page.addScriptTag({
      content: MEASURE_FN + '\nwindow.measureVisualLabelPx = measureVisualLabelPx;\n',
    });

    // ── Plan level: multi-component payload must open ALL components ──────
    const planCheck = await page.evaluate((payload) => {
      const model = window.FrontierTable.normalizeGraphPayload(payload);
      const plan = window.FrontierTable.resolveFrontierGraphOpenPlan(model);
      return { mode: plan.mode, count: plan.diagramCount, note: plan.statusNote };
    }, multiDiagramPayload());
    if (planCheck.mode !== 'pack') {
      failures.push('open plan mode=' + planCheck.mode + ' (T294 wants pack of all components)');
    }
    if (planCheck.count !== 7) {
      failures.push('open plan diagramCount=' + planCheck.count + ' (want 7)');
    }

    // ── Product path: open the graph ──────────────────────────────────────
    await page.evaluate(() => {
      try { localStorage.removeItem(window.MermaidActions.PIN_STORAGE_KEY); } catch (_) {}
      if (typeof window.closeMermaidPanel === 'function') window.closeMermaidPanel();
    });
    await page.evaluate(() => window.openFrontierGraph({ pin: false }));
    await page.waitForFunction(
      () => {
        const body = document.getElementById('mvp-body');
        if (!body) return false;
        return body.querySelectorAll('.mvp-pack-block svg').length >= 7;
      },
      null,
      { timeout: 25000 }
    ).catch(() => {});
    await page.waitForTimeout(400); // scale/reflow rAF

    const layout = await page.evaluate(() => {
      const MA = window.MermaidActions;
      const body = document.getElementById('mvp-body');
      const panel = document.getElementById('mermaid-viz-panel');
      if (!body || !panel) return { ok: false, reason: 'no panel body' };
      const pane = body.getBoundingClientRect();
      // Path-agnostic: measure whatever is actually painted, so the pre-fix
      // single-primary path is judged by the same oracles rather than being
      // waved through for having no pack blocks.
      const packBlocks = Array.from(body.querySelectorAll('.mvp-pack-block'));
      const svgs = Array.from(body.querySelectorAll('svg'));
      const boxes = packBlocks.length ? packBlocks : svgs;

      const blockMetrics = boxes.map((box) => {
        const br = box.getBoundingClientRect();
        const svg = box.tagName.toLowerCase() === 'svg' ? box : box.querySelector('svg');
        const sr = svg ? svg.getBoundingClientRect() : { width: 0, height: 0 };
        return { blockW: br.width, blockH: br.height, svgW: sr.width, svgH: sr.height };
      });
      const inkBoxes = svgs.map((s) => {
        const r = s.getBoundingClientRect();
        return { w: r.width, h: r.height };
      });

      // Composite extent actually painted.
      let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
      boxes.forEach((b) => {
        const r = b.getBoundingClientRect();
        minX = Math.min(minX, r.left); minY = Math.min(minY, r.top);
        maxX = Math.max(maxX, r.right); maxY = Math.max(maxY, r.bottom);
      });
      const compW = isFinite(minX) ? maxX - minX : 0;
      const compH = isFinite(minY) ? maxY - minY : 0;

      const labelPxs = svgs.map((s) => window.measureVisualLabelPx(s));
      const fitMode = body.getAttribute('data-mvp-fit-mode') || '';

      return {
        ok: true,
        open: panel.classList.contains('open') && !panel.hidden,
        large: panel.classList.contains('mvp-large'),
        blockCount: packBlocks.length,
        svgCount: svgs.length,
        fitMode: fitMode,
        planLabelPx: parseFloat(body.getAttribute('data-mvp-label-px') || '0'),
        minLabelPx: Math.min.apply(null, labelPxs.length ? labelPxs : [0]),
        labelPxs: labelPxs,
        floorPx: MA.MIN_LEGIBLE_LABEL_PX,
        tallEmpty: MA.assessTallEmptyColumnLayout(blockMetrics),
        ink: MA.assessPaneInkCover({ paneW: pane.width, paneH: pane.height }, inkBoxes),
        strip: MA.assessMicroStripLayout({
          paneW: pane.width,
          paneH: pane.height,
          svgDisplayW: compW,
          svgDisplayH: compH,
          scale: 1,
          naturalFontPx: 16,
          minLabelPx: 0.0001, // shape-only: legibility is asserted separately
        }),
        paneW: pane.width,
        paneH: pane.height,
        compW: compW,
        compH: compH,
        status: (document.getElementById('mvp-status') || {}).textContent || '',
      };
    });

    if (process.env.T294_DEBUG) console.log('layout', JSON.stringify(layout, null, 1));
    if (!layout.ok) {
      failures.push('layout probe: ' + layout.reason);
    } else {
      if (!layout.open) failures.push('panel not open');
      if (!layout.large) failures.push('panel not mvp-large');
      if (layout.blockCount !== 7) {
        failures.push(
          'rendered ' + layout.blockCount + ' packed components + ' + layout.svgCount +
          ' SVG(s) — want all 7 components (T280 opened only the primary)'
        );
      }
      if (layout.fitMode !== 'pack-scale-to-fill' && layout.fitMode !== 'reflow-readable') {
        failures.push('fit mode=' + (layout.fitMode || '(none)') + ' — T294 fit not applied');
      }
      // (a) legibility: every rendered component readable without zoom.
      if (!(layout.minLabelPx >= layout.floorPx - 0.5)) {
        failures.push(
          'ILLEGIBLE: smallest rendered label ' + layout.minLabelPx.toFixed(2) +
          'px < floor ' + layout.floorPx + 'px — micro-strip class (a)'
        );
      }
      // (a) shape: composite must not be a sliver in a huge empty pane.
      if (layout.strip && layout.strip.stripShape) {
        failures.push(
          'composite is a thin strip (coverW=' + layout.strip.coverW.toFixed(2) +
          ' coverH=' + layout.strip.coverH.toFixed(2) + ') — huge empty pane'
        );
      }
      if (layout.ink && !layout.ink.ok) {
        failures.push(
          'pane mostly empty: ink cover ' + layout.ink.cover.toFixed(3) +
          ' — graph does not fill the view'
        );
      }
      if (layout.tallEmpty && layout.tallEmpty.isTallEmpty) {
        failures.push('tall-empty-column layout (T280 trainwreck) count=' + layout.tallEmpty.tallEmptyCount);
      }
      if (!/nodes/.test(layout.status)) {
        failures.push('status missing graph meta: ' + layout.status);
      }
    }

    await page.screenshot({ path: path.join(OUT_DIR, 't294-frontier-graph-fill.png'), fullPage: false });

    // ── ARMED (a): the pre-fix contain-only path still fails these oracles ──
    const armedStrip = await page.evaluate(() => {
      const MA = window.MermaidActions;
      const body = document.getElementById('mvp-body');
      const pane = body.getBoundingClientRect();
      // Natural size of the wide primary as mermaid actually rendered it.
      let natW = 0, natH = 0;
      Array.from(body.querySelectorAll('svg')).forEach((svg) => {
        const vb = (svg.getAttribute('viewBox') || '').trim().split(/[\s,]+/);
        const w = vb.length >= 4 ? parseFloat(vb[2]) : 0;
        const h = vb.length >= 4 ? parseFloat(vb[3]) : 0;
        if (w > natW) { natW = w; natH = h; }
      });
      if (!natW || !natH) return { fixtureOk: false };
      // T280 product path: open the primary alone, contain-scale it.
      const contain = MA.computeContainScale(natW, natH, pane.width, pane.height);
      const oldStrip = MA.assessMicroStripLayout({
        paneW: pane.width,
        paneH: pane.height,
        svgDisplayW: natW * contain,
        svgDisplayH: natH * contain,
        scale: contain,
      });
      const oldCover = MA.assessSingleGraphPaneCover({
        paneW: pane.width,
        paneH: pane.height,
        svgDisplayW: natW * contain,
        svgDisplayH: natH * contain,
      });
      const oldInk = MA.assessPaneInkCover(
        { paneW: pane.width, paneH: pane.height },
        [{ w: natW * contain, h: natH * contain }]
      );
      return {
        fixtureOk: true,
        natW: natW,
        natH: natH,
        contain: contain,
        floorScale: MA.legibilityFloorScale({}),
        oldIsMicroStrip: oldStrip.isMicroStrip,
        oldLabelPx: oldStrip.legible.labelPx,
        oldCoverPassed: oldCover.ok,
        oldInkOk: oldInk.ok,
      };
    });
    if (!armedStrip.fixtureOk) {
      failures.push('armed(a): could not measure a rendered viewBox — fixture broken');
    } else {
      if (!(armedStrip.contain < armedStrip.floorScale)) {
        failures.push(
          'armed(a) FIXTURE TOO WEAK: contain scale ' + armedStrip.contain.toFixed(3) +
          ' is already above the legibility floor ' + armedStrip.floorScale.toFixed(3) +
          ' — widen the primary so the old path really breaks'
        );
      }
      if (armedStrip.oldIsMicroStrip !== true) {
        failures.push('armed(a): micro-strip oracle did NOT reject the pre-fix contain path');
      }
      if (armedStrip.oldCoverPassed !== true) {
        failures.push(
          'armed(a): expected the T280 cover oracle to PASS the strip ' +
          '(that is why it was a false fix) — fixture no longer models it'
        );
      }
      if (armedStrip.oldInkOk !== false) {
        failures.push('armed(a): ink oracle did not flag the single-strip empty pane');
      }
    }

    // ── (b) graph error must be loud, never the empty paste shell ─────────
    await page.evaluate((b) => fetch(b + '/__mode?m=panic'), base);
    await page.evaluate(() => {
      if (typeof window.closeMermaidPanel === 'function') window.closeMermaidPanel();
    });
    await page.evaluate(() => window.openFrontierGraph({ pin: false }));
    await page.waitForFunction(
      () => {
        const body = document.getElementById('mvp-body');
        if (!body) return false;
        const t = body.textContent || '';
        return t.indexOf('Loading') < 0;
      },
      null,
      { timeout: 20000 }
    ).catch(() => {});
    await page.waitForTimeout(200);

    const errView = await page.evaluate(() => {
      const body = document.getElementById('mvp-body');
      const status = document.getElementById('mvp-status');
      const panel = document.getElementById('mermaid-viz-panel');
      const html = body ? body.innerHTML : '';
      return {
        open: !!(panel && panel.classList.contains('open') && !panel.hidden),
        hasLoudError: !!(body && body.querySelector('[data-mvp-fetch-error]')),
        errorKind: body && body.querySelector('[data-mvp-fetch-error]')
          ? body.querySelector('[data-mvp-fetch-error]').getAttribute('data-mvp-error-kind')
          : '',
        hasEmptyShell: !!(body && body.querySelector('[data-mvp-empty]')),
        text: (body ? body.textContent || '' : '').replace(/\s+/g, ' ').trim(),
        status: (status ? status.textContent || '' : '').replace(/\s+/g, ' ').trim(),
        // Armed: the pre-fix body for this exact case.
        preFixShellHtml: window.MermaidActions.emptyStateHtml(),
        html: html.slice(0, 400),
      };
    });

    if (!errView.open) failures.push('(b) panel not open on graph error');
    if (errView.hasEmptyShell) {
      failures.push('(b) EMPTY PASTE SHELL rendered for a graph error — owner screenshot class');
    }
    if (!errView.hasLoudError) {
      failures.push('(b) no loud error panel ([data-mvp-fetch-error]) after backend panic');
    }
    if (errView.errorKind !== 'panic') {
      failures.push('(b) error kind=' + (errView.errorKind || '(none)') + ' (want panic)');
    }
    if (!/graph\.rs:704/.test(errView.text)) {
      failures.push('(b) panic detail missing from the panel body: ' + errView.text.slice(0, 120));
    }
    if (/No graph loaded/.test(errView.text)) {
      failures.push('(b) body still reads "No graph loaded" — reads as unimplemented, not broken');
    }
    if (!/exit status 101|Backend panic/.test(errView.status)) {
      failures.push('(b) status line does not carry the failure: ' + errView.status);
    }
    // ARMED (b): the pre-fix shell must be detected as a failure by this check.
    if (!/data-mvp-empty/.test(errView.preFixShellHtml)
      || !/No graph loaded/.test(errView.preFixShellHtml)) {
      failures.push('(b) armed check broken: emptyStateHtml no longer matches the pre-fix shell');
    }

    await page.screenshot({ path: path.join(OUT_DIR, 't294-frontier-graph-error.png'), fullPage: false });
  } catch (e) {
    failures.push('exception: ' + (e && e.message ? e.message : e));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL t294-frontier-graph-test:');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('ok - T294 graph fills pane with legible labels; backend panic is loud');
  console.log('     fail-classes armed: (a) micro strip  (b) empty paste shell');
  console.log('screenshots: artifacts/t294-frontier-graph-fill.png, t294-frontier-graph-error.png');
})();
