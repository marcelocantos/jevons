// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Daily :13705 hard-reload census (🎯T494 / 🎯T494.1 sanity).
// Intentional Universe-A probe — never part of make test-journey.
//
//   node scripts/chat-ui-test/t494-daily-census.js
//   node scripts/chat-ui-test/t494-daily-census.js --screenshot /tmp/t494.png
//
// T494: replay tail visible (visibleInScroller ≥ 1 with a real bubble).
// T494.1 sanity: Latest hidden, ≥2 user/assistant bubbles, no empty-slot
// desert, pin and canvas-end agree. Isolate J19 is the packing oracle.
// Exit 1 on census fail; exit 2 on setup.

'use strict';

const path = require('path');
const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);
const VC = require(path.join(__dirname, '..', '..', 'web', 'scripts', 'viewport_census.js'));

const argv = process.argv.slice(2);
function opt(name, def) {
  const i = argv.indexOf('--' + name);
  if (i === -1) return def;
  const next = argv[i + 1];
  return next && !next.startsWith('--') ? next : true;
}

const HOST = String(opt('host', '127.0.0.1:13705') || '127.0.0.1:13705');
const SCREENSHOT = String(opt('screenshot', '') || '');
const SETTLE_MS = Math.max(500, parseInt(String(opt('settle', '2500')), 10) || 2500);

function die(code, msg) {
  console.error(msg);
  process.exit(code);
}

