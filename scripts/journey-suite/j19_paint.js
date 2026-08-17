// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright census for J19 (🎯T491 / 🎯T493 / 🎯T494).
// Isolate cockpit only — never :13705, never the owner's journal.
// Seeded ROOThist-* turns must survive connect replay AND render in the
// pinned viewport: checkVisibility, centre hit-test, then a screenshot
// for Vision OCR (applied by the Go journey).
//
//   node scripts/journey-suite/j19_paint.js --host 127.0.0.1:PORT \
//        --prefix ROOThist- --screenshot /tmp/j19.png
//
// Exit 0 only when the model is not collapsed AND the pane is not empty
// AND the T493 DOM gates pass AND the T494.1 visual desert is absent
// (Latest hidden, no empty slots, no void between bubbles) AND a
// scroll-up-then-down detour does not open a void under the last turn
// (🎯T494.1.2).
// Exit 2 on usage / daily-port.

'use strict';

const path = require('path');
const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);
const VC = require(path.join(__dirname, '..', '..', 'web', 'scripts', 'viewport_census.js'));

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
const SCREENSHOT = String(opt('screenshot', '') || '');

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
  const context = await browser.newContext({
    viewport: VC.ORACLE_VIEWPORT,
    screen: VC.ORACLE_VIEWPORT,
    deviceScaleFactor: VC.ORACLE_DPR,
  });
  const page = await context.newPage();
  const censusPath = path.join(__dirname, '..', '..', 'web', 'scripts', 'viewport_census.js');
  await page.addInitScript({ path: censusPath });
  const url = 'http://' + HOST + '/';
  try {
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await page.waitForFunction(() => {
      return !!(document.getElementById('messages-canvas') &&
        (typeof window.__transcriptRows !== 'undefined') &&
        window.ViewportCensus);
    }, null, { timeout: 20000 });

    await page.waitForFunction(() => {
      const st = document.getElementById('status-text');
      const txt = st ? String(st.textContent || '') : '';
      const replay = !!window.historyReplayActive;
      const awaiting = !!window.awaitingHistoryMeta;
      const rows = (window.__transcriptRows || []).length;
      const attached = document.querySelectorAll('#messages-canvas > .msg').length;
      return !replay && !awaiting && (txt === 'connected' || rows > 0 || attached > 0);
    }, null, { timeout: 25000 }).catch(() => {});
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 400)));

    // Do not force scrollTop = scrollHeight — that is the T494 bug
    // (lands in the turn-slot tail). Census the product's connect pin.
    await page.evaluate(() => new Promise((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(resolve));
    }));
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 200)));

    const census = await page.evaluate((prefix) => {
      const base = window.ViewportCensus.collect({ prefix: prefix });
      const rows = Array.isArray(window.__transcriptRows) ? window.__transcriptRows : [];
      const attached = [...document.querySelectorAll('#messages-canvas > .msg')];
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
      const status = document.getElementById('status-text');
      return Object.assign({}, base, {
        status: status ? String(status.textContent || '') : '',
        historyReplayActive: !!window.historyReplayActive,
        awaitingHistoryMeta: !!window.awaitingHistoryMeta,
        transcriptRows: rows.length,
        uniqueVIndex: uniqueV.length,
        uniqueTops: uniqueTops.length,
        stackedAt0: stackedAt0,
        userEls: attached.filter((el) => el.classList.contains('user')).length,
      });
    }, PREFIX);

    if (SCREENSHOT) {
      const pane = page.locator('#messages');
      await pane.screenshot({ path: SCREENSHOT });
    }

    const markerN = (census.modelMarkers || []).length;
    const visibleMarkerN = (census.visibleMarkers || []).length;
    const missingSeed = markerN === 0 && census.modelRows === 0;
    const collapsed = !missingSeed && (
      markerN < MIN_MARKERS ||
      census.transcriptRows < MIN_MARKERS ||
      census.uniqueVIndex < MIN_MARKERS ||
      (census.uniqueTops <= 1 && census.attached > 1) ||
      (census.transcriptRows <= 1 && EXPECT > 1)
    );
    const emptyPane = !!census.emptyPane;
    const gatesFail = !!census.gatesFail;
    const viewportDrift = !census.viewportPinned;
    const lastTok = PREFIX + String(EXPECT - 1).padStart(2, '0');
    const lastVisible = (census.visibleMarkers || []).indexOf(lastTok) >= 0 ||
      (census.visibleTexts || []).some(function (t) { return String(t).indexOf(lastTok) !== -1; });
    const noVisibleSeed = !emptyPane && (visibleMarkerN < 1 || !lastVisible);
    const latestFail = !!census.latestOnHardReload;
    const desertFail = !!census.emptySlotDesert;
    const packedFail = !!census.packedPaneFail;
    const desertGapFail = !!census.desertGap;
    const overlapFail = !!census.overlappingRects;
    const liveEndFail = !!census.liveEndDisagree;

    // Programmatic stand-in for the stuck wheel: if we are tracking and
    // not at canvas end, a scrollTop nudge must not snap back.
    const wheel = await page.evaluate(async () => {
      const msgs = document.getElementById('messages');
      if (!msgs) return { skipped: true };
      const before = msgs.scrollTop;
      const dist = msgs.scrollHeight - msgs.clientHeight - before;
      msgs.scrollTop = before + 80;
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      const after = msgs.scrollTop;
      const tracking = typeof window.isTracking === 'function' && window.isTracking();
      const snapped = tracking && dist > 20 && Math.abs(after - before) < 2;
      // Restore the product pin so later OCR sees the live end.
      if (typeof window.pinToLiveEnd === 'function') msgs.scrollTop = window.pinToLiveEnd();
      else msgs.scrollTop = before;
      return { before: before, after: after, dist: dist, tracking: tracking, snapped: snapped };
    });
    const wheelStuck = !!(wheel && wheel.snapped);

    // 🎯T494.1.2: hard-reload lands; scroll up a little then down again
    // must not open a void under the last turn. Intermittent is a fail
    // — run the detour several times.
    const DETOUR_N = 5;
    const detours = [];
    for (let d = 0; d < DETOUR_N; d++) {
      const one = await page.evaluate(async () => {
        const msgs = document.getElementById('messages');
        if (!msgs) return { skipped: true };
        const wait = function (ms) {
          return new Promise(function (r) { setTimeout(r, ms); });
        };
        const frames = function () {
          return new Promise(function (r) {
            requestAnimationFrame(function () { requestAnimationFrame(r); });
          });
        };
        function lastInk() {
          const view = msgs.getBoundingClientRect();
          let bottom = 0;
          const sel = '#messages-canvas > .msg.user, #messages-canvas > .msg.jevons';
          const nodes = msgs.querySelectorAll(sel);
          for (let i = 0; i < nodes.length; i++) {
            const r = nodes[i].getBoundingClientRect();
            if (r.bottom > bottom) bottom = r.bottom;
          }
          return { viewBottom: view.bottom, inkBottom: bottom, inkVoid: view.bottom - bottom };
        }
        // Owner path: wheel up (Free), remat above the fold, then
        // immediately roll back to the end — do not wait for settle.
        // Late noteRowHeightChange then shrinks rows above the pin and
        // the last turn rides up, leaving a void (🎯T494.1.2).
        msgs.dispatchEvent(new WheelEvent('wheel', { deltaY: -120, bubbles: true }));
        // Daily reproduced the void on the first 200px lift with no
        // wait before jumping back (🎯T494.1.2).
        // 200px is enough on daily (tall unmeasured rows sit just
        // above the fold). The isolate end-band is already rematted
        // short turns — climb far enough to hit estimate-tall rows.
        const lift = 200;
        msgs.scrollTop = Math.max(0, msgs.scrollTop - lift);
        if (typeof window.virtualizeMessages === 'function') window.virtualizeMessages();
        await frames();
        await wait(120);
        if (typeof window.virtualizeMessages === 'function') window.virtualizeMessages();
        await frames();
        await wait(80);
        msgs.dispatchEvent(new WheelEvent('wheel', { deltaY: 240, bubbles: true }));
        msgs.scrollTop = msgs.scrollHeight;
        await frames();
        await wait(200);
        if (typeof window.virtualizeMessages === 'function') window.virtualizeMessages();
        await frames();
        await wait(200);
        const after = window.ViewportCensus
          ? window.ViewportCensus.collect({ prefix: '' })
          : {};
        const ink = lastInk();
        return {
          voidBelowLast: after.voidBelowLast,
          voidBelowLastFail: after.voidBelowLastFail,
          canvasRatchetFail: after.canvasRatchetFail,
          canvasRatchet: after.canvasRatchet,
          canvasMinHeight: after.canvasMinHeight,
          layoutTotal: after.layoutTotal,
          lastContentBottom: after.lastContentBottom,
          canvasHeight: after.canvasHeight,
          inkVoid: ink.inkVoid,
          scrollTop: after.scrollTop,
          follow: after.followMode,
        };
      });
      detours.push(one);
    }
    const VOID_VISIBLE = VC.VOID_BELOW_VISIBLE_PX || 120;
    const detourVoid = detours.some(function (t) {
      if (!t || t.skipped) return false;
      if (t.voidBelowLastFail || t.canvasRatchetFail) return true;
      return (Number(t.inkVoid) || 0) > VOID_VISIBLE;
    });

    const ok = !collapsed && !missingSeed && !emptyPane && !gatesFail &&
      !viewportDrift && !noVisibleSeed &&
      !latestFail && !desertFail && !packedFail && !desertGapFail &&
      !overlapFail &&
      !liveEndFail && !wheelStuck && !detourVoid &&
      census.transcriptRows >= MIN_MARKERS &&
      census.uniqueVIndex >= MIN_MARKERS &&
      census.uniqueTops >= MIN_MARKERS &&
      markerN >= MIN_MARKERS &&
      census.visibleInScroller >= 1 &&
      census.visibleCheckOk >= 1 &&
      census.visibleHitOk >= 1 &&
      (census.visibleBubbles || 0) >= 2 &&
      census.fabHidden !== false;

    const report = {
      host: HOST,
      prefix: PREFIX,
      min: MIN_MARKERS,
      expect: EXPECT,
      screenshot: SCREENSHOT,
      ok: ok,
      collapsed: collapsed,
      missingSeed: missingSeed,
      emptyPane: emptyPane,
      gatesFail: gatesFail,
      viewportDrift: viewportDrift,
      latestFail: latestFail,
      desertFail: desertFail,
      packedFail: packedFail,
      desertGapFail: desertGapFail,
      overlapFail: overlapFail,
      liveEndFail: liveEndFail,
      wheelStuck: wheelStuck,
      wheel: wheel,
      detourVoid: detourVoid,
      detours: detours,
      census: census,
    };
    console.log(JSON.stringify(report, null, 2));

    if (viewportDrift) {
      die(1, 'J19 viewport not pinned: inner=' + census.innerWidth + 'x' +
        census.innerHeight + ' dpr=' + census.devicePixelRatio +
        ' want ' + VC.ORACLE_VIEWPORT.width + 'x' + VC.ORACLE_VIEWPORT.height +
        ' dpr=' + VC.ORACLE_DPR);
    }
    if (emptyPane) {
      die(1,
        'J19 empty pane (🎯T494): modelRows=' + census.modelRows +
        ' visibleInScroller=0 attached=' + census.attached +
        ' canvasH=' + census.canvasHeight +
        ' scrollTop=' + census.scrollTop);
    }
    if (gatesFail) {
      die(1,
        'J19 visibility gates (🎯T493): visible=' + census.visibleInScroller +
        ' checkVisibility=' + census.visibleCheckOk +
        ' hitTest=' + census.visibleHitOk);
    }
    if (collapsed) {
      die(1,
        'J19 paint collapse: transcriptRows=' + census.transcriptRows +
        ' uniqueVIndex=' + census.uniqueVIndex +
        ' uniqueTops=' + census.uniqueTops +
        ' stackedAt0=' + census.stackedAt0 +
        ' markers=' + markerN);
    }
    if (missingSeed) {
      die(1, 'J19 setup: isolate seed did not reach the model');
    }
    if (noVisibleSeed) {
      die(1, 'J19 visible turns have no ' + PREFIX + '* token (got ' +
        visibleMarkerN + ' visible markers, visibleInScroller=' +
        census.visibleInScroller + ')');
    }
    if (latestFail) {
      die(1, 'J19 Latest visible after hard-reload (🎯T494.1): fabHidden=' +
        census.fabHidden + ' follow=' + census.followMode +
        ' atBottom=' + census.atBottom + ' dist=' + census.distFromBottom);
    }
    if (desertFail) {
      die(1, 'J19 empty turn-slot desert (🎯T494.1): emptySlots=' +
        census.emptySlots + ' labelledSlots=' + census.labelledSlots);
    }
    if (packedFail) {
      die(1, 'J19 pane not packed (🎯T494.1): visibleBubbles=' +
        census.visibleBubbles + ' visibleInScroller=' + census.visibleInScroller);
    }
    if (overlapFail) {
      die(1, 'J19 overlapping bubbles (🎯T119.7): maxInkGap=' +
        census.maxInkGap + ' visibleBubbles=' + census.visibleBubbles);
    }
    if (desertGapFail) {
      die(1, 'J19 void between bubbles (🎯T494.1): maxInkGap=' +
        census.maxInkGap + ' clientH=' + census.clientHeight +
        ' visibleBubbles=' + census.visibleBubbles);
    }
    if (liveEndFail) {
      die(1, 'J19 two bottoms (🎯T494.1): pinWant=' + census.pinWant +
        ' canvasEnd=' + census.canvasEndPin);
    }
    if (wheelStuck) {
      die(1, 'J19 wheel snap-back (🎯T494.1): st ' + wheel.before +
        ' → ' + wheel.after + ' dist=' + wheel.dist);
    }
    if (detourVoid) {
      const hit = detours.filter(function (t) {
        return t && (t.voidBelowLastFail || t.canvasRatchetFail ||
          (Number(t.inkVoid) || 0) > VOID_VISIBLE);
      })[0] || {};
      die(1, 'J19 void after scroll-up-then-down (🎯T494.1.2): voidBelow=' +
        hit.voidBelowLast + ' inkVoid=' + hit.inkVoid +
        ' ratchet=' + hit.canvasRatchet + ' minH=' + hit.canvasMinHeight +
        ' layout=' + hit.layoutTotal);
    }
    if (!ok) {
      die(1,
        'J19 paint short: transcriptRows=' + census.transcriptRows +
        ' visibleInScroller=' + census.visibleInScroller +
        ' markers=' + markerN);
    }
  } finally {
    await browser.close();
  }
})().catch((err) => {
  die(1, 'j19_paint: ' + (err && err.stack ? err.stack : err));
});
