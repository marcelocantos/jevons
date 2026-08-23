// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Phase-1 oracle: dump chrome DOM+computed layout from old :13705 and new
// :5173 at 1440×900, then print a structured diff.
//
// Owner caveats (keep these load-bearing):
// 1. React is not required to be an exact element/attribute clone. A very
//    small NAMED list of readily explicable exceptions is the bar. Anything
//    unnamed is a fail.
// 2. Do NOT match sidebar transcript DOM trees. Compact AgentInteraction
//    (#agent-inspect) is a density parameter, not a port of inspect/virt-shell.
//    Never add those selectors. #rhs-bottom is pane chrome only (box, not kids).
// 3. Usage bars ARE required matching. Tree + company-mark SVG must match;
//    only plan-bar-fill width % and plan-tri left % may differ (pace class
//    follows those numbers). PNG still masks ticker fills.
//
// Semantic layer first: computed CSS (including ::placeholder) on chrome
// and transcript *styles*. Journal vs fixture text/used boxes are skipped.
// Pixel PNG compare is phase 2, after this oracle is green.

'use strict';

const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const OLD_UI = process.env.JEVONS_OLD_UI_URL || 'http://127.0.0.1:13705/';
const NEW_UI = process.env.JEVONS_NEW_UI_URL || 'http://127.0.0.1:5173/';
const OUT = process.argv[2] || path.join(__dirname, 'artifacts', 'dom-census.json');
const FROZEN_NOW = 1_700_000_000_000;

const { compareTickers } = require('./plan_ticker_shape');
const SHAPE_SRC = fs.readFileSync(path.join(__dirname, 'plan_ticker_shape.js'), 'utf8')
  .replace(/module\.exports[\s\S]*$/, '') + '\nwindow.dumpTicker = dumpTicker;\n';

// Named exceptions stay short and explicit (caveat 1). Content/state only —
// not CSS forks. Do not grow this map to hide an unexplained style miss.
const NAMED = {
  '.ft-id': '🎯 id column width follows live vs fixture target ids',
  '.ft-name': 'name column is the remainder after .ft-id',
  '.model-badge': 'badge width follows painted model subscript',
  '.agent-node': 'fixture has no provider-band !; live overseer may be hot',
  '#frontier-table tr': 'golden raster rows are 26px; live daily rows are 28.5px',
  '.ft-play-btn': 'play control sized to the 26px golden row',
  '#send': 'disabled opacity follows seed-only send enablement; live widget leaves it on',
};

// Journal vs fixture text only. Transcript CSS is in the compare.
const SKIP = new Set([
  '.msg innerHTML',
  // React composer is a real empty field (placeholder). Vanilla still seeds Wispr.
  '#input seedOnly',
  '#input paintsPlaceholder',
]);

// Caveat 2: compare the RHS bottom pane as a box, never its transcript tree.
const SKIP_TREE = new Set(['#rhs-bottom', '#messages-canvas', 'body', '.msg', '.msg-body', '.turn-marker']);

// Used boxes follow journal vs fixture text. Still compare padding/font/etc.
const SKIP_RECT = new Set([
  '.msg', '.msg-body', '.msg-time', '.turn-marker',
  '#messages-canvas', '#frontier-table', '#frontier-table tr',
  '.ft-id', '.ft-name', '.ft-play-btn', '.model-badge', '.agent-node',
  '#plan-ticker',
]);
const SKIP_USED_SIZE = SKIP_RECT;
const SKIP_CLS = new Set(['.msg', '.turn-marker', '#input']);

const SELECTORS = [
  'html', 'body',
  '#status', '#dot', '#status-text', '#plan-ticker', '#theme-toggle',
  '#main', '#chat-pane', '#messages', '#messages-canvas',
  '#input-bar', '#input', '#send',
  '#activity-pane', '#rhs-width-handle', '#activity-header',
  '#agents', '#rhs-split', '#rhs-split-handle',
  '#rhs-bottom', '#rhs-bottom-tabs', '#rhs-tab-frontier',
  '#frontier-pane', '#frontier-toolbar', '#frontier-body', '#frontier-table',
  '#workers',
];

