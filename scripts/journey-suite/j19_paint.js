// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright census for J19 (🎯T491). Opens the isolate cockpit and
// asserts connect-replay paint: one virtual-list row per replayed owner
// turn, unique _vIndex / prefix-sum tops. The daily collapse
// (msgHistory full, __transcriptRows.length===1, every .msg at top:0)
// is a fail. Never bind Universe A (:13705).
//
//   node scripts/journey-suite/j19_paint.js --host 127.0.0.1:PORT --prefix ROOThist-
//
// Exit 0 only when the painted list is not collapsed. Exit 2 on usage
// / daily-port. Exit 1 on the product collapse (or a setup miss).

'use strict';

const path = require('path');
const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const DAILY_PORT = 13705;
const argv = process.argv.slice(2);
function opt(name, def) {
  const i = argv.indexOf('--' + name);
  if (i === -1) return def;
  const next = argv[i + 1];
  return next && !next.startsWith('--') ? next : true;
}

const HOST = String(opt('host', '') || '');
const PREFIX = String(opt('prefix', 'ROOThist-') || 'ROOThist-');
const MIN_MARKERS = Math.max(1, parseInt(String(opt('min', '8')), 10) || 8);
const EXPECT = Math.max(MIN_MARKERS, parseInt(String(opt('expect', String(MIN_MARKERS))), 10) || MIN_MARKERS);

function die(code, msg) {
  console.error(msg);
  process.exit(code);
}

if (!HOST) die(2, 'j19_paint: --host HOST:PORT is required');
if (HOST.indexOf(':' + DAILY_PORT) !== -1 || HOST === String(DAILY_PORT)) {
  die(2, 'j19_paint: refuses daily port ' + DAILY_PORT + ' (Universe A)');
}

(async () => {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1100, height: 900 } });
  const url = 'http://' + HOST + '/';
  try {
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await page.waitForFunction(() => {
      return !!(document.getElementById('messages-canvas') &&
        (typeof window.__transcriptRows !== 'undefined'));
    }, null, { timeout: 20000 });

    // Wait out connect replay + the 150ms idle pin, then a little more so
    // a post-pin wipe (the daily collapse) has time to land before census.
    await page.waitForFunction(() => {
      const st = document.getElementById('status-text');
      const txt = st ? String(st.textContent || '') : '';
      const replay = !!window.historyReplayActive;
      const awaiting = !!window.awaitingHistoryMeta;
      const rows = (window.__transcriptRows || []).length;
      const attached = document.querySelectorAll('#messages-canvas > .msg').length;
      return !replay && !awaiting && (txt === 'connected' || rows > 0 || attached > 0);
    }, null, { timeout: 25000 }).catch(() => {});
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 800)));

    const census = await page.evaluate((prefix) => {
      const rows = Array.isArray(window.__transcriptRows) ? window.__transcriptRows : [];
      const layout = window.__transcriptLayout || {};
      const attached = [...document.querySelectorAll('#messages-canvas > .msg')];
      const users = [...document.querySelectorAll('#messages-canvas > .msg.user, #messages > .msg.user')];
      const rowTexts = rows.map((r) => String((r && r.text) || ''));
      const userTexts = users.map((el) => String((el.innerText || el.textContent || '')).trim());
      const markers = [];
      const seen = Object.create(null);
      function note(text) {
        const s = String(text || '');
        const idx = s.indexOf(prefix);
        if (idx < 0) return;
        const tok = s.slice(idx).split(/\s/)[0];
        if (!tok || seen[tok]) return;
        seen[tok] = true;
        markers.push(tok);
      }
      rowTexts.forEach(note);
      userTexts.forEach(note);

      const vIndexes = attached.map((el) => (el._vIndex == null ? null : (el._vIndex | 0)));
      const tops = attached.map((el) => {
        const raw = el.style && el.style.top;
        if (raw) return parseFloat(raw) || 0;
        return el.offsetTop || 0;
      });
      const uniqueV = [...new Set(vIndexes.filter((v) => v != null))];
      const uniqueTops = [...new Set(tops.map((t) => Math.round(t)))];
      const stackedAt0 = attached.filter((el, i) => {
        const v = vIndexes[i];
        const t = tops[i];
        return (v === 0 || v == null) && Math.abs(t) < 1;
      }).length;
      const canvas = document.getElementById('messages-canvas');
      const status = document.getElementById('status-text');
      return {
        status: status ? String(status.textContent || '') : '',
        historyReplayActive: !!window.historyReplayActive,
        awaitingHistoryMeta: !!window.awaitingHistoryMeta,
        transcriptRows: rows.length,
        layoutHeights: Array.isArray(layout.heights) ? layout.heights.length : 0,
        attached: attached.length,
        userEls: users.length,
        markers: markers,
        uniqueVIndex: uniqueV.length,
        uniqueTops: uniqueTops.length,
        stackedAt0: stackedAt0,
        vIndexes: vIndexes.slice(0, 24),
        tops: tops.slice(0, 24).map((t) => Math.round(t)),
        canvasH: canvas ? canvas.offsetHeight : 0,
        rowRoles: rows.map((r) => (r && r.role) || ''),
      };
    }, PREFIX);

    const markerN = census.markers.length;
    // Setup miss: the journal never reached the page. Collapse: some of
    // the seeded turns painted (or a leftover bubble remains) but the
    // virtual-list model did not keep one row per turn — the daily
    // "history vanished" picture (🎯T491).
    const missingSeed = markerN === 0 && census.userEls === 0 && census.transcriptRows === 0;
    const collapsed = !missingSeed && (
      markerN < MIN_MARKERS ||
      census.transcriptRows < MIN_MARKERS ||
      census.uniqueVIndex < MIN_MARKERS ||
      (census.uniqueTops <= 1 && census.attached > 1) ||
      (census.transcriptRows <= 1 && EXPECT > 1)
    );
    const ok = !collapsed && !missingSeed &&
      census.transcriptRows >= MIN_MARKERS &&
      census.uniqueVIndex >= MIN_MARKERS &&
      census.uniqueTops >= MIN_MARKERS &&
      markerN >= MIN_MARKERS;

    const report = {
      host: HOST,
      prefix: PREFIX,
      min: MIN_MARKERS,
      expect: EXPECT,
      ok: ok,
      collapsed: collapsed,
      missingSeed: missingSeed,
      census: census,
    };
    console.log(JSON.stringify(report, null, 2));

    if (collapsed) {
      die(1,
        'J19 paint collapse: transcriptRows=' + census.transcriptRows +
        ' userEls=' + census.userEls +
        ' attached=' + census.attached +
        ' uniqueVIndex=' + census.uniqueVIndex +
        ' uniqueTops=' + census.uniqueTops +
        ' stackedAt0=' + census.stackedAt0 +
        ' markers=' + markerN);
    }
    if (missingSeed) {
      die(1,
        'J19 setup: replay painted fewer than ' + MIN_MARKERS +
        ' ' + PREFIX + '* markers (got ' + markerN +
        ', userEls=' + census.userEls + ')');
    }
    if (!ok) {
      die(1,
        'J19 paint short: transcriptRows=' + census.transcriptRows +
        ' uniqueVIndex=' + census.uniqueVIndex +
        ' uniqueTops=' + census.uniqueTops +
        ' markers=' + markerN);
    }
  } finally {
    await browser.close();
  }
})().catch((err) => {
  die(1, 'j19_paint: ' + (err && err.stack ? err.stack : err));
});
