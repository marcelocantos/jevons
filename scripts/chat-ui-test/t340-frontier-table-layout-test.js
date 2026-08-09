// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T340: REAL render oracle for frontier table layout.
//
// Prior T331/T332 hermetics only grepped CSS pad sums / width tokens — they
// stayed green while the owner still saw truncated hierarchical ids and uneven
// text-to-text gutters. This Playwright fixture measures after layout:
//
//   1) Full hierarchical ids (🎯T254.1 / 🎯T262.3) with no ellipsis clip
//      (text ink fully inside cell; overflow visible / no text-overflow ellipsis;
//      scrollWidth not exceeding clientWidth under a clipping overflow).
//   2) Even TEXT-to-TEXT gutters via Range.getBoundingClientRect ink boxes —
//      same order of magnitude across id↔name, name↔status, status↔fanout.
//   3) Product CSS must not reintroduce width:7ch + overflow:hidden +
//      text-overflow:ellipsis under global border-box.
//
//   node scripts/chat-ui-test/t340-frontier-table-layout-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const OUT_DIR = path.join(__dirname, 'artifacts');
fs.mkdirSync(OUT_DIR, { recursive: true });

// Load-bearing hierarchical ids from owner reject of T332 residual.
// Names are deliberately long so they ellipsize in the name column (owner rows
// are long assertions). Short names would leave mid-cell dead space and make
// text-to-text name↔status look like a chasm even with correct column pads.
const LONG_NAME =
  'Owner-visible hierarchical frontier leaf with a long assertion name that must ellipsize in the name column under normal RHS width';
const FRONTIER_ROWS = [
  {
    id: 'T254.1',
    name: LONG_NAME + ' (alpha)',
    status: 'Identified',
    fanout: 2,
    dependents: [{ id: 'T254.2', name: 'Child' }],
  },
  {
    id: 'T262.3',
    name: LONG_NAME + ' (beta)',
    status: 'Converging',
    fanout: 1,
    dependents: [{ id: 'T262.4', name: 'Peer' }],
  },
  {
    id: 'T27.5',
    name: LONG_NAME + ' (gamma)',
    status: 'Identified',
    fanout: 0,
    dependents: [],
  },
  {
    id: 'T1',
    name: LONG_NAME + ' (short-id residual)',
    status: 'Identified',
    fanout: 3,
    dependents: [{ id: 'T2', name: 'X' }],
  },
];

function frontierPayload() {
  return {
    available: true,
    ledger: '/tmp/t340-bullseye.yaml',
    cwd: '/tmp/t340-proj',
    targets: FRONTIER_ROWS,
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
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 't340-frontier-table-layout-test', ok: true }));
        return;
      }
      if (u.pathname === '/api/frontier' || u.pathname.startsWith('/api/frontier?')) {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(frontierPayload()));
        return;
      }
      if (u.pathname === '/api/agents') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify([]));
        return;
      }
      if (u.pathname.startsWith('/api/')) {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ available: false, agents: [], rows: [], targets: [] }));
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

