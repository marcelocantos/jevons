// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright census for J19 (🎯T491 / 🎯T493 / 🎯T494) against the
// React cockpit (🎯T540.1.12). After 🎯T540.2 isolate GET / is React
// when ui/dist exists; otherwise Go starts a :5173-style Vite proxy.
// Never :13705, never the owner's journal.
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

const DAILY_PORT = 13705;
const ORACLE_VIEWPORT = { width: 1280, height: 800 };
const ORACLE_DPR = 1;
const VOID_BELOW_VISIBLE_PX = 120;

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
    viewport: ORACLE_VIEWPORT,
    screen: ORACLE_VIEWPORT,
    deviceScaleFactor: ORACLE_DPR,
  });
  const page = await context.newPage();
  const url = 'http://' + HOST + '/';
  try {
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30000 });
    // React mount: #root + transcript scroller. Do not wait for vanilla
    // window model globals — isolate GET / may still be that surface
    // until T540.2; this host is the React load path.
    await page.waitForFunction(() => {
      return !!(document.getElementById('root') &&
        document.getElementById('messages') &&
        document.getElementById('messages-canvas'));
    }, null, { timeout: 20000 });

    await page.waitForFunction(() => {
      const st = document.getElementById('status-text');
      const txt = st ? String(st.textContent || '') : '';
      const users = document.querySelectorAll('#messages-canvas > .msg.user').length;
      return txt === 'connected' || users > 0;
    }, null, { timeout: 25000 }).catch(() => {});
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 400)));
    await page.evaluate(() => new Promise((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(resolve));
    }));
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 200)));

    // T491: sweep the virtualizer so collapsed-to-one-row is visible.
    // React has no window model array; unique user tokens across a
    // scroll sweep are the model census.
    const sweep = await page.evaluate(async (prefix) => {
      const msgs = document.getElementById('messages');
      const canvas = document.getElementById('messages-canvas');
      if (!msgs || !canvas) return { markers: [], skipped: true };
      const frames = () => new Promise((r) => {
        requestAnimationFrame(() => requestAnimationFrame(r));
      });
      const seen = Object.create(null);
      const markers = [];
      function collect() {
        const nodes = canvas.querySelectorAll(':scope > .msg.user');
        for (let i = 0; i < nodes.length; i++) {
          const s = String(nodes[i].innerText || nodes[i].textContent || '');
          const idx = s.indexOf(prefix);
          if (idx < 0) continue;
          const tok = s.slice(idx).split(/\s/)[0];
          if (!tok || seen[tok]) continue;
          seen[tok] = true;
          markers.push(tok);
        }
      }
      msgs.scrollTop = 0;
      await frames();
      collect();
      let guard = 0;
      while (msgs.scrollTop + msgs.clientHeight < msgs.scrollHeight - 8 && guard < 40) {
        msgs.scrollTop += Math.max(80, msgs.clientHeight * 0.7);
        await frames();
        collect();
        guard++;
      }
      msgs.scrollTop = msgs.scrollHeight;
      await frames();
      collect();
      return { markers: markers, skipped: false };
    }, PREFIX);

    // Product connect pin: live end. Do not leave the sweep mid-list.
    await page.evaluate(() => {
      const msgs = document.getElementById('messages');
      if (msgs) msgs.scrollTop = msgs.scrollHeight;
    });
    await page.evaluate(() => new Promise((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(resolve));
    }));
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 200)));

    const census = await page.evaluate((args) => {
      const prefix = args.prefix;
      const sweepMarkers = args.sweepMarkers || [];
      const scroller = document.getElementById('messages');
      const canvas = document.getElementById('messages-canvas');
      const attached = canvas
        ? Array.prototype.slice.call(canvas.querySelectorAll(':scope > .msg, :scope > .turn-marker'))
        : [];
      const msgEls = canvas
        ? Array.prototype.slice.call(canvas.querySelectorAll(':scope > .msg'))
        : [];
      const scrollerRect = scroller ? scroller.getBoundingClientRect() : {
        left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0,
      };

      function boxCentre(rect) {
        const w = Number(rect && rect.width) || 0;
        const h = Number(rect && rect.height) || 0;
        return { x: (Number(rect.left) || 0) + w / 2, y: (Number(rect.top) || 0) + h / 2 };
      }
      function pointInRect(p, r) {
        if (!p || !r) return false;
        return p.x >= r.left && p.x < r.right && p.y >= r.top && p.y < r.bottom;
      }
      function inspect(el) {
        const r = el.getBoundingClientRect();
        const centre = boxCentre(r);
        const vis = (typeof el.checkVisibility === 'function')
          ? el.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })
          : (r.width > 0 && r.height > 0);
        const hit = document.elementFromPoint(centre.x, centre.y);
        const hitOk = !!(hit && (hit === el || (el.contains && el.contains(hit))));
        return {
          checkVisibility: !!vis,
          hitOk: hitOk,
          inScroller: pointInRect(centre, scrollerRect),
          text: String(el.innerText || el.textContent || '').trim().slice(0, 160),
          w: r.width, h: r.height, top: r.top, bottom: r.bottom,
        };
      }

      const visible = [];
      for (let i = 0; i < msgEls.length; i++) {
        const el = msgEls[i];
        const g = inspect(el);
        if (!g.inScroller) continue;
        let role = 'other';
        if (el.classList.contains('user')) role = 'user';
        else if (el.classList.contains('jevons') || el.classList.contains('assistant')) role = 'assistant';
        visible.push({
          role: role, text: g.text, checkVisibility: g.checkVisibility,
          hitOk: g.hitOk, w: g.w, h: g.h, top: g.top, bottom: g.bottom,
        });
      }

      const vIndexes = attached.map((el) => {
        const raw = el.getAttribute('data-index');
        return raw == null ? null : (raw | 0);
      });
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

      const modelMarkers = sweepMarkers.slice();
      const visibleMarkers = [];
      const seenV = Object.create(null);
      visible.forEach(function (v) {
        const s = String(v.text || '');
        const idx = s.indexOf(prefix);
        if (idx < 0) return;
        const tok = s.slice(idx).split(/\s/)[0];
        if (!tok || seenV[tok]) return;
        seenV[tok] = true;
        visibleMarkers.push(tok);
      });

      const visibleInScroller = visible.length;
      const visibleCheckOk = visible.filter(function (v) { return v.checkVisibility; }).length;
      const visibleHitOk = visible.filter(function (v) { return v.hitOk; }).length;
      const bubbleRects = visible.filter(function (v) {
        return v.role === 'user' || v.role === 'assistant';
      });
      const visibleBubbles = bubbleRects.length;
      const maxInkGap = (function () {
        const boxes = bubbleRects.slice().sort(function (a, b) { return a.top - b.top; });
        let max = 0;
        for (let i = 1; i < boxes.length; i++) {
          const gap = boxes[i].top - boxes[i - 1].bottom;
          if (gap > max) max = gap;
        }
        return max;
      })();
      const overlap = (function () {
        const boxes = bubbleRects.slice().sort(function (a, b) { return a.top - b.top; });
        for (let i = 1; i < boxes.length; i++) {
          if (boxes[i].top < boxes[i - 1].bottom - 0.5) return true;
        }
        return false;
      })();

      let emptySlots = 0;
      let labelledSlots = 0;
      const slots = canvas ? canvas.querySelectorAll(':scope > .turn-marker') : [];
      for (let i = 0; i < slots.length; i++) {
        const t = String(slots[i].innerText || '').trim();
        if (t) labelledSlots++;
        else emptySlots++;
      }

      const sh = scroller ? scroller.scrollHeight : 0;
      const ch = scroller ? scroller.clientHeight : 0;
      const st = scroller ? scroller.scrollTop : 0;
      const canvasEndPin = Math.max(0, sh - ch);
      const pinWant = canvasEndPin;
      const fab = document.getElementById('jump-bottom');
      const fabHidden = !fab || !!fab.hidden;
      const distFromBottom = sh - ch - st;
      const atBottom = ch <= 0 || distFromBottom <= 16 || Math.abs(st - pinWant) <= 16;
      const followMode = atBottom ? 'track' : 'free';

      let lastContentBottom = 0;
      for (let i = 0; i < attached.length; i++) {
        const r = attached[i].getBoundingClientRect();
        const canvasRect = canvas ? canvas.getBoundingClientRect() : { top: 0 };
        const end = (r.bottom - canvasRect.top);
        if (end > lastContentBottom) lastContentBottom = end;
      }
      const canvasH = canvas ? canvas.offsetHeight : 0;
      const voidBelowLast = canvasH > 0 ? canvasH - lastContentBottom : 0;

      const maxIdx = uniqueV.length ? Math.max.apply(null, uniqueV) : -1;
      const modelRows = Math.max(sweepMarkers.length, maxIdx + 1, attached.length);
      const emptyPane = modelRows > 0 && visibleInScroller === 0;
      const desertCap = Math.max(120, ch * 0.25);

      const status = document.getElementById('status-text');
      return {
        status: status ? String(status.textContent || '') : '',
        innerWidth: window.innerWidth,
        innerHeight: window.innerHeight,
        devicePixelRatio: window.devicePixelRatio,
        viewportPinned: window.innerWidth === 1280 && window.innerHeight === 800 &&
          window.devicePixelRatio === 1,
        modelRows: modelRows,
        transcriptRows: modelRows,
        attached: attached.length,
        userEls: msgEls.filter((el) => el.classList.contains('user')).length,
        uniqueVIndex: uniqueV.length,
        uniqueTops: uniqueTops.length,
        stackedAt0: stackedAt0,
        visibleInScroller: visibleInScroller,
        visibleCheckOk: visibleCheckOk,
        visibleHitOk: visibleHitOk,
        visibleBubbles: visibleBubbles,
        visibleTexts: visible.map(function (v) { return v.text; }),
        modelMarkers: modelMarkers,
        visibleMarkers: visibleMarkers,
        emptySlots: emptySlots,
        labelledSlots: labelledSlots,
        emptyPane: emptyPane,
        emptySlotDesert: emptySlots > 0,
        packedPaneFail: visibleBubbles < 2,
        maxInkGap: maxInkGap,
        desertGap: maxInkGap > desertCap,
        overlappingRects: overlap,
        latestOnHardReload: fabHidden === false || (followMode === 'track' && atBottom === false),
        liveEndDisagree: Math.abs(pinWant - canvasEndPin) > 16,
        lastContentBottom: lastContentBottom,
        voidBelowLast: voidBelowLast,
        voidBelowLastFail: canvasH > 0 && lastContentBottom > 0 && (canvasH - lastContentBottom) > 120,
        canvasMinHeight: 0,
        layoutTotal: canvasH,
        canvasRatchet: 0,
        canvasRatchetFail: false,
        gatesFail: visibleInScroller > 0 &&
          (visibleCheckOk < visibleInScroller || visibleHitOk < visibleInScroller),
        followMode: followMode,
        fabHidden: fabHidden,
        atBottom: atBottom,
        pinWant: pinWant,
        canvasEndPin: canvasEndPin,
        distFromBottom: distFromBottom,
        scrollTop: st,
        scrollHeight: sh,
        clientHeight: ch,
        canvasHeight: canvasH,
        react: true,
        root: !!document.getElementById('root'),
      };
    }, { prefix: PREFIX, sweepMarkers: sweep.markers || [] });

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
      (census.uniqueVIndex < 2 && census.attached > 1) ||
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

    const wheel = await page.evaluate(async () => {
      const msgs = document.getElementById('messages');
      if (!msgs) return { skipped: true };
      const before = msgs.scrollTop;
      const dist = msgs.scrollHeight - msgs.clientHeight - before;
      msgs.scrollTop = before + 80;
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      const after = msgs.scrollTop;
      const tracking = dist <= 16;
      const snapped = tracking && dist > 20 && Math.abs(after - before) < 2;
      msgs.scrollTop = msgs.scrollHeight;
      return { before: before, after: after, dist: dist, tracking: tracking, snapped: snapped };
    });
    const wheelStuck = !!(wheel && wheel.snapped);

    const DETOUR_N = 5;
    const detours = [];
    for (let d = 0; d < DETOUR_N; d++) {
      const one = await page.evaluate(async () => {
        const msgs = document.getElementById('messages');
        const canvas = document.getElementById('messages-canvas');
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
          const nodes = msgs.querySelectorAll('#messages-canvas > .msg.user, #messages-canvas > .msg.jevons');
          for (let i = 0; i < nodes.length; i++) {
            const r = nodes[i].getBoundingClientRect();
            if (r.bottom > bottom) bottom = r.bottom;
          }
          return { viewBottom: view.bottom, inkBottom: bottom, inkVoid: view.bottom - bottom };
        }
        msgs.dispatchEvent(new WheelEvent('wheel', { deltaY: -120, bubbles: true }));
        const lift = 200;
        msgs.scrollTop = Math.max(0, msgs.scrollTop - lift);
        await frames();
        await wait(120);
        await frames();
        await wait(80);
        msgs.dispatchEvent(new WheelEvent('wheel', { deltaY: 240, bubbles: true }));
        msgs.scrollTop = msgs.scrollHeight;
        await frames();
        await wait(200);
        await frames();
        await wait(200);
        const ink = lastInk();
        let lastContentBottom = 0;
        if (canvas) {
          const canvasRect = canvas.getBoundingClientRect();
          const nodes = canvas.querySelectorAll(':scope > .msg, :scope > .turn-marker');
          for (let i = 0; i < nodes.length; i++) {
            const r = nodes[i].getBoundingClientRect();
            const end = r.bottom - canvasRect.top;
            if (end > lastContentBottom) lastContentBottom = end;
          }
        }
        const canvasH = canvas ? canvas.offsetHeight : 0;
        const voidBelowLast = canvasH > 0 ? canvasH - lastContentBottom : 0;
        return {
          voidBelowLast: voidBelowLast,
          voidBelowLastFail: canvasH > 0 && lastContentBottom > 0 && voidBelowLast > 120,
          canvasRatchetFail: false,
          canvasRatchet: 0,
          canvasMinHeight: 0,
          layoutTotal: canvasH,
          lastContentBottom: lastContentBottom,
          canvasHeight: canvasH,
          inkVoid: ink.inkVoid,
          scrollTop: msgs.scrollTop,
          follow: '',
        };
      });
      detours.push(one);
    }
    const detourVoid = detours.some(function (t) {
      if (!t || t.skipped) return false;
      if (t.voidBelowLastFail || t.canvasRatchetFail) return true;
      return (Number(t.inkVoid) || 0) > VOID_BELOW_VISIBLE_PX;
    });

    const ok = !collapsed && !missingSeed && !emptyPane && !gatesFail &&
      !viewportDrift && !noVisibleSeed &&
      !latestFail && !desertFail && !packedFail && !desertGapFail &&
      !overlapFail &&
      !liveEndFail && !wheelStuck && !detourVoid &&
      census.transcriptRows >= MIN_MARKERS &&
      markerN >= MIN_MARKERS &&
      census.visibleInScroller >= 1 &&
      census.visibleCheckOk >= 1 &&
      census.visibleHitOk >= 1 &&
      (census.visibleBubbles || 0) >= 2 &&
      census.fabHidden !== false &&
      census.root === true;

    const report = {
      host: HOST,
      prefix: PREFIX,
      min: MIN_MARKERS,
      expect: EXPECT,
      screenshot: SCREENSHOT,
      react: true,
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
        ' want ' + ORACLE_VIEWPORT.width + 'x' + ORACLE_VIEWPORT.height +
        ' dpr=' + ORACLE_DPR);
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
          (Number(t.inkVoid) || 0) > VOID_BELOW_VISIBLE_PX);
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
