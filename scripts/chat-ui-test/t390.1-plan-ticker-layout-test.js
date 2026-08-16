// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T390.1: REAL render oracle for the plan-usage status bar.
//
// Source greps stay green while groups overlap, triangles sit at 0, or
// "cl/s 86%" text still paints instead of the T287 mark. This fixture
// loads the product CSS + plan_usage.js + model_prefix.js, paints a
// snapshot shaped like GET /api/plan-usage, and measures:
//
//   1) One group per publisher (claude, codex, grok). Idle bedrock absent.
//   2) Company marks, not "cl"/"cx"/"gk" text.
//   3) Claude's box holds session + weekly; each bar has a triangle.
//   4) Triangle left% matches remaining-time fraction (±2px).
//   5) Groups do not overlap; no "cl/s" / "unavailable" ink on the bar.
//
//   node scripts/chat-ui-test/t390.1-plan-ticker-layout-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const OUT_DIR = path.join(__dirname, 'artifacts');
fs.mkdirSync(OUT_DIR, { recursive: true });

const WEB = path.join(__dirname, '..', '..', 'web');
const NOW = Date.parse('2026-08-15T12:00:00Z');
const MINUTE = 60 * 1000;
const HOUR = 60 * MINUTE;

function iso(ms) { return new Date(ms).toISOString(); }

const SNAPSHOT = {
  at: iso(NOW),
  backends: [
    {
      provider: 'claude',
      status: 'available',
      plan_type: 'max',
      fleet_agents: 7,
      fetched_at: iso(NOW - 30 * 1000),
      age_seconds: 30,
      windows: [
        { name: 'session', remaining_percent: 86, used_percent: 14, resets_at: iso(NOW + 97 * MINUTE), limit_window_seconds: 5 * 3600 },
        { name: 'weekly', remaining_percent: 14, used_percent: 86, resets_at: iso(NOW + 52 * HOUR), limit_window_seconds: 7 * 24 * 3600 }
      ]
    },
    {
      provider: 'codex',
      status: 'available',
      fleet_agents: 0,
      fetched_at: iso(NOW),
      age_seconds: 0,
      windows: [
        { name: 'weekly', remaining_percent: 100, used_percent: 0, resets_at: iso(NOW + HOUR), limit_window_seconds: 7 * 24 * 3600 }
      ]
    },
    {
      provider: 'grok',
      status: 'available',
      fleet_agents: 3,
      fetched_at: iso(NOW),
      age_seconds: 0,
      windows: [
        { name: 'weekly', remaining_percent: 58, used_percent: 42, resets_at: iso(NOW + 3 * 24 * HOUR), limit_window_seconds: 7 * 24 * 3600 }
      ]
    },
    {
      provider: 'bedrock',
      status: 'unavailable',
      reason: 'AWS Bedrock does not publish Claude-style session/weekly subscription remaining',
      fleet_agents: 0,
      fetched_at: iso(NOW),
      age_seconds: 0
    }
  ]
};

function cssBlock(html, needle) {
  const start = html.indexOf(needle);
  if (start < 0) return '';
  const open = html.indexOf('{', start);
  let depth = 0;
  for (let i = open; i < html.length; i++) {
    if (html[i] === '{') depth++;
    else if (html[i] === '}') {
      depth--;
      if (depth === 0) return html.slice(start, i + 1);
    }
  }
  return '';
}

function extractPlanCss(html) {
  const vars = cssBlock(html, ':root, [data-theme="dark"]');
  const status = cssBlock(html, '#status {');
  const start = html.indexOf('#plan-ticker {');
  const theme = html.indexOf('/* Theme toggle');
  if (!vars) throw new Error('could not slice :root theme from index.html');
  if (start < 0 || theme < 0) throw new Error('could not slice #plan-ticker CSS from index.html');
  return [
    vars,
    '* { margin: 0; padding: 0; box-sizing: border-box; }',
    'html, body { background: var(--bg); color: var(--text); }',
    'body { font-family: var(--sans); }',
    status,
    html.slice(start, theme)
  ].join('\n');
}

