// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Viewport census for cockpit on-screen evidence (🎯T493).
//
// Three gates, all required: checkVisibility, centre hit-test (self or
// descendant), Vision OCR of a pinned-viewport screenshot. VisibilityHelper
// is the named DOM inspect (1+2). OCR lives in internal/imagetext and is
// applied by the journey after the screenshot — a caption is not evidence.
//
// UMD so Playwright addInitScript and Node require() share one file.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ViewportCensus = factory();
  }
}(typeof globalThis !== 'undefined' ? globalThis : this, function () {
  'use strict';

  // Host-independent oracle size. Never viewport:null.
  const ORACLE_VIEWPORT = { width: 1280, height: 800 };
  const ORACLE_DPR = 1;

  function boxCentre(rect) {
    const w = Number(rect && rect.width) || 0;
    const h = Number(rect && rect.height) || 0;
    const left = Number(rect && rect.left) || 0;
    const top = Number(rect && rect.top) || 0;
    return { x: left + w / 2, y: top + h / 2 };
  }

  function rectsIntersect(a, b) {
    if (!a || !b) return false;
    const aw = Number(a.width) || 0;
    const ah = Number(a.height) || 0;
    const bw = Number(b.width) || 0;
    const bh = Number(b.height) || 0;
    if (aw <= 0 || ah <= 0 || bw <= 0 || bh <= 0) return false;
    return a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top;
  }

  function pointInRect(p, r) {
    if (!p || !r) return false;
    return p.x >= r.left && p.x < r.right && p.y >= r.top && p.y < r.bottom;
  }

  // hitKey is the hit element's identity; contains is true when the
  // candidate contains the hit (descendant). No hit → fail.
  function hitTestPasses(hitKey, selfKey, contains) {
    if (hitKey == null || selfKey == null) return false;
    if (hitKey === selfKey) return true;
    return !!contains;
  }

  // inspect is the 🎯T493 DOM pair: checkVisibility (opacity + CSS
  // visibility) and centre hit-test. inScroller is the pin, not a third
  // gate — an off-viewport centre fails the hit-test because
  // elementFromPoint returns null / chrome.
  function inspect(el, scrollerRect) {
    const empty = {
      checkVisibility: false,
      hitOk: false,
      inScroller: false,
      centre: { x: 0, y: 0 },
      hitTag: '',
      w: 0,
      h: 0,
      top: 0,
      bottom: 0,
      text: '',
    };
    if (!el || typeof el.getBoundingClientRect !== 'function') return empty;
    const r = el.getBoundingClientRect();
    const centre = boxCentre(r);
    const vis = (typeof el.checkVisibility === 'function')
      ? el.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })
      : (r.width > 0 && r.height > 0);
    const hit = (typeof document !== 'undefined' && typeof document.elementFromPoint === 'function')
      ? document.elementFromPoint(centre.x, centre.y)
      : null;
    const hitOk = hitTestPasses(hit, el, !!(hit && typeof el.contains === 'function' && el.contains(hit)));
    return {
      checkVisibility: !!vis,
      hitOk: hitOk,
      inScroller: pointInRect(centre, scrollerRect),
      centre: centre,
      hitTag: hit ? String(hit.tagName || '') : '',
      w: r.width,
      h: r.height,
      top: r.top,
      bottom: r.bottom,
      text: String(el.innerText || el.textContent || '').trim().slice(0, 160),
    };
  }

  function domOnScreen(g) {
    return !!(g && g.checkVisibility && g.hitOk && g.inScroller);
  }

  const VisibilityHelper = {
    inspect: inspect,
    domOnScreen: domOnScreen,
  };

  // modelRows > 0 with nothing in the scroller is the empty-pane fail
  // (🎯T494): the list claims history and the owner sees a blank.
  function emptyPaneFail(modelRows, visibleInScroller) {
    return (Number(modelRows) || 0) > 0 && (Number(visibleInScroller) || 0) === 0;
  }

  // Empty OCR of a non-empty model is the same empty pane, never a green
  // invented from the DOM (🎯T493). Degraded OCR is OUTAGE, not this.
  function emptyOCRFail(modelRows, ocrText) {
    return (Number(modelRows) || 0) > 0 && String(ocrText || '').trim() === '';
  }

  // 🎯T494.1: Latest after a hard-reload means pin and atBottom disagree.
  function latestOnHardReloadFail(opts) {
    const o = opts || {};
    if (o.fabHidden === false) return true;
    if (o.followMode === 'track' && o.atBottom === false) return true;
    return false;
  }

  // Unlabelled turn-slots reserve layout and paint nothing (the white desert).
  function emptySlotDesertFail(emptySlots) {
    return (Number(emptySlots) || 0) > 0;
  }

  // A 1280×800 oracle viewport with ≥2 seeded turns must show ≥2 bubbles.
  function packedPaneFail(visibleBubbles, min) {
    const need = min == null ? 2 : Number(min);
    return (Number(visibleBubbles) || 0) < need;
  }

  // Largest empty band between consecutive ink boxes (top-sorted).
  // The owner screenshot: two leftover turns and a viewport of void.
  function maxInkGapPx(rects) {
    const boxes = (rects || []).filter(function (r) {
      return r && Number.isFinite(Number(r.top)) && Number.isFinite(Number(r.bottom));
    }).slice().sort(function (a, b) { return Number(a.top) - Number(b.top); });
    let max = 0;
    for (let i = 1; i < boxes.length; i++) {
      const gap = Number(boxes[i].top) - Number(boxes[i - 1].bottom);
      if (gap > max) max = gap;
    }
    return max;
  }

  // More than a quarter of the pane empty between two consecutive
  // bubbles is "I took one look and something is horribly wrong."
  const DESERT_GAP_FRAC = 0.25;
  const DESERT_GAP_MIN_PX = 120;
  // 🎯T119.7: two live bubbles whose boxes intersect (stale prefix).
  function overlappingRectsFail(rects) {
    const boxes = (rects || []).filter(function (r) {
      return r && Number.isFinite(Number(r.top)) && Number.isFinite(Number(r.bottom));
    }).slice().sort(function (a, b) { return Number(a.top) - Number(b.top); });
    for (let i = 1; i < boxes.length; i++) {
      if (Number(boxes[i].top) < Number(boxes[i - 1].bottom) - 0.5) return true;
    }
    return false;
  }

  function desertGapFail(maxGap, clientHeight) {
    const ch = Number(clientHeight) || 0;
    const g = Number(maxGap) || 0;
    const cap = Math.max(DESERT_GAP_MIN_PX, ch * DESERT_GAP_FRAC);
    return g > cap;
  }

  // 🎯T494.1.2: empty canvas under the last painted turn after a
  // scroll-up-then-down. 16px is prefix noise; 120px is the owner's
  // "inches of whitespace" picture. Journey uses the latter.
  const VOID_BELOW_EPS_PX = 16;
  const VOID_BELOW_VISIBLE_PX = DESERT_GAP_MIN_PX;
  // Canvas min-height ratcheted past the prefix (🎯T494.1.2). The
  // MutationObserver snaps #messages-canvas as if it were a row.
  function canvasRatchetFail(canvasMinHeight, layoutTotal, epsPx) {
    const minH = Number(canvasMinHeight) || 0;
    const total = Number(layoutTotal) || 0;
    const eps = epsPx == null ? VOID_BELOW_VISIBLE_PX : Number(epsPx);
    const e = Number.isFinite(eps) && eps >= 0 ? eps : VOID_BELOW_VISIBLE_PX;
    if (!(minH > 0) || !(total > 0)) return false;
    return (minH - total) > e;
  }

  function voidBelowLastFail(lastContentBottom, canvasHeight, epsPx) {
    const last = Number(lastContentBottom) || 0;
    const canvas = Number(canvasHeight) || 0;
    const eps = epsPx == null ? VOID_BELOW_VISIBLE_PX : Number(epsPx);
    const e = Number.isFinite(eps) && eps >= 0 ? eps : VOID_BELOW_VISIBLE_PX;
    if (!(last > 0) || !(canvas > 0)) return false;
    return (canvas - last) > e;
  }

  // Pin target and canvas-end must be the same live end (ε = Latest band).
  function liveEndDisagreeFail(pinWant, canvasEndPin, epsPx) {
    const eps = epsPx == null ? 16 : Number(epsPx);
    const e = Number.isFinite(eps) && eps >= 0 ? eps : 16;
    return Math.abs((Number(pinWant) || 0) - (Number(canvasEndPin) || 0)) > e;
  }

  // 🎯T494.1: wheel-down from the post-reload pin must not fight.
  // Already at canvas max (dist ≤ 16): a no-op is fine.
  // Else a wheel-down must increase scrollTop; tracking must not snap back.
  const WHEEL_AT_END_PX = 16;
  function wheelDownFoughtFail(dist, before, after, tracking) {
    const d = Number(dist) || 0;
    if (d <= WHEEL_AT_END_PX) return false;
    const grew = (Number(after) || 0) - (Number(before) || 0);
    return !!tracking && grew < 2;
  }

  // Wheel-up then wheel-down snapping back to the initial pin while Latest
  // is visible is the two-bottoms fight (last-bubble pin vs canvas end).
  function wheelRoundTripSnapFail(opts) {
    const o = opts || {};
    const dist = Number(o.dist) || 0;
    if (dist <= WHEEL_AT_END_PX) return false;
    if (o.fabHidden !== false) return false;
    return Math.abs((Number(o.afterRound) || 0) - (Number(o.pinSt) || 0)) < 8;
  }

  function viewportPinned(innerW, innerH, dpr) {
    return innerW === ORACLE_VIEWPORT.width &&
      innerH === ORACLE_VIEWPORT.height &&
      Number(dpr) === ORACLE_DPR;
  }

  function noteMarkers(texts, prefix, into, seen) {
    const p = String(prefix || '');
    if (!p) return;
    (texts || []).forEach(function (text) {
      const s = String(text || '');
      const idx = s.indexOf(p);
      if (idx < 0) return;
      const tok = s.slice(idx).split(/\s/)[0];
      if (!tok || seen[tok]) return;
      seen[tok] = true;
      into.push(tok);
    });
  }

  // Runs in the page. Candidate "should be visible" rows are attached
  // .msg nodes whose box centre sits inside #messages.
  function collect(opts) {
    opts = opts || {};
    const prefix = String(opts.prefix || '');
    const scroller = typeof document !== 'undefined' ? document.getElementById('messages') : null;
    const canvas = typeof document !== 'undefined' ? document.getElementById('messages-canvas') : null;
    const rows = (typeof window !== 'undefined' && Array.isArray(window.__transcriptRows))
      ? window.__transcriptRows : [];
    const attached = canvas ? Array.prototype.slice.call(canvas.querySelectorAll(':scope > .msg')) : [];
    const scrollerRect = scroller ? scroller.getBoundingClientRect() : {
      left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0,
    };

    const visible = [];
    for (let i = 0; i < attached.length; i++) {
      const el = attached[i];
      const g = inspect(el, scrollerRect);
      if (!g.inScroller) continue;
      let role = 'other';
      if (el.classList.contains('user')) role = 'user';
      else if (el.classList.contains('jevons') || el.classList.contains('assistant')) role = 'assistant';
      visible.push({
        role: role,
        text: g.text,
        checkVisibility: g.checkVisibility,
        hitOk: g.hitOk,
        hitTag: g.hitTag,
        w: g.w,
        h: g.h,
        top: g.top,
        bottom: g.bottom,
      });
    }

    const modelMarkers = [];
    const visibleMarkers = [];
    const seenM = Object.create(null);
    const seenV = Object.create(null);
    noteMarkers(rows.map(function (row) { return (row && row.text) || ''; }), prefix, modelMarkers, seenM);
    noteMarkers(visible.map(function (v) { return v.text; }), prefix, visibleMarkers, seenV);

    const visibleInScroller = visible.length;
    const visibleCheckOk = visible.filter(function (v) { return v.checkVisibility; }).length;
    const visibleHitOk = visible.filter(function (v) { return v.hitOk; }).length;
    const bubbleRects = visible.filter(function (v) {
      return v.role === 'user' || v.role === 'assistant';
    });
    const visibleBubbles = bubbleRects.length;
    const maxInkGap = maxInkGapPx(bubbleRects);
    const iw = typeof window !== 'undefined' ? window.innerWidth : 0;
    const ih = typeof window !== 'undefined' ? window.innerHeight : 0;
    const dpr = typeof window !== 'undefined' ? window.devicePixelRatio : 0;

    let emptySlots = 0;
    let labelledSlots = 0;
    for (let i = 0; i < rows.length; i++) {
      const row = rows[i];
      if (!row) continue;
      const kind = row.kind || row.role || '';
      if (kind !== 'turn-slot' && kind !== 'turn-marker') continue;
      if (String(row.text || '').trim()) labelledSlots++;
      else emptySlots++;
    }

    const sh = scroller ? scroller.scrollHeight : 0;
    const ch = scroller ? scroller.clientHeight : 0;
    const st = scroller ? scroller.scrollTop : 0;
    const canvasEndPin = Math.max(0, sh - ch);
    // pinToLiveEnd over-assigns scrollHeight (T351); the engine clamps
    // to sh − ch. Compare the clamped target to canvas-end, not the write.
    const pinWrite = (typeof window !== 'undefined' && typeof window.pinToLiveEnd === 'function')
      ? Number(window.pinToLiveEnd())
      : canvasEndPin;
    const pinWant = Number.isFinite(pinWrite)
      ? Math.max(0, Math.min(pinWrite, canvasEndPin))
      : canvasEndPin;
    const followMode = (typeof window !== 'undefined' && window.followMode)
      ? String(window.followMode)
      : '';
    const fab = typeof document !== 'undefined' ? document.getElementById('jump-bottom') : null;
    const fabHidden = !fab || !!fab.hidden;
    const distFromBottom = sh - ch - st;
    const atBottom = ch <= 0 || distFromBottom <= 16 || Math.abs(st - pinWant) <= 16;

    let lastContentBottom = 0;
    const VL = (typeof window !== 'undefined') ? window.VirtualList : null;
    const layout = (typeof window !== 'undefined') ? window.__transcriptLayout : null;
    if (VL && VL.layoutTop && layout && layout.heights) {
      for (let i = 0; i < rows.length; i++) {
        const row = rows[i];
        if (!row) continue;
        const kind = row.kind || row.role || '';
        const labelled = (kind === 'turn-slot' || kind === 'turn-marker')
          && String(row.text || '').trim();
        const bubble = kind === 'user' || kind === 'jevons' || kind === 'assistant';
        if (!labelled && !bubble) continue;
        const top = VL.layoutTop(layout, i);
        const h = Number(layout.heights[i]) || 0;
        const end = top + h;
        if (end > lastContentBottom) lastContentBottom = end;
      }
    } else if (typeof window !== 'undefined' && typeof window.lastTranscriptBubbleBottom === 'function') {
      lastContentBottom = Number(window.lastTranscriptBubbleBottom()) || 0;
    }
    const canvasH = canvas ? canvas.offsetHeight : 0;
    const voidBelowLast = canvasH > 0 ? canvasH - lastContentBottom : 0;
    const canvasMinHeight = canvas ? (parseFloat(canvas.style.minHeight) || 0) : 0;
    const layoutTotal = (VL && VL.layoutTotal && layout) ? VL.layoutTotal(layout) : 0;

    return {
      innerWidth: iw,
      innerHeight: ih,
      devicePixelRatio: dpr,
      viewportPinned: viewportPinned(iw, ih, dpr),
      modelRows: rows.length,
      attached: attached.length,
      visibleInScroller: visibleInScroller,
      visibleCheckOk: visibleCheckOk,
      visibleHitOk: visibleHitOk,
      visibleBubbles: visibleBubbles,
      visibleTexts: visible.map(function (v) { return v.text; }),
      modelMarkers: modelMarkers,
      visibleMarkers: visibleMarkers,
      emptySlots: emptySlots,
      labelledSlots: labelledSlots,
      emptyPane: emptyPaneFail(rows.length, visibleInScroller),
      emptySlotDesert: emptySlotDesertFail(emptySlots),
      packedPaneFail: packedPaneFail(visibleBubbles, 2),
      maxInkGap: maxInkGap,
      desertGap: desertGapFail(maxInkGap, ch),
      overlappingRects: overlappingRectsFail(bubbleRects),
      latestOnHardReload: latestOnHardReloadFail({
        fabHidden: fabHidden,
        followMode: followMode,
        atBottom: atBottom,
      }),
      liveEndDisagree: liveEndDisagreeFail(pinWant, canvasEndPin, 16),
      lastContentBottom: lastContentBottom,
      voidBelowLast: voidBelowLast,
      voidBelowLastFail: voidBelowLastFail(lastContentBottom, canvasH, VOID_BELOW_VISIBLE_PX),
      canvasMinHeight: canvasMinHeight,
      layoutTotal: layoutTotal,
      canvasRatchet: canvasMinHeight - layoutTotal,
      canvasRatchetFail: canvasRatchetFail(canvasMinHeight, layoutTotal, VOID_BELOW_VISIBLE_PX),
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
    };
  }

  function pinScrollBottom() {
    const scroller = typeof document !== 'undefined' ? document.getElementById('messages') : null;
    if (!scroller) return false;
    scroller.scrollTop = scroller.scrollHeight;
    return true;
  }

  return {
    ORACLE_VIEWPORT: ORACLE_VIEWPORT,
    ORACLE_DPR: ORACLE_DPR,
    boxCentre: boxCentre,
    rectsIntersect: rectsIntersect,
    pointInRect: pointInRect,
    hitTestPasses: hitTestPasses,
    inspect: inspect,
    domOnScreen: domOnScreen,
    VisibilityHelper: VisibilityHelper,
    emptyPaneFail: emptyPaneFail,
    emptyOCRFail: emptyOCRFail,
    latestOnHardReloadFail: latestOnHardReloadFail,
    emptySlotDesertFail: emptySlotDesertFail,
    packedPaneFail: packedPaneFail,
    maxInkGapPx: maxInkGapPx,
    overlappingRectsFail: overlappingRectsFail,
    desertGapFail: desertGapFail,
    DESERT_GAP_FRAC: DESERT_GAP_FRAC,
    DESERT_GAP_MIN_PX: DESERT_GAP_MIN_PX,
    voidBelowLastFail: voidBelowLastFail,
    canvasRatchetFail: canvasRatchetFail,
    VOID_BELOW_EPS_PX: VOID_BELOW_EPS_PX,
    VOID_BELOW_VISIBLE_PX: VOID_BELOW_VISIBLE_PX,
    liveEndDisagreeFail: liveEndDisagreeFail,
    wheelDownFoughtFail: wheelDownFoughtFail,
    wheelRoundTripSnapFail: wheelRoundTripSnapFail,
    WHEEL_AT_END_PX: WHEEL_AT_END_PX,
    viewportPinned: viewportPinned,
    collect: collect,
    pinScrollBottom: pinScrollBottom,
  };
}));
