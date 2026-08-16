// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Viewport census for cockpit on-screen evidence (🎯T493).
//
// Three gates, all required: checkVisibility, centre hit-test (self or
// descendant), Vision OCR of a pinned-viewport screenshot. This module is
// the DOM half (1+2) plus the empty-pane predicate. OCR lives in
// internal/imagetext and is applied by the journey after the screenshot.
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

  // modelRows > 0 with nothing in the scroller is the empty-pane fail
  // (🎯T494): the list claims history and the owner sees a blank.
  function emptyPaneFail(modelRows, visibleInScroller) {
    return (Number(modelRows) || 0) > 0 && (Number(visibleInScroller) || 0) === 0;
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

  // Pin target and canvas-end must be the same live end (ε = Latest band).
  function liveEndDisagreeFail(pinWant, canvasEndPin, epsPx) {
    const eps = epsPx == null ? 16 : Number(epsPx);
    const e = Number.isFinite(eps) && eps >= 0 ? eps : 16;
    return Math.abs((Number(pinWant) || 0) - (Number(canvasEndPin) || 0)) > e;
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
      const r = el.getBoundingClientRect();
      const centre = boxCentre(r);
      if (!pointInRect(centre, scrollerRect)) continue;
      const vis = (typeof el.checkVisibility === 'function')
        ? el.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })
        : (r.width > 0 && r.height > 0);
      const hit = typeof document.elementFromPoint === 'function'
        ? document.elementFromPoint(centre.x, centre.y)
        : null;
      const hitOk = hitTestPasses(hit, el, !!(hit && el.contains(hit)));
      const text = String(el.innerText || el.textContent || '').trim();
      let role = 'other';
      if (el.classList.contains('user')) role = 'user';
      else if (el.classList.contains('jevons') || el.classList.contains('assistant')) role = 'assistant';
      visible.push({
        role: role,
        text: text.slice(0, 160),
        checkVisibility: !!vis,
        hitOk: hitOk,
        hitTag: hit ? String(hit.tagName || '') : '',
        w: r.width,
        h: r.height,
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
    const visibleBubbles = visible.filter(function (v) {
      return v.role === 'user' || v.role === 'assistant';
    }).length;
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
      latestOnHardReload: latestOnHardReloadFail({
        fabHidden: fabHidden,
        followMode: followMode,
        atBottom: atBottom,
      }),
      liveEndDisagree: liveEndDisagreeFail(pinWant, canvasEndPin, 16),
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
      canvasHeight: canvas ? canvas.offsetHeight : 0,
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
    emptyPaneFail: emptyPaneFail,
    latestOnHardReloadFail: latestOnHardReloadFail,
    emptySlotDesertFail: emptySlotDesertFail,
    packedPaneFail: packedPaneFail,
    liveEndDisagreeFail: liveEndDisagreeFail,
    viewportPinned: viewportPinned,
    collect: collect,
    pinScrollBottom: pinScrollBottom,
  };
}));