function fixtureHtml() {
  const index = fs.readFileSync(path.join(WEB, 'index.html'), 'utf8');
  const css = extractPlanCss(index);
  return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>T390.1 plan ticker</title>
<style>
${css}
#status { width: 720px; }
</style>
</head>
<body>
<div id="status">
  <span class="dot on"></span>
  <span id="status-text">connected</span>
  <div id="plan-ticker"></div>
  <div id="theme-toggle"><button class="active">sys</button></div>
</div>
<script src="/scripts/model_prefix.js"></script>
<script src="/scripts/plan_usage.js"></script>
</body>
</html>`;
}

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  return 'application/octet-stream';
}

function startServer() {
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/' || u.pathname === '/index.html') {
        res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
        res.end(fixtureHtml());
        return;
      }
      const rel = u.pathname;
      const file = path.normalize(path.join(WEB, rel));
      if (!file.startsWith(WEB)) {
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
      resolve({ srv, base: `http://127.0.0.1:${srv.address().port}` });
    });
    srv.on('error', reject);
  });
}

(async () => {
  const failures = [];
  const { srv, base } = await startServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 900, height: 200 } });

  try {
    await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof PlanUsage === 'object' && typeof ModelPrefix === 'object', null, { timeout: 10000 });

    const painted = await page.evaluate(({ snap, now }) => {
      const el = document.getElementById('plan-ticker');
      const view = PlanUsage.formatPlanUsage(snap, now);
      PlanUsage.paintPlanUsage(el, view);
      return {
        text: (el.textContent || '').replace(/\s+/g, ' ').trim(),
        providers: [...el.querySelectorAll('.plan-group')].map((g) => g.getAttribute('data-provider')),
        companies: [...el.querySelectorAll('.plan-group')].map((g) => g.getAttribute('data-company')),
        marks: [...el.querySelectorAll('.plan-icon .model-icon')].map((s) => s.getAttribute('data-mark')),
        view: {
          groups: view.groups.map((g) => ({
            provider: g.provider,
            available: g.available,
            windows: g.windows.map((w) => ({
              name: w.name,
              remainingPercent: w.remainingPercent,
              remainingTimePercent: w.remainingTimePercent,
              pace: w.pace
            }))
          }))
        }
      };
    }, { snap: SNAPSHOT, now: NOW });

    if (painted.providers.join(',') !== 'claude,codex,grok') {
      failures.push('groups: want claude,codex,grok got ' + painted.providers.join(','));
    }
    if (painted.providers.indexOf('bedrock') >= 0) {
      failures.push('idle bedrock must not occupy the bar');
    }
    if (painted.marks.indexOf('claude-splat') < 0) failures.push('missing Claude splat');
    if (painted.marks.indexOf('openai') < 0) failures.push('missing OpenAI mark');
    if (painted.marks.indexOf('grok') < 0) failures.push('missing Grok mark');
    if (/cl\/s|cx\/w|gk\/w|unavailable/i.test(painted.text)) {
      failures.push('bar still shows abbrev/unavailable text: ' + JSON.stringify(painted.text));
    }

    const layout = await page.evaluate(() => {
      const el = document.getElementById('plan-ticker');
      const groups = [...el.querySelectorAll('.plan-group')];
      const out = { groups: [], overlaps: [] };
      let prev = null;
      for (const g of groups) {
        const r = g.getBoundingClientRect();
        if (prev && r.left < prev.right - 0.5) {
          out.overlaps.push({ a: prev.provider, b: g.getAttribute('data-provider') });
        }
        const wins = [...g.querySelectorAll('.plan-win')].map((w) => {
          const track = w.querySelector('.plan-track');
          const bar = w.querySelector('.plan-bar');
          const fill = w.querySelector('.plan-bar-fill');
          const tri = w.querySelector('.plan-tri');
          const tr = track ? track.getBoundingClientRect() : null;
          const br = bar ? bar.getBoundingClientRect() : null;
          const fr = fill ? fill.getBoundingClientRect() : null;
          const styleLeft = tri ? parseFloat(tri.style.left) : null;
          const cs = bar ? getComputedStyle(bar) : null;
          const bL = cs ? parseFloat(cs.borderLeftWidth) || 0 : 0;
          const bR = cs ? parseFloat(cs.borderRightWidth) || 0 : 0;
          const railLeft = br ? br.left + bL : null;
          const railWidth = br ? br.width - bL - bR : 0;
          let triCenter = null;
          if (tri && railLeft != null && railWidth > 0 && Number.isFinite(styleLeft)) {
            triCenter = railLeft + (styleLeft / 100) * railWidth;
          }
          const fillFrac = (fr && railWidth > 0) ? (fr.right - railLeft) / railWidth * 100 : null;
          const triFrac = (triCenter != null && railWidth > 0) ? (triCenter - railLeft) / railWidth * 100 : null;
          return {
            window: w.getAttribute('data-window'),
            pace: w.getAttribute('data-pace'),
            label: (w.querySelector('.plan-win-label') || {}).textContent || '',
            fillWidth: fr ? fr.width : 0,
            trackWidth: tr ? tr.width : 0,
            railWidth: railWidth,
            styleLeft: styleLeft,
            triCenter: triCenter,
            fillRight: fr ? fr.right : null,
            railLeft: railLeft,
            fillFrac: fillFrac,
            triFrac: triFrac
          };
        });
        const icon = g.querySelector('.plan-icon .model-icon');
        const box = g.querySelector('.plan-box');
        const ir = icon ? icon.getBoundingClientRect() : null;
        const br = box ? box.getBoundingClientRect() : null;
        out.groups.push({
          provider: g.getAttribute('data-provider'),
          company: g.getAttribute('data-company'),
          left: r.left,
          right: r.right,
          width: r.width,
          height: r.height,
          hasBox: !!box,
          iconWidth: ir ? ir.width : 0,
          iconHeight: ir ? ir.height : 0,
          iconMid: ir ? ir.top + ir.height / 2 : null,
          boxMid: br ? br.top + br.height / 2 : null,
          windows: wins
        });
        prev = { right: r.right, provider: g.getAttribute('data-provider') };
      }
      return out;
    });

    if (layout.overlaps.length) {
      failures.push('groups overlap: ' + JSON.stringify(layout.overlaps));
    }

    for (const g of layout.groups) {
      if (g.iconWidth < 16 || g.iconHeight < 16) {
        failures.push(g.provider + ' icon too small: ' + g.iconWidth + 'x' + g.iconHeight + ' (want ≥16, 50% up from 11)');
      }
      if (g.iconMid != null && g.boxMid != null && Math.abs(g.iconMid - g.boxMid) > 1.5) {
        failures.push(g.provider + ' icon not vertically centred on box: iconMid=' + g.iconMid.toFixed(1) + ' boxMid=' + g.boxMid.toFixed(1));
      }
    }

    for (const g of layout.groups) {
      const vg = painted.view.groups.find((x) => x.provider === g.provider);
      if (!vg) continue;
      for (const win of g.windows) {
        const vw = vg.windows.find((x) => x.name === win.window);
        if (!vw) continue;
        if (typeof vw.remainingPercent === 'number' && typeof win.fillFrac === 'number') {
          if (Math.abs(win.fillFrac - vw.remainingPercent) > 2) {
            failures.push(g.provider + ' ' + win.window + ' fillFrac ' + win.fillFrac.toFixed(1) + ' != rem ' + vw.remainingPercent);
          }
        }
        if (typeof vw.remainingTimePercent === 'number' && typeof win.triFrac === 'number') {
          if (Math.abs(win.triFrac - vw.remainingTimePercent) > 2) {
            failures.push(g.provider + ' ' + win.window + ' triFrac ' + win.triFrac.toFixed(1) + ' != time ' + vw.remainingTimePercent.toFixed(1));
          }
        }
        // Visual order must match the numbers: leftover (fill past tip)
        // only when remaining > remaining-time. 1px slack for subpixel.
        if (win.fillRight != null && win.triCenter != null &&
            typeof vw.remainingPercent === 'number' && typeof vw.remainingTimePercent === 'number') {
          const fillPast = win.fillRight - win.triCenter;
          const leftover = vw.remainingPercent - vw.remainingTimePercent;
          if (leftover > 1 && fillPast < -1) {
            failures.push(g.provider + ' ' + win.window + ' fill should sit right of tip (leftover ' + leftover.toFixed(1) + 'pp) but fillPast=' + fillPast.toFixed(2));
          }
          if (leftover < -1 && fillPast > 1) {
            failures.push(g.provider + ' ' + win.window + ' fill should sit left of tip (overspend ' + leftover.toFixed(1) + 'pp) but fillPast=' + fillPast.toFixed(2) + ' — geometry/colour mismatch');
          }
        }
      }
    }

    const claude = layout.groups.find((g) => g.provider === 'claude');
    if (!claude) {
      failures.push('no claude group in layout');
    } else {
      if (!claude.hasBox) failures.push('claude missing window box');
      if (claude.windows.length !== 2) failures.push('claude should have session+weekly, got ' + claude.windows.length);
      const sess = claude.windows[0];
      const week = claude.windows[1];
      if (sess && sess.label.toLowerCase() !== 's') failures.push('session label want s got ' + sess.label);
      if (week && week.label.toLowerCase() !== 'w') failures.push('weekly label want w got ' + week.label);
      if (sess && sess.trackWidth < 20) failures.push('session track too narrow: ' + sess.trackWidth);
      if (sess && !(sess.fillWidth > 0)) failures.push('session fill has no width');

      const viewCl = painted.view.groups.find((g) => g.provider === 'claude');
      const wantS = viewCl && viewCl.windows[0] && viewCl.windows[0].remainingTimePercent;
      if (sess && typeof wantS === 'number' && typeof sess.styleLeft === 'number') {
        if (Math.abs(sess.styleLeft - wantS) > 0.6) {
          failures.push('session triangle style.left ' + sess.styleLeft + ' != remainingTime ' + wantS);
        }
      } else {
        failures.push('session triangle missing');
      }
    }

    const grok = layout.groups.find((g) => g.provider === 'grok');
    if (!grok || !grok.hasBox || grok.windows.length !== 1) {
      failures.push('grok must paint as a real weekly bar, got ' + JSON.stringify(grok));
    }

    const claudeSess = claude && claude.windows.find((w) => w.window === 'session');
    if (claudeSess && (claudeSess.pace === 'under' || claudeSess.pace === 'locked')) {
      failures.push('session must not paint weekly waste, pace=' + claudeSess.pace);
    }
    const grokWin = grok && grok.windows[0];
    if (grokWin && grokWin.pace !== 'under') {
      failures.push('fixture grok weekly should be continuation-blue, pace=' + (grokWin && grokWin.pace));
    }
    const cx = layout.groups.find((g) => g.provider === 'codex');
    const cxWin = cx && cx.windows[0];
    if (cxWin && cxWin.pace !== 'locked') {
      failures.push('fixture codex weekly (1h left, unused) should be locked-purple, pace=' + (cxWin && cxWin.pace));
    }

    const shot = path.join(OUT_DIR, 't390.1-plan-ticker.png');
    await page.locator('#status').screenshot({ path: shot });

    if (failures.length) {
      console.error('FAIL T390.1 plan ticker layout');
      failures.forEach((f) => console.error('  - ' + f));
      console.error('  screenshot: ' + shot);
      console.error('  painted: ' + JSON.stringify(painted, null, 2));
      console.error('  layout: ' + JSON.stringify(layout, null, 2));
      process.exitCode = 1;
    } else {
      console.log('ok - T390.1 plan ticker layout');
      console.log('  screenshot: ' + shot);
      console.log('  groups: ' + painted.providers.join(', '));
      console.log('  marks: ' + painted.marks.join(', '));
    }
  } catch (e) {
    console.error('FAIL T390.1 plan ticker layout');
    console.error(e);
    process.exitCode = 1;
  } finally {
    await browser.close();
    srv.close();
  }
})();
