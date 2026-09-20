// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T390.1.2: REAL render of HSV-lerped plan-usage fills.
//
// paceColor is the source of truth (inline background, not a class hop).
// A snap-to-named-class mutant keeps mid-ahead at --amber or --red and
// mid-under at --green or --plan-under.
//
//   node scripts/chat-ui-test/t390.1.2-pace-color-layout-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawnSync } = require('child_process');

const repoRoot = path.join(__dirname, '..', '..');
const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const OUT_DIR = fs.mkdtempSync(path.join(os.tmpdir(), 't390.1.2-pace-color-'));
const UI = path.join(repoRoot, 'ui', 'src');

function bundlePace() {
  const outFile = path.join(OUT_DIR, 'pace.js');
  const rolldown = path.join(repoRoot, 'ui', 'node_modules', '.bin', 'rolldown');
  const r = spawnSync(
    rolldown,
    [
      path.join(repoRoot, 'ui', 'src', 'plan', 'pace.ts'),
      '-f', 'iife',
      '-n', 'Pace',
      '-o', outFile,
      '--platform', 'browser',
    ],
    { encoding: 'utf8', cwd: path.join(repoRoot, 'ui') },
  );
  if (r.status !== 0) {
    throw new Error('rolldown pace.ts failed: ' + (r.stderr || r.stdout || r.status));
  }
  return fs.readFileSync(outFile, 'utf8');
}

const paceJs = bundlePace();