const STYLE_KEYS = [
  'display', 'position', 'boxSizing', 'font', 'fontSize', 'fontWeight',
  'fontFamily', 'lineHeight', 'letterSpacing', 'fontKerning', 'fontVariant',
  'fontFeatureSettings', 'textRendering', 'webkitFontSmoothing',
  'padding', 'margin', 'border', 'borderRadius', 'boxShadow', 'outline',
  'width', 'height', 'maxWidth', 'minWidth', 'minHeight', 'maxHeight',
  'overflow', 'overflowX', 'overflowY', 'overflowWrap', 'overflowAnchor',
  'wordBreak', 'textOverflow', 'textTransform', 'textAlign',
  'flex', 'flexDirection', 'alignItems', 'justifyContent', 'alignSelf', 'gap',
  'backgroundColor', 'backgroundImage', 'color', 'caretColor', 'whiteSpace',
  'verticalAlign', 'transform', 'opacity', 'filter', 'appearance', 'resize',
  'scrollbarGutter', 'scrollbarWidth', 'zIndex',
];

const PSEUDOS = [
  { sel: '#input', pseudo: '::placeholder' },
  { sel: '#messages', pseudo: '::-webkit-scrollbar' },
  { sel: '#messages', pseudo: '::-webkit-scrollbar-thumb' },
];

async function dump(page) {
  return page.evaluate(({ selectors, styleKeys, pseudos }) => {
    function rect(el) {
      const r = el.getBoundingClientRect();
      return {
        x: Math.round(r.x * 100) / 100,
        y: Math.round(r.y * 100) / 100,
        w: Math.round(r.width * 100) / 100,
        h: Math.round(r.height * 100) / 100,
      };
    }
    function styles(el, pseudo) {
      const cs = pseudo ? getComputedStyle(el, pseudo) : getComputedStyle(el);
      const o = {};
      for (const k of styleKeys) o[k] = cs[k];
      if (pseudo) o.content = cs.content;
      return o;
    }
    function node(el) {
      if (!el) return null;
      const kids = Array.from(el.children).map((c) => ({
        tag: c.tagName.toLowerCase(),
        id: c.id || '',
        cls: (c.className && typeof c.className === 'string') ? c.className.trim().slice(0, 80) : '',
      }));
      return {
        tag: el.tagName.toLowerCase(),
        id: el.id || '',
        cls: (el.className && typeof el.className === 'string') ? el.className.trim().slice(0, 120) : '',
        childCount: el.children.length,
        kids,
        text: (el.innerText || '').replace(/\s+/g, ' ').trim().slice(0, 80),
        rect: rect(el),
        style: styles(el),
      };
    }
    const chrome = {};
    for (const sel of selectors) chrome[sel] = node(document.querySelector(sel));
    for (const p of (pseudos || [])) {
      const el = document.querySelector(p.sel);
      chrome[p.sel + p.pseudo] = el ? { style: styles(el, p.pseudo) } : null;
    }
    const input = document.getElementById('input');
    chrome['#input placeholder'] = input ? (input.getAttribute('placeholder') || '') : null;
    chrome['#input paintsPlaceholder'] = input ? (input.value === '') : null;
    chrome['#input seedOnly'] = input ? input.classList.contains('composer-seed-only') : null;
    const canvas = document.getElementById('messages-canvas') || document.getElementById('messages') || document.body;
    const probe = document.createElement('div');
    probe.className = 'msg jevons';
    probe.setAttribute('data-census-probe', '1');
    probe.style.cssText = 'position:absolute;visibility:hidden;pointer-events:none;left:0;top:0;';
    probe.innerHTML = '<div class="msg-body"></div><div class="msg-time"></div>';
    canvas.appendChild(probe);
    const msg = probe;
    const liveMsg = document.querySelector('#messages-canvas > .msg.jevons:not(.virt-shell):not([data-census-probe]), .msg.jevons:not(.virt-shell)');
    const marker = document.createElement('div');
    marker.className = 'turn-marker';
    marker.setAttribute('data-census-probe', '1');
    marker.style.cssText = 'position:absolute;visibility:hidden;pointer-events:none;';
    canvas.appendChild(marker);
    const tr = document.querySelector('#frontier-table tr');
    const badge = document.querySelector('.model-badge');
    const tree = document.querySelector('.agent-tree, #agents .agent-node, .agent-node');
    chrome['.msg'] = node(msg);
    chrome['.msg-body'] = msg ? node(msg.querySelector('.msg-body')) : null;
    chrome['.msg-time'] = msg ? node(msg.querySelector('.msg-time')) : null;
    chrome['.turn-marker'] = node(marker);
    chrome['#frontier-table tr'] = node(tr);
    chrome['.ft-id'] = tr ? node(tr.querySelector('.ft-id')) : null;
    chrome['.ft-name'] = tr ? node(tr.querySelector('.ft-name')) : null;
    chrome['.ft-play-btn'] = tr ? node(tr.querySelector('.ft-play-btn')) : null;
    chrome['.model-badge'] = node(badge);
    chrome['.agent-node'] = node(tree);
    chrome['.msg innerHTML'] = liveMsg && liveMsg.querySelector('.msg-body')
      ? liveMsg.querySelector('.msg-body').innerHTML.slice(0, 240)
      : null;
    if (probe.parentNode) probe.parentNode.removeChild(probe);
    if (marker.parentNode) marker.parentNode.removeChild(marker);
    chrome['html.theme'] = document.documentElement.getAttribute('data-theme');
    chrome.planTicker = (typeof window.dumpTicker === 'function')
      ? window.dumpTicker(document.getElementById('plan-ticker'))
      : null;
    chrome['ancestry.messages'] = (function () {
      const el = document.getElementById('messages');
      if (!el) return [];
      const ids = [];
      let n = el;
      while (n && n !== document.documentElement) {
        ids.push((n.id ? '#' + n.id : n.tagName.toLowerCase()) + (n.className && typeof n.className === 'string' && n.className.trim() ? '.' + n.className.trim().split(/\s+/)[0] : ''));
        n = n.parentElement;
      }
      return ids;
    })();
    return chrome;
  }, { selectors: SELECTORS, styleKeys: STYLE_KEYS, pseudos: PSEUDOS });
}