(async () => {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: VC.ORACLE_VIEWPORT,
    screen: VC.ORACLE_VIEWPORT,
    deviceScaleFactor: VC.ORACLE_DPR,
  });
  const page = await context.newPage();
  const censusPath = path.join(__dirname, '..', '..', 'web', 'scripts', 'viewport_census.js');
  await page.addInitScript({ path: censusPath });

  const url = 'http://' + HOST + '/?t494census=' + Date.now();
  try {
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 45000 });
    await page.waitForFunction(() => {
      return !!(document.getElementById('messages-canvas') &&
        (typeof window.__transcriptRows !== 'undefined') &&
        window.ViewportCensus);
    }, null, { timeout: 30000 });

    await page.waitForFunction(() => {
      const st = document.getElementById('status-text');
      const txt = st ? String(st.textContent || '') : '';
      const replay = !!window.historyReplayActive;
      const awaiting = !!window.awaitingHistoryMeta;
      const rows = (window.__transcriptRows || []).length;
      const attached = document.querySelectorAll('#messages-canvas > .msg').length;
      return !replay && !awaiting && (txt === 'connected' || rows > 0 || attached > 0);
    }, null, { timeout: 45000 }).catch(() => {});

    await page.evaluate((ms) => new Promise((resolve) => setTimeout(resolve, ms)), SETTLE_MS);
    await page.evaluate(() => new Promise((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(resolve));
    }));

    // Census the product's connect pin — do not force scrollHeight.
    const census = await page.evaluate(() => {
      const base = window.ViewportCensus.collect({ prefix: '' });
      const rows = Array.isArray(window.__transcriptRows) ? window.__transcriptRows : [];
      const attached = [...document.querySelectorAll('#messages-canvas > .msg')];
      const status = document.getElementById('status-text');
      return Object.assign({}, base, {
        status: status ? String(status.textContent || '') : '',
        historyReplayActive: !!window.historyReplayActive,
        awaitingHistoryMeta: !!window.awaitingHistoryMeta,
        transcriptRows: rows.length,
        attachedDom: attached.length,
      });
    });

    const roles = await page.evaluate(() => {
      const scroller = document.getElementById('messages');
      const scrollerRect = scroller ? scroller.getBoundingClientRect() : null;
      const attached = [...document.querySelectorAll('#messages-canvas > .msg')];
      const out = { user: 0, assistant: 0, other: 0, samples: [] };
      if (!scrollerRect) return out;
      for (const el of attached) {
        const r = el.getBoundingClientRect();
        const cx = r.left + r.width / 2;
        const cy = r.top + r.height / 2;
        if (cx < scrollerRect.left || cx >= scrollerRect.right ||
            cy < scrollerRect.top || cy >= scrollerRect.bottom) continue;
        let role = 'other';
        if (el.classList.contains('user')) role = 'user';
        else if (el.classList.contains('jevons') || el.classList.contains('assistant')) role = 'assistant';
        out[role]++;
        if (out.samples.length < 6) {
          out.samples.push({
            role: role,
            text: String(el.innerText || '').trim().slice(0, 120),
          });
        }
      }
      return out;
    });
    census.visibleRoles = roles;

    if (SCREENSHOT) {
      const pane = page.locator('#messages');
      await pane.screenshot({ path: SCREENSHOT });
      census.screenshot = SCREENSHOT;
    }

    const realBubble = (roles.user + roles.assistant) >= 1;
    const packed = (roles.user + roles.assistant) >= 2 ||
      (Number(census.visibleBubbles) || 0) >= 2;
    const emptyPane = !!census.emptyPane ||
      ((Number(census.modelRows) || 0) > 1 && (Number(census.visibleInScroller) || 0) === 0);
    const latestOk = census.fabHidden !== false && !census.latestOnHardReload;
    const desertOk = !census.emptySlotDesert;
    const liveEndOk = !census.liveEndDisagree && census.atBottom !== false;
    const ok = !emptyPane &&
      (Number(census.visibleInScroller) || 0) >= 1 &&
      realBubble &&
      packed &&
      latestOk &&
      desertOk &&
      liveEndOk &&
      !census.gatesFail &&
      census.viewportPinned !== false;

    const report = {
      host: HOST,
      url: url,
      ok: ok,
      emptyPane: emptyPane,
      realBubble: realBubble,
      census: {
        status: census.status,
        modelRows: census.modelRows,
        attached: census.attached,
        visibleInScroller: census.visibleInScroller,
        visibleCheckOk: census.visibleCheckOk,
        visibleHitOk: census.visibleHitOk,
        visibleBubbles: census.visibleBubbles,
        visibleRoles: roles,
        emptyPane: census.emptyPane,
        emptySlots: census.emptySlots,
        labelledSlots: census.labelledSlots,
        emptySlotDesert: census.emptySlotDesert,
        packedPaneFail: census.packedPaneFail,
        latestOnHardReload: census.latestOnHardReload,
        liveEndDisagree: census.liveEndDisagree,
        fabHidden: census.fabHidden,
        followMode: census.followMode,
        atBottom: census.atBottom,
        pinWant: census.pinWant,
        canvasEndPin: census.canvasEndPin,
        scrollTop: census.scrollTop,
        scrollHeight: census.scrollHeight,
        clientHeight: census.clientHeight,
        canvasHeight: census.canvasHeight,
        voidBelowLast: census.voidBelowLast,
        voidBelowLastFail: census.voidBelowLastFail,
        canvasRatchetFail: census.canvasRatchetFail,
        desertGap: census.desertGap,
        maxInkGap: census.maxInkGap,
        gatesFail: census.gatesFail,
        viewportPinned: census.viewportPinned,
        historyReplayActive: census.historyReplayActive,
        awaitingHistoryMeta: census.awaitingHistoryMeta,
      },
      screenshot: SCREENSHOT || null,
    };
    console.log(JSON.stringify(report, null, 2));

    if (!ok) {
      die(1,
        'T494.1 daily census FAIL: visibleInScroller=' + census.visibleInScroller +
        ' modelRows=' + census.modelRows +
        ' packed=' + packed +
        ' emptyPane=' + emptyPane +
        ' fabHidden=' + census.fabHidden +
        ' latestOnHardReload=' + census.latestOnHardReload +
        ' emptySlots=' + census.emptySlots +
        ' liveEndDisagree=' + census.liveEndDisagree);
    }
  } finally {
    await browser.close().catch(() => {});
  }
})().catch((err) => {
  die(2, 't494-daily-census setup: ' + (err && err.stack ? err.stack : err));
});
