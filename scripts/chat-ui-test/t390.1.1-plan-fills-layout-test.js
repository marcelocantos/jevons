// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T390.1.1: REAL render of the two weekly waste fills.
//
// classifyPace assigns plan-under / plan-locked; this fixture loads the
// product cockpit.css and measures the painted fill colours. A class-name
// grep stays green while --plan-under and --plan-locked fail to reach the
// bar.
//
//   node scripts/chat-ui-test/t390.1.1-plan-fills-layout-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const os = require('os');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const OUT_DIR = fs.mkdtempSync(path.join(os.tmpdir(), 't390.1.1-fills-'));
const UI = path.join(__dirname, '..', '..', 'ui', 'src');

function fixtureHtml() {
  const css = fs.readFileSync(path.join(UI, 'cockpit.css'), 'utf8');
  return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>T390.1.1 plan fills</title>
<style>
${css}
#status { width: 720px; }
</style>
</head>
<body>
<div id="status">
  <span class="dot on"></span>
  <span id="status-text">connected</span>
  <div id="plan-ticker">
    <span class="plan-group" data-provider="grok">
      <span class="plan-icon"></span>
      <span class="plan-box">
        <span class="plan-win plan-under" data-pace="under" data-window="weekly">
          <span class="plan-track">
            <span class="plan-bar" aria-hidden="true">
              <span class="plan-bar-fill" style="width:42%"></span>
            </span>
          </span>
          <span class="plan-win-label">w</span>
        </span>
      </span>
    </span>
    <span class="plan-group" data-provider="codex">
      <span class="plan-icon"></span>
      <span class="plan-box">
        <span class="plan-win plan-locked" data-pace="locked" data-window="monthly">
          <span class="plan-track">
            <span class="plan-bar" aria-hidden="true">
              <span class="plan-bar-fill" style="width:64%"></span>
            </span>
          </span>
          <span class="plan-win-label">m</span>
        </span>
      </span>
    </span>
    <span class="plan-group" data-provider="claude">
      <span class="plan-icon"></span>
      <span class="plan-box">
        <span class="plan-win" data-window="session">
          <span class="plan-track">
            <span class="plan-bar" aria-hidden="true">
              <span class="plan-bar-fill" style="width:14%"></span>
            </span>
          </span>
          <span class="plan-win-label">s</span>
        </span>
      </span>
    </span>
  </div>
</div>
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
      resolve({ srv, base: `http://127.0.0.1:${srv.address().port}` });
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
  const page = await browser.newPage({ viewport: { width: 900, height: 200 } });
  try {
    await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
    const painted = await page.evaluate(() => {
      const root = getComputedStyle(document.documentElement);
      const underVar = root.getPropertyValue('--plan-under').trim();
      const lockedVar = root.getPropertyValue('--plan-locked').trim();
      const greenVar = root.getPropertyValue('--green').trim();
      const fills = {};
      for (const win of document.querySelectorAll('#plan-ticker .plan-win')) {
        const fill = win.querySelector('.plan-bar-fill');
        fills[win.getAttribute('data-window') + ':' + (win.getAttribute('data-pace') || '')] = {
          bg: fill ? getComputedStyle(fill).backgroundColor : '',
          className: win.className,
          width: fill ? fill.getBoundingClientRect().width : 0,
        };
      }
      return { underVar, lockedVar, greenVar, fills };
    });

    const underFill = painted.fills['weekly:under'];
    const lockedFill = painted.fills['monthly:locked'];
    const sessionFill = painted.fills['session:'];
    if (!underFill) failures.push('missing weekly under fill');
    if (!lockedFill) failures.push('missing weekly locked fill');
    if (!sessionFill) failures.push('missing session fill');

    const underWant = rgb(painted.underVar) || rgb(await page.evaluate((c) => {
      const el = document.createElement('span');
      el.style.color = c;
      document.body.appendChild(el);
      const out = getComputedStyle(el).color;
      el.remove();
      return out;
    }, painted.underVar));
    const lockedWant = rgb(painted.lockedVar) || rgb(await page.evaluate((c) => {
      const el = document.createElement('span');
      el.style.color = c;
      document.body.appendChild(el);
      const out = getComputedStyle(el).color;
      el.remove();
      return out;
    }, painted.lockedVar));
    const greenWant = rgb(painted.greenVar) || rgb(await page.evaluate((c) => {
      const el = document.createElement('span');
      el.style.color = c;
      document.body.appendChild(el);
      const out = getComputedStyle(el).color;
      el.remove();
      return out;
    }, painted.greenVar));

    const underGot = rgb(underFill && underFill.bg);
    const lockedGot = rgb(lockedFill && lockedFill.bg);
    const sessionGot = rgb(sessionFill && sessionFill.bg);

    if (far(underGot, underWant)) {
      failures.push('under fill bg=' + (underFill && underFill.bg) + ' want --plan-under ' + painted.underVar);
    }
    if (far(lockedGot, lockedWant)) {
      failures.push('locked fill bg=' + (lockedFill && lockedFill.bg) + ' want --plan-locked ' + painted.lockedVar);
    }
    if (sessionGot && underWant && !far(sessionGot, underWant)) {
      failures.push('session fill painted continuation-blue');
    }
    if (sessionGot && lockedWant && !far(sessionGot, lockedWant)) {
      failures.push('session fill painted locked-purple');
    }
    if (underGot && lockedGot && !far(underGot, lockedGot)) {
      failures.push('under and locked fills painted the same colour');
    }
    if (underFill && !(underFill.width > 0)) {
      failures.push('under fill has no width');
    }
    if (lockedFill && !(lockedFill.width > 0)) {
      failures.push('locked fill has no width');
    }

    const shot = path.join(OUT_DIR, 't390.1.1-plan-fills.png');
    await page.locator('#status').screenshot({ path: shot });

    if (failures.length) {
      console.error('FAIL T390.1.1 plan fills layout');
      failures.forEach((f) => console.error('  - ' + f));
      console.error('  screenshot: ' + shot);
      console.error('  painted: ' + JSON.stringify(painted));
      process.exitCode = 1;
    } else {
      console.log('ok - T390.1.1 plan fills layout');
      console.log('  screenshot: ' + shot);
      console.log('  under: ' + (underFill && underFill.bg) + ' locked: ' + (lockedFill && lockedFill.bg));
    }
  } catch (e) {
    console.error('FAIL T390.1.1 plan fills layout');
    console.error(e);
    process.exitCode = 1;
  } finally {
    await browser.close();
    srv.close();
  }
})();