async function open(url, fixture) {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 1,
    colorScheme: 'dark',
  });
  await context.addCookies([{ name: 'theme', value: 'dark', url: url.replace(/\/$/, '') + '/' }]);
  await context.addInitScript(SHAPE_SRC);
  await context.addInitScript((opts) => {
    window.__JEVONS_CLOCK_NOW = opts.now;
    if (opts.fixture) window.__JEVONS_PIXEL_FIXTURE = true;
    try { document.documentElement.setAttribute('data-theme', 'dark'); } catch (_) { /* ignore */ }
    try {
      localStorage.setItem('jevons-rhs-layout-v1', JSON.stringify({ sidebarWidth: 458, fleetFraction: 0.448 }));
    } catch (_) { /* ignore */ }
  }, { now: FROZEN_NOW, fixture: fixture });
  const page = await context.newPage();
  await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30000 });
  await page.waitForSelector('#status', { timeout: 15000 });
  await page.evaluate(() => document.fonts && document.fonts.ready);
  await page.waitForTimeout(800);
  await page.waitForSelector('#plan-ticker .plan-group, #plan-ticker .plan-chip', { timeout: 15000 }).catch(() => {});
  const input = page.locator('#input');
  if (await input.count()) await input.focus();
  const data = await dump(page);
  await browser.close();
  return data;
}

