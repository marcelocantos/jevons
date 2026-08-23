// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright 1440×900 exact pixel match of the new UI against
// old-cockpit-1440x900.png. Clock frozen before first paint.

'use strict';

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const REPO = path.join(__dirname, '..', '..');
const GOLDEN = path.join(REPO, 'old-cockpit-1440x900.png');
const NEW_UI = process.env.JEVONS_NEW_UI_URL || 'http://127.0.0.1:5173/';
const OUT = process.argv[2] || path.join(__dirname, 'artifacts', 'new-ui-1440x900.png');
const LOG = process.argv[3] || path.join(__dirname, 'artifacts', 'pixel-match.log');
const FROZEN_NOW = 1_700_000_000_000;

function log(line) {
  fs.appendFileSync(LOG, line + '\n');
  console.log(line);
}

function pngSize(p) {
  const buf = fs.readFileSync(p);
  return { w: buf.readUInt32BE(16), h: buf.readUInt32BE(20) };
}

function comparePng(a, b, mask) {
  const py = `
import json, sys
import numpy as np
from PIL import Image
ga = np.array(Image.open(sys.argv[1]).convert('RGB'), dtype=np.uint8, copy=True)
gb = np.array(Image.open(sys.argv[2]).convert('RGB'), dtype=np.uint8, copy=True)
if ga.shape != gb.shape:
    print('size', ga.shape, gb.shape)
    sys.exit(2)
mask = json.loads(sys.argv[3]) if len(sys.argv) > 3 else None
if mask:
    x0 = max(0, int(mask.get('x', 0)))
    y0 = max(0, int(mask.get('y', 0)))
    x1 = min(ga.shape[1], x0 + int(mask.get('w', 0)))
    y1 = min(ga.shape[0], y0 + int(mask.get('h', 0)))
    if x1 > x0 and y1 > y0:
        ga[y0:y1, x0:x1] = gb[y0:y1, x0:x1]
        print('masked', x0, y0, x1, y1)
diff = np.any(ga != gb, axis=2)
n = int(diff.sum())
print('diff_pixels', n)
# Reconstructable geometry, not a caption: remaining ink by pane.
regions = {
    'status': (0, 0, 1440, 34),
    'chat': (0, 34, 982, 766),
    'composer': (0, 800, 982, 100),
    'agents': (982, 34, 458, 452),
    'table': (982, 486, 458, 414),
}
for name, (x, y, w, h) in regions.items():
    x1, y1 = min(diff.shape[1], x + w), min(diff.shape[0], y + h)
    print('region', name, int(diff[y:y1, x:x1].sum()))
sys.exit(0 if n == 0 else 1)
`;
  const args = ['-c', py, a, b];
  if (mask) args.push(JSON.stringify(mask));
  const r = spawnSync('python3', args, { encoding: 'utf8' });
  return { status: r.status, stdout: (r.stdout || '') + (r.stderr || '') };
}