function fixtureHtml() {
  const css = fs.readFileSync(path.join(UI, 'cockpit.css'), 'utf8');
  return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>T390.1.2 pace color</title>
<style>
${css}
#status { width: 920px; }
</style>
</head>
<body>
<div id="status">
  <span class="dot on"></span>
  <span id="status-text">connected</span>
  <div id="plan-ticker">
    <span class="plan-group" data-provider="claude">
      <span class="plan-icon"></span>
      <span class="plan-box">
        <span class="plan-win" data-window="a" data-sample="A">
          <span class="plan-track"><span class="plan-bar"><span class="plan-bar-fill" style="width:50%"></span></span></span>
          <span class="plan-win-label">A</span>
        </span>
        <span class="plan-win" data-window="mid-ab" data-sample="midAB">
          <span class="plan-track"><span class="plan-bar"><span class="plan-bar-fill" style="width:55%"></span></span></span>
          <span class="plan-win-label">ab</span>
        </span>
        <span class="plan-win" data-window="b" data-sample="B">
          <span class="plan-track"><span class="plan-bar"><span class="plan-bar-fill" style="width:62%"></span></span></span>
          <span class="plan-win-label">B</span>
        </span>
        <span class="plan-win" data-window="mid-ahead" data-sample="midBC">
          <span class="plan-track"><span class="plan-bar"><span class="plan-bar-fill" style="width:70%"></span></span></span>
          <span class="plan-win-label">ah</span>
        </span>
        <span class="plan-win" data-window="c" data-sample="C">
          <span class="plan-track"><span class="plan-bar"><span class="plan-bar-fill" style="width:80%"></span></span></span>
          <span class="plan-win-label">C</span>
        </span>
      </span>
    </span>
    <span class="plan-group" data-provider="codex">
      <span class="plan-icon"></span>
      <span class="plan-box">
        <span class="plan-win" data-window="mid-under" data-sample="midUnder">
          <span class="plan-track"><span class="plan-bar"><span class="plan-bar-fill" style="width:46%"></span></span></span>
          <span class="plan-win-label">w</span>
        </span>
        <span class="plan-win" data-window="b-prime" data-sample="Bp">
          <span class="plan-track"><span class="plan-bar"><span class="plan-bar-fill" style="width:42%"></span></span></span>
          <span class="plan-win-label">u</span>
        </span>
        <span class="plan-win" data-window="c-prime" data-sample="Cp">
          <span class="plan-track"><span class="plan-bar"><span class="plan-bar-fill" style="width:64%"></span></span></span>
          <span class="plan-win-label">l</span>
        </span>
      </span>
    </span>
  </div>
</div>
<script>${paceJs}</script>
<script>
(function () {
  var stops = Pace.overspendStops();
  var under = Pace.PACE_UNDER_WASTE;
  var locked = Pace.PACE_LOCKED_WASTE;
  var colors = {
    A: Pace.paceColor(stops.a),
    midAB: Pace.paceColor((stops.a + stops.b) / 2),
    B: Pace.paceColor(stops.b),
    midBC: Pace.paceColor((stops.b + stops.c) / 2),
    C: Pace.paceColor(stops.c),
    midUnder: Pace.paceColor(0.5, { continuation: under / 2, locked: 0 }),
    Bp: Pace.paceColor(0.5, { continuation: under, locked: 0 }),
    Cp: Pace.paceColor(0.2, { continuation: under, locked: locked }),
  };
  document.querySelectorAll('#plan-ticker .plan-win').forEach(function (win) {
    var key = win.getAttribute('data-sample');
    var fill = win.querySelector('.plan-bar-fill');
    if (key && colors[key] && fill) fill.style.background = colors[key];
  });
})();
</script>
</body>
</html>`;
}

function startServer() {
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      res.end(fixtureHtml());
    });
    srv.listen(0, '127.0.0.1', () => {
      resolve({ srv, base: 'http://127.0.0.1:' + srv.address().port });
    });
    srv.on('error', reject);
  });
}

function rgb(s) {
  const m = /^rgba?\((\d+),\s*(\d+),\s*(\d+)/.exec(s || '');
  if (!m) return null;
  return [Number(m[1]), Number(m[2]), Number(m[3])];
}

function far(a, b) {
  if (!a || !b) return true;
  return Math.abs(a[0] - b[0]) + Math.abs(a[1] - b[1]) + Math.abs(a[2] - b[2]) > 30;
}

(async () => {
  const failures = [];
  const { srv, base } = await startServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1000, height: 220 } });
  try {
    await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
    const painted = await page.evaluate(() => {
      const root = getComputedStyle(document.documentElement);
      const tokens = {
        green: root.getPropertyValue('--green').trim(),
        amber: root.getPropertyValue('--amber').trim(),
        red: root.getPropertyValue('--red').trim(),
        under: root.getPropertyValue('--plan-under').trim(),
        locked: root.getPropertyValue('--plan-locked').trim(),
      };
      const fills = {};
      for (const win of document.querySelectorAll('#plan-ticker .plan-win')) {
        const fill = win.querySelector('.plan-bar-fill');
        fills[win.getAttribute('data-sample')] = fill ? getComputedStyle(fill).backgroundColor : '';
      }
      return { tokens, fills };
    });

    function tokenRgb(cssColor) {
      return rgb(cssColor) || null;
    }

    const resolve = async (cssColor) => {
      const direct = tokenRgb(cssColor);
      if (direct) return direct;
      return rgb(await page.evaluate((c) => {
        const el = document.createElement('span');
        el.style.color = c;
        document.body.appendChild(el);
        const out = getComputedStyle(el).color;
        el.remove();
        return out;
      }, cssColor));
    };

    const green = await resolve(painted.tokens.green);
    const amber = await resolve(painted.tokens.amber);
    const red = await resolve(painted.tokens.red);
    const under = await resolve(painted.tokens.under);
    const locked = await resolve(painted.tokens.locked);

    const A = rgb(painted.fills.A);
    const midAB = rgb(painted.fills.midAB);
    const B = rgb(painted.fills.B);
    const midBC = rgb(painted.fills.midBC);
    const C = rgb(painted.fills.C);
    const midUnder = rgb(painted.fills.midUnder);
    const Bp = rgb(painted.fills.Bp);
    const Cp = rgb(painted.fills.Cp);

    if (far(A, green)) failures.push('A fill=' + painted.fills.A + ' want --green ' + painted.tokens.green);
    if (far(B, amber)) failures.push('B fill=' + painted.fills.B + ' want --amber ' + painted.tokens.amber);
    if (far(C, red)) failures.push('C fill=' + painted.fills.C + ' want --red ' + painted.tokens.red);
    if (far(Bp, under)) failures.push("B' fill=" + painted.fills.Bp + ' want --plan-under ' + painted.tokens.under);
    if (far(Cp, locked)) failures.push("C' fill=" + painted.fills.Cp + ' want --plan-locked ' + painted.tokens.locked);

    if (!far(midAB, green) || !far(midAB, amber)) {
      failures.push('mid(A,B) snapped to a named stop: ' + painted.fills.midAB);
    }
    if (!far(midBC, amber) || !far(midBC, red)) {
      failures.push('mid-ahead snapped to orange or red: ' + painted.fills.midBC);
    }
    if (!far(midUnder, green) || !far(midUnder, under)) {
      failures.push('mid-under snapped to green or blue: ' + painted.fills.midUnder);
    }

    const shot = path.join(OUT_DIR, 't390.1.2-pace-color.png');
    await page.locator('#status').screenshot({ path: shot });

    if (failures.length) {
      console.error('FAIL T390.1.2 pace color layout');
      failures.forEach((f) => console.error('  - ' + f));
      console.error('  screenshot: ' + shot);
      console.error('  painted: ' + JSON.stringify(painted));
      process.exitCode = 1;
    } else {
      console.log('ok - T390.1.2 pace color layout');
      console.log('  screenshot: ' + shot);
      console.log('  mid-ahead: ' + painted.fills.midBC + ' mid-under: ' + painted.fills.midUnder);
    }
  } catch (e) {
    console.error('FAIL T390.1.2 pace color layout');
    console.error(e);
    process.exitCode = 1;
  } finally {
    await browser.close();
    srv.close();
  }
})();