function diff(oldC, newC) {
  const keys = Array.from(new Set([...Object.keys(oldC), ...Object.keys(newC)])).sort();
  const out = [];
  for (const k of keys) {
    if (SKIP.has(k) || k === 'planTicker') continue;
    const a = oldC[k];
    const b = newC[k];
    if (a == null && b == null) continue;
    if (a == null) { out.push({ sel: k, kind: 'only-new', new: b }); continue; }
    if (b == null) { out.push({ sel: k, kind: 'only-old', old: a }); continue; }
    if (typeof a !== 'object' || typeof b !== 'object') {
      if (a !== b) out.push({ sel: k, kind: 'value', old: a, new: b });
      continue;
    }
    const rec = { sel: k, kind: 'node' };
    const fields = [];
    if (a.tag !== b.tag) fields.push(['tag', a.tag, b.tag]);
    if (a.id !== b.id) fields.push(['id', a.id, b.id]);
    const skipTree = SKIP_TREE.has(k);
    if (!skipTree && !SKIP_CLS.has(k) && a.cls !== b.cls) fields.push(['cls', a.cls, b.cls]);
    if (!skipTree && a.childCount !== b.childCount) {
      fields.push(['childCount', a.childCount, b.childCount]);
    }
    if (!SKIP_RECT.has(k) && a.rect && b.rect) {
      for (const rk of ['x', 'y', 'w', 'h']) {
        if (Math.abs((a.rect[rk] || 0) - (b.rect[rk] || 0)) >= 1) {
          fields.push(['rect.' + rk, a.rect[rk], b.rect[rk]]);
        }
      }
    }
    if (a.style && b.style) {
      for (const sk of Object.keys(a.style)) {
        if (SKIP_USED_SIZE.has(k) && (sk === 'width' || sk === 'height' || sk === 'minHeight' || sk === 'minWidth')) continue;
        if (a.style[sk] !== b.style[sk]) fields.push(['style.' + sk, a.style[sk], b.style[sk]]);
      }
    }
    const ak = (a.kids || []).map((x) => x.tag + '#' + x.id + '.' + x.cls).join('|');
    const bk = (b.kids || []).map((x) => x.tag + '#' + x.id + '.' + x.cls).join('|');
    if (!skipTree && ak !== bk) fields.push(['kids', ak, bk]);
    if (fields.length) {
      rec.fields = fields;
      out.push(rec);
    }
  }
  return out;
}

async function main() {
  fs.mkdirSync(path.dirname(OUT), { recursive: true });
  const oldC = await open(OLD_UI, false);
  // Semantic CSS vs live: do not load the pixel fixture (frozen journal/bars).
  const newC = await open(NEW_UI, false);
  const d = diff(oldC, newC);
  const ticker = compareTickers(oldC.planTicker, newC.planTicker);
  const report = { old: OLD_UI, neu: NEW_UI, diffs: d.length, ticker: ticker, items: d };
  fs.writeFileSync(OUT, JSON.stringify({ old: oldC, neu: newC, diff: d, ticker: ticker }, null, 2));
  const named = d.filter((item) => NAMED[item.sel]);
  const unexplained = d.filter((item) => !NAMED[item.sel]);
  console.log(
    'wrote ' + OUT +
    ' chrome_diffs=' + d.length +
    ' named=' + named.length +
    ' unexplained=' + unexplained.length +
    ' ticker=' + (ticker.ok ? 'OK' : 'FAIL'),
  );
  if (!ticker.ok) {
    console.log('TICKER TREE MISMATCH (only fill width and triangle left may differ)');
    console.log(JSON.stringify(ticker, null, 2));
  }
  for (const item of named) {
    console.log('NAMED ' + item.sel + ' — ' + NAMED[item.sel]);
  }
  if (!ticker.ok || unexplained.length) process.exitCode = 1;
  for (const item of d) {
    if (item.kind !== 'node') {
      console.log(item.kind + ' ' + item.sel);
      continue;
    }
    console.log('NODE ' + item.sel);
    for (const [name, a, b] of item.fields) {
      if (name === 'kids' || (typeof a === 'string' && a.length > 80)) {
        console.log('  ' + name + ':');
        console.log('    old ' + a);
        console.log('    new ' + b);
      } else {
        console.log('  ' + name + ': ' + a + ' → ' + b);
      }
    }
  }
}

main().catch((e) => {
  console.error(e && e.stack ? e.stack : e);
  process.exit(1);
});