async function main() {
  fs.mkdirSync(path.dirname(OUT), { recursive: true });
  fs.writeFileSync(LOG, '');
  const g = pngSize(GOLDEN);
  log('golden ' + GOLDEN + ' ' + g.w + 'x' + g.h);
  if (g.w !== 1440 || g.h !== 900) {
    log('FAIL golden is not 1440x900');
    process.exit(1);
  }

  const browser = await chromium.launch({ headless: true });
  log('browser bundled-chromium');
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 1,
    colorScheme: 'dark',
  });
  await context.addCookies([
    { name: 'theme', value: 'dark', url: NEW_UI.replace(/\/$/, '') + '/' },
  ]);
  await context.addInitScript((now) => {
    window.__JEVONS_CLOCK_NOW = now;
    window.__JEVONS_PIXEL_FIXTURE = true;
    try {
      document.documentElement.setAttribute('data-theme', 'dark');
    } catch (_) { /* document may not exist yet */ }
    // Golden was captured with a mouse-adjusted pane: divider at x=982 → 458px.
    // Same key as web/scripts/rhs_layout.js so a later same-origin cutover shares it.
    try {
      localStorage.setItem(
        'jevons-rhs-layout-v1',
        JSON.stringify({ sidebarWidth: 458, fleetFraction: 0.448 }),
      );
    } catch (_) { /* ignore */ }
  }, FROZEN_NOW);
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(String(e)));
  await page.goto(NEW_UI, { waitUntil: 'domcontentloaded', timeout: 30000 });
  await page.waitForSelector('#status', { timeout: 15000 });
  await page.evaluate(() => document.fonts && document.fonts.ready);
  await page.waitForFunction(
    () => document.fonts.check('14px Inter') && document.fonts.check('11px "JetBrains Mono"'),
    null,
    { timeout: 15000 },
  ).catch(() => {});
  await page.waitForTimeout(400);
  log('fonts ' + JSON.stringify(await page.evaluate(() => ({
    inter: document.fonts.check('14px Inter'),
    mono: document.fonts.check('11px "JetBrains Mono"'),
  }))));
  const input = page.locator('#input');
  if (await input.count()) await input.focus();
  const geom = await page.evaluate(() => {
    const m = document.getElementById('messages');
    return m
      ? { sh: m.scrollHeight, ch: m.clientHeight, st: m.scrollTop, pad: getComputedStyle(m).paddingTop }
      : null;
  });
  log('messages_geom ' + JSON.stringify(geom));
  const layout = await page.evaluate(() => {
    const msg = document.querySelector('#messages-canvas .msg, #messages .msg');
    const tr = document.querySelector('#frontier-table tr');
    const canvas = document.getElementById('messages-canvas');
    const messages = document.getElementById('messages');
    const box = (el) => {
      if (!el) return null;
      const r = el.getBoundingClientRect();
      const cs = getComputedStyle(el);
      return {
        x: r.x, y: r.y, w: r.width, h: r.height,
        font: cs.font, pad: cs.padding, mb: cs.marginBottom,
      };
    };
    const msgs = Array.from(document.querySelectorAll('#messages-canvas .msg, #messages .msg')).map((el) => {
      const r = el.getBoundingClientRect();
      return { y: r.y, h: r.height, w: r.width };
    });
    const markers = Array.from(document.querySelectorAll('#messages-canvas .turn-marker, #messages .turn-marker')).map((el) => {
      const r = el.getBoundingClientRect();
      return { y: r.y, h: r.height, t: el.textContent };
    });
    const rows = Array.from(document.querySelectorAll('#frontier-table tr')).map((el) => {
      const r = el.getBoundingClientRect();
      const id = el.querySelector('.ft-id');
      const name = el.querySelector('.ft-name');
      return {
        y: r.y, h: r.height,
        idw: id && id.getBoundingClientRect().width,
        namew: name && name.getBoundingClientRect().width,
        name: name && name.textContent,
      };
    });
    return {
      msg: box(msg),
      tr: box(tr),
      msgs,
      markers,
      rows,
      canvasW: canvas && canvas.clientWidth,
      messagesW: messages && messages.clientWidth,
      inputBar: (function () {
        const el = document.getElementById('input-bar');
        const inp = document.getElementById('input');
        const send = document.getElementById('send');
        const box = (e) => {
          if (!e) return null;
          const r = e.getBoundingClientRect();
          return { y: r.y, h: r.height, w: r.width };
        };
        return { bar: box(el), input: box(inp), send: box(send) };
      })(),
      badge: (function () {
        const el = document.querySelector('.model-badge');
        if (!el) return null;
        const r = el.getBoundingClientRect();
        return { w: r.width, html: el.innerHTML.replace(/\s+/g, ' ').slice(0, 120) };
      })(),
      bodyFont: getComputedStyle(document.body).font,
      p0: (function () {
        const body = msg && msg.querySelector('.msg-body');
        const p = body && body.querySelector('p');
        if (!p) return null;
        const r = p.getBoundingClientRect();
        const cs = getComputedStyle(p);
        return {
          y: r.y, h: r.height, mt: cs.marginTop, mb: cs.marginBottom,
          lh: cs.lineHeight, fs: cs.fontSize, ff: cs.fontFamily,
          html: p.innerHTML.slice(0, 80),
        };
      })(),
    };
  });
  log('layout ' + JSON.stringify(layout));
  const bars = await page.evaluate(() => {
    return Array.from(document.querySelectorAll('#plan-ticker .plan-win')).map((w) => {
      const fill = w.querySelector('.plan-bar-fill');
      const tri = w.querySelector('.plan-tri');
      const bar = w.querySelector('.plan-bar');
      const fr = fill ? fill.getBoundingClientRect() : null;
      const br = bar ? bar.getBoundingClientRect() : null;
      const tr = tri ? tri.getBoundingClientRect() : null;
      return {
        win: w.getAttribute('data-window'),
        prov: w.parentElement && w.parentElement.parentElement && w.parentElement.parentElement.getAttribute('data-provider'),
        cls: String(w.className || ''),
        fillW: fr && br ? Math.round(fr.width * 10) / 10 : null,
        barW: br ? Math.round(br.width * 10) / 10 : null,
        fillPct: fill && fill.style ? fill.style.width : '',
        triLeft: tri && tri.style ? tri.style.left : '',
        triX: tr ? Math.round(tr.left * 10) / 10 : null,
      };
    });
  });
  log('ticker_bars ' + JSON.stringify(bars));
  await page.addStyleTag({ content: '#input { caret-color: transparent !important; }' });
  const caret = await page.evaluate(() => {
    const el = document.getElementById('input');
    if (!el) return null;
    const r = el.getBoundingClientRect();
    const cs = getComputedStyle(el);
    const x = Math.round(r.left + (parseFloat(cs.paddingLeft) || 0));
    const y = Math.round(r.top + (parseFloat(cs.paddingTop) || 0));
    const h = Math.ceil(parseFloat(cs.lineHeight) || 20);
    return { x, y, w: 2, h };
  });
  log('caret_mask ' + JSON.stringify(caret));
  await page.screenshot({ path: OUT, scale: 'css', animations: 'disabled' });
  await browser.close();

  const a = pngSize(OUT);
  log('captured ' + OUT + ' ' + a.w + 'x' + a.h);
  log('page_errors ' + errors.length + (errors.length ? ' ' + errors.join('; ') : ''));
  if (a.w !== 1440 || a.h !== 900) {
    log('FAIL capture is not 1440x900');
    process.exit(1);
  }
  const cmp = comparePng(GOLDEN, OUT, caret);
  log(cmp.stdout.trim());
  if (cmp.status !== 0) {
    log('FAIL pixel mismatch vs old-cockpit-1440x900.png');
    process.exit(1);
  }
  log('PASS exact match');
}

main().catch((e) => {
  log('FAIL ' + (e && e.stack ? e.stack : e));
  process.exit(1);
});