// CSS-source mutation gate: product must not reintroduce the T332 clip pattern.
function assertCssSourceGate(failures) {
  const html = fs.readFileSync(path.join(__dirname, '..', '..', 'web', 'index.html'), 'utf8');
  const idBlock = html.match(/#frontier-table\s+\.ft-id\s*\{[^}]*\}/);
  if (!idBlock) {
    failures.push('CSS: missing #frontier-table .ft-id rule');
    return;
  }
  const block = idBlock[0];
  if (/width:\s*7ch/.test(block) && /text-overflow:\s*ellipsis/.test(block)) {
    failures.push('CSS: 7ch+text-overflow:ellipsis pattern returned (T332 residual)');
  }
  if (/overflow:\s*hidden/.test(block) && /text-overflow:\s*ellipsis/.test(block)) {
    failures.push('CSS: overflow:hidden + text-overflow:ellipsis on .ft-id (clips hierarchical ids)');
  }
  if (/width:\s*([\d.]+)rem/.test(block)) {
    const m = block.match(/width:\s*([\d.]+)rem/);
    if (m && parseFloat(m[1]) >= 5.25) {
      failures.push('CSS: .ft-id width ≥5.25rem chasm pattern (T331)');
    }
  }
}

(async () => {
  const failures = [];
  assertCssSourceGate(failures);

  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  // Narrow-ish RHS: owner sees frontier in ~sidebar width, not full viewport.
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });

  try {
    await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.loadFrontier === 'function' || typeof loadFrontier === 'function',
      null,
      { timeout: 15000 }
    );

    // Ensure frontier tab is active; pin RHS/frontier to normal sidebar width
    // (owner rejects under ~320–380px chrome, not full viewport).
    await page.evaluate(() => {
      const rhs = document.getElementById('rhs') || document.querySelector('.rhs, #right, #sidebar');
      const bottom = document.getElementById('rhs-bottom');
      const body = document.getElementById('frontier-body');
      const pane = document.getElementById('frontier-pane');
      [rhs, bottom, body, pane].forEach((el) => {
        if (!el) return;
        el.style.minWidth = '0';
        el.style.width = '360px';
        el.style.maxWidth = '360px';
      });
      const tab = document.getElementById('rhs-tab-frontier');
      if (tab) tab.click();
      try {
        if (typeof loadFrontier === 'function') loadFrontier({ quiet: false });
        else if (window.loadFrontier) window.loadFrontier({ quiet: false });
      } catch (_) { /* isolated */ }
    });

    await page.waitForSelector('#frontier-table tr .ft-id', { timeout: 10000 });
    // Let fonts/layout settle.
    await page.waitForTimeout(150);

    const measure = await page.evaluate(() => {
      // Visible text ink. Range rects for overflow:hidden+ellipsis cells report
      // full unclipped text (negative gutters) — clamp to the content box so we
      // measure what the owner sees (ellipsis end), not layout-overflow ink.
      function textInkRect(el) {
        if (!el) return null;
        const cr = el.getBoundingClientRect();
        const cs = getComputedStyle(el);
        const pl = parseFloat(cs.paddingLeft) || 0;
        const pr = parseFloat(cs.paddingRight) || 0;
        const contentLeft = cr.left + pl;
        const contentRight = cr.right - pr;
        const clips =
          cs.overflow === 'hidden' ||
          cs.overflowX === 'hidden' ||
          cs.textOverflow === 'ellipsis';

        const range = document.createRange();
        const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, null);
        let tn = walker.nextNode();
        while (tn && !String(tn.textContent || '').trim()) tn = walker.nextNode();
        if (tn) range.selectNodeContents(tn);
        else range.selectNodeContents(el);
        const r = range.getBoundingClientRect();

        if (!(r.width > 0) || !(r.height > 0)) {
          // Empty fanout glyph / transparent cell: content-box edges.
          return {
            left: contentLeft,
            right: contentRight,
            top: cr.top,
            bottom: cr.bottom,
            width: Math.max(0, contentRight - contentLeft),
            height: cr.height,
            empty: true,
          };
        }
        let left = r.left;
        let right = r.right;
        if (clips) {
          left = Math.max(left, contentLeft);
          right = Math.min(right, contentRight);
        }
        return {
          left: left,
          right: right,
          top: r.top,
          bottom: r.bottom,
          width: Math.max(0, right - left),
          height: r.height,
          empty: false,
        };
      }

      const table = document.getElementById('frontier-table');
      if (!table) return { error: 'no #frontier-table' };
      const rows = [...table.querySelectorAll('tbody tr, tr')].filter((tr) =>
        tr.querySelector('.ft-id')
      );
      if (!rows.length) return { error: 'no data rows' };

      const idCells = [];
      const gutters = [];

      for (const tr of rows) {
        const id = tr.querySelector('.ft-id');
        const name = tr.querySelector('.ft-name');
        const status = tr.querySelector('.ft-status');
        const fan = tr.querySelector('.ft-fanout');
        if (!id || !name || !status || !fan) continue;

        const cs = getComputedStyle(id);
        const text = (id.textContent || '').trim();
        const cell = id.getBoundingClientRect();
        const ink = textInkRect(id);
        // Clip only when overflow hides overflow, or when scroll exceeds client
        // under a non-visible overflow. overflow:visible + ink past padding is OK
        // only if the column still sized so text is fully painted (scrollWidth≈client).
        const overflowHides =
          cs.overflow === 'hidden' ||
          cs.overflowX === 'hidden' ||
          cs.overflow === 'scroll' ||
          cs.overflowX === 'scroll';
        const clippedByOverflow =
          overflowHides && id.scrollWidth > id.clientWidth + 1;
        // Cell too narrow for its text (even with overflow:visible the column
        // failed to size — text paints over the next column).
        const cellTooNarrow =
          ink &&
          !ink.empty &&
          cell.width + 1 < ink.width + (parseFloat(cs.paddingLeft) || 0) +
            (parseFloat(cs.paddingRight) || 0) - 2;
        const inkClipped =
          (ink &&
            !ink.empty &&
            overflowHides &&
            (ink.right > cell.right + 1.5 || ink.left < cell.left - 1.5)) ||
          !!cellTooNarrow;

        idCells.push({
          text: text,
          textOverflow: cs.textOverflow,
          overflow: cs.overflow,
          overflowX: cs.overflowX,
          width: cs.width,
          maxWidth: cs.maxWidth,
          scrollWidth: id.scrollWidth,
          clientWidth: id.clientWidth,
          cellW: cell.width,
          inkW: ink ? ink.width : 0,
          inkRight: ink ? ink.right : 0,
          cellRight: cell.right,
          clippedByOverflow: clippedByOverflow,
          inkClipped: !!inkClipped,
          cellTooNarrow: !!cellTooNarrow,
          boxSizing: cs.boxSizing,
        });

        // Text-to-text gutters (ink right of left col → ink left of right col).
        // Prefer rows with non-empty fanout so status↔fan is a real text gap.
        const idInk = textInkRect(id);
        const nameInk = textInkRect(name);
        const stInk = textInkRect(status);
        const fanInk = textInkRect(fan);
        if (!idInk || !nameInk || !stInk || !fanInk) continue;
        if (idInk.empty || nameInk.empty || stInk.empty) continue;

        const gIdName = nameInk.left - idInk.right;
        const gNameSt = stInk.left - nameInk.right;
        const gStFan = fanInk.empty
          ? null
          : fanInk.left - stInk.right;

        gutters.push({
          id: text,
          idName: gIdName,
          nameStatus: gNameSt,
          statusFan: gStFan,
          fanEmpty: !!fanInk.empty,
        });
      }

      // CSS computed check on first id: no ellipsis clip policy.
      const firstId = rows[0] && rows[0].querySelector('.ft-id');
      const firstCs = firstId ? getComputedStyle(firstId) : null;

      return {
        rowCount: rows.length,
        idCells: idCells,
        gutters: gutters,
        firstIdPolicy: firstCs
          ? {
              textOverflow: firstCs.textOverflow,
              overflow: firstCs.overflow,
              overflowX: firstCs.overflowX,
            }
          : null,
      };
    });

    if (measure.error) {
      failures.push('measure: ' + measure.error);
    } else {
      // --- (1) Full hierarchical ids, no ellipsis after layout ---
      const need = ['T254.1', 'T262.3', 'T27.5'];
      for (const id of need) {
        const cell = (measure.idCells || []).find((c) => c.text.indexOf(id) >= 0);
        if (!cell) {
          failures.push('missing rendered id cell for ' + id);
          continue;
        }
        // Full id visible in text content (product prefixes 🎯).
        if (cell.text.indexOf(id) < 0) {
          failures.push('id text missing ' + id + ', got ' + JSON.stringify(cell.text));
        }
        if (cell.textOverflow === 'ellipsis') {
          failures.push(id + ': computed text-overflow is ellipsis (must not clip)');
        }
        if (cell.clippedByOverflow) {
          failures.push(
            id +
              ': scrollWidth (' +
              cell.scrollWidth +
              ') > clientWidth (' +
              cell.clientWidth +
              ') under overflow hidden — truncated'
          );
        }
        if (cell.inkClipped) {
          failures.push(
            id +
              ': text ink extends past cell box (inkRight=' +
              cell.inkRight.toFixed(1) +
              ' cellRight=' +
              cell.cellRight.toFixed(1) +
              ') — truncated after layout'
          );
        }
      }

      if (measure.firstIdPolicy && measure.firstIdPolicy.textOverflow === 'ellipsis') {
        failures.push('policy: .ft-id computed text-overflow:ellipsis');
      }

      // --- (2) Even TEXT-to-TEXT gutters (same order of magnitude) ---
      // Prefer hierarchical id rows with non-empty fanout (column width is set by
      // longest id; short T1 residual trailing space is expected in a table col).
      const hierarchical = (measure.gutters || []).filter(
        (g) => /\d+\.\d+/.test(g.id) && g.statusFan != null && !g.fanEmpty
      );
      const sample = hierarchical.length
        ? hierarchical
        : (measure.gutters || []).filter((g) => g.statusFan != null && !g.fanEmpty);
      if (!sample.length) {
        failures.push('no gutter samples measured (need hierarchical row with fanout)');
      } else {
        for (const g of sample) {
          const triples = [g.idName, g.nameStatus, g.statusFan];
          for (let i = 0; i < triples.length; i++) {
            if (!(triples[i] > 0)) {
              failures.push(
                g.id +
                  ': gutter[' +
                  i +
                  ']=' +
                  triples[i] +
                  ' not positive (glued columns?) idName=' +
                  g.idName +
                  ' nameStatus=' +
                  g.nameStatus +
                  ' statusFan=' +
                  g.statusFan
              );
            }
          }
          // Same order of magnitude: max/min ≤ 3 across pairs on a row.
          // Owner rejected ~50px vs ~3px; allow modest variance (padding + align).
          const positive = triples.filter((x) => typeof x === 'number' && x > 0);
          if (positive.length >= 2) {
            const mn = Math.min.apply(null, positive);
            const mx = Math.max.apply(null, positive);
            if (mn > 0 && mx / mn > 3) {
              failures.push(
                g.id +
                  ': uneven text-to-text gutters max/min=' +
                  (mx / mn).toFixed(2) +
                  ' (idName=' +
                  g.idName.toFixed(1) +
                  ' nameStatus=' +
                  g.nameStatus.toFixed(1) +
                  ' statusFan=' +
                  (g.statusFan != null ? g.statusFan.toFixed(1) : 'n/a') +
                  ') — need same order of magnitude'
              );
            }
            // Absolute band: not a T331-style 50px chasm and not <2px glue.
            for (const v of positive) {
              if (v < 2) {
                failures.push(g.id + ': gutter ' + v.toFixed(1) + 'px too tight (glue)');
              }
              if (v > 28) {
                failures.push(
                  g.id +
                    ': gutter ' +
                    v.toFixed(1) +
                    'px too wide (chasm / dead column space)'
                );
              }
            }
          }
        }
      }
    }

    // Artifact for owner/debug when headed or on failure.
    const shot = path.join(OUT_DIR, 't340-frontier-table.png');
    await page.locator('#frontier-body, #frontier-table').first().screenshot({ path: shot }).catch(() => {});
    fs.writeFileSync(
      path.join(OUT_DIR, 't340-frontier-table-measure.json'),
      JSON.stringify(measure, null, 2)
    );

    if (failures.length) {
      console.error('T340 frontier table layout FAIL:');
      failures.forEach((f) => console.error('  -', f));
      console.error('measure:', JSON.stringify(measure, null, 2));
      process.exitCode = 1;
    } else {
      console.log('ok  - T340 full hierarchical ids visible (no ellipsis after layout)');
      console.log('ok  - T340 text-to-text gutters same order of magnitude');
      console.log('ok  - T340 CSS rejects 7ch+ellipsis / rem chasm');
      console.log('All t340-frontier-table-layout tests passed');
    }
  } catch (e) {
    console.error('T340 layout test crashed:', e && e.stack ? e.stack : e);
    process.exitCode = 1;
  } finally {
    await browser.close().catch(() => {});
    srv.close();
  }
})();
