// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Instant custom hover tips (🎯T175). Replaces delayed native title= tooltips
// for dense chrome (frontier status / fanout / truncated name).
// Show on pointerenter with 0ms delay — never setTimeout before show.
//
// 🎯T181: optional HTML content + left-of-pointer placement for rich cards.
// Default remains above-host text tips (status/fanout).
//
// 🎯T186: sticky hover — tip stays open while pointer is inside the hit rect.
// Tip receives pointer events so wheel scroll works. Optional maxRight clamp
// so card never covers #frontier-table.
//
// 🎯T187: never auto-timeout while over the card. Nested scroll/wheel must
// not re-arm hide. No setInterval.
//
// 🎯T231 OWNER HARD PIN — NO multi-element bridge:
//   ONE invisible axis-aligned rect = AABB(card ∪ id+name of active row).
//   Leave that rect → dismiss immediately (HIDE_GRACE_MS = 0).
//   Product model is pointInHitRect only — not host+tip+bridge flags/grace.
//   Outside top/bottom of that rect is leave (exit above/below).
//   Host↔card inside the rect does not dismiss; another row → leave/switch.
//   Flicker → fix geometry, never add timeout.
//
// 🎯T230: frontier quiet poll / re-render must not tear down a tip while
// the pointer is latched inside the hit rect (or over host/tip before rect
// is laid out). isHoverLatched / anyHoverLatched skip remount.
//
// 🎯T203: product-wide singleton — at most one InstantTip panel visible.
// Showing a tip for a new host force-hides every other open InstantTip
// (sticky state reset via tip._instantTipForceHide).

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.InstantTip = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Oracle: product tip path must show with zero delay.
  var SHOW_DELAY_MS = 0;
  // 🎯T231: continuous hit-rect → no grace. Product cards always 0.
  var HIDE_GRACE_MS = 0;
  var TIP_CLASS = 'instant-tip';
  var SHOW_CLASS = 'instant-tip-show';
  var HOST_CLASS = 'has-instant-tip';
  var CARD_CLASS = 'instant-tip-card';
  var HIT_LAYER_CLASS = 'instant-tip-hit';
  var PLACE_ABOVE_HOST = 'above-host';
  var PLACE_LEFT_OF_POINTER = 'left-of-pointer';
  var EDGE_PAD = 4;
  var POINTER_GAP = 12;
  var DEFAULT_CLAMP_GAP = 8;
  var DEFAULT_CLAMP_SELECTORS = ['#frontier-table', '#frontier-body'];

  // 🎯T203: tips currently claimed open (product-wide singleton registry).
  var openTips = [];

  // Pure schedule descriptor for hermetic checks (no timer).
  function showSchedule() {
    return {
      delayMs: SHOW_DELAY_MS,
      usesTimeout: false,
      event: 'pointerenter',
    };
  }

  // Pure hide descriptor (🎯T231 single hit-rect).
  // Product continuous path: graceMs 0, no scheduled hide-with-delay.
  function hideSchedule(opts) {
    var o = opts || {};
    var hitGroup = o.hitGroup !== false;
    var grace = 0;
    if (!hitGroup && o.hideGraceMs != null) {
      var g = Number(o.hideGraceMs);
      grace = g >= 0 ? g : 0;
    }
    return {
      graceMs: grace,
      usesTimeout: grace > 0,
      usesInterval: false,
      event: 'pointerleave',
      cancelOn: 'pointerenter',
      model: hitGroup ? 'hit-rect' : 'gap-grace',
      gapOnly: false,
      neverWhileOverTip: true,
      immediateOnLeaveHitGroup: true,
    };
  }

  // Pure: product cards force 0 grace (🎯T231).
  function resolveHideGraceMs(opts) {
    var o = opts || {};
    if (o.hitGroup !== false) return 0;
    if (o.hideGraceMs != null) {
      var h = Number(o.hideGraceMs);
      return h >= 0 ? h : 0;
    }
    return 0;
  }

  // ─── Pure geometry (🎯T231): one AABB hit rect ─────────────────────────

  function normalizeRect(r) {
    if (!r || typeof r !== 'object') return null;
    var left = Number(r.left);
    var top = Number(r.top);
    if (!isFinite(left) || !isFinite(top)) return null;
    var right = r.right != null && isFinite(Number(r.right))
      ? Number(r.right)
      : left + (Number(r.width) || 0);
    var bottom = r.bottom != null && isFinite(Number(r.bottom))
      ? Number(r.bottom)
      : top + (Number(r.height) || 0);
    return { left: left, top: top, right: right, bottom: bottom };
  }

  // Pure: axis-aligned bounding box of rect list. Empty → null.
  function unionHitRect(rects) {
    var list = (rects || []).map(normalizeRect).filter(Boolean);
    if (!list.length) return null;
    var left = list[0].left;
    var top = list[0].top;
    var right = list[0].right;
    var bottom = list[0].bottom;
    for (var i = 1; i < list.length; i++) {
      left = Math.min(left, list[i].left);
      top = Math.min(top, list[i].top);
      right = Math.max(right, list[i].right);
      bottom = Math.max(bottom, list[i].bottom);
    }
    return { left: left, top: top, right: right, bottom: bottom };
  }

  // Pure: point inside one hit rect (🎯T231 product predicate).
  function pointInHitRect(x, y, rect) {
    var r = normalizeRect(rect);
    if (!r) return false;
    var px = Number(x);
    var py = Number(y);
    if (!isFinite(px) || !isFinite(py)) return false;
    return px >= r.left && px <= r.right && py >= r.top && py <= r.bottom;
  }

  // Pure: build the single product hit rect from card + host cells.
  // args: { cardRect, hostRects, tableRect? }
  // ONE AABB encompassing card ∪ id+name. No bridge. Optional vertical clip
  // to table so exit above table top / below bottom is outside when the
  // union would otherwise extend past table (hosts already row-band).
  function computeHitRect(args) {
    var a = args || {};
    var parts = [];
    var card = normalizeRect(a.cardRect || a.tipRect);
    if (card) parts.push(card);
    var hosts = a.hostRects || [];
    for (var i = 0; i < hosts.length; i++) {
      var h = normalizeRect(hosts[i]);
      if (h) parts.push(h);
    }
    var rect = unionHitRect(parts);
    if (!rect) return null;
    var table = normalizeRect(a.tableRect);
    if (table) {
      // Clip only if the union extends past table vertically on the table
      // side without the card holding that extent. Owner: outside top/bottom
      // of the hit rect is leave — pure AABB already defines top/bottom.
      // Do not invent a taller strip; table clip shrinks host contribution
      // only when hosts fall outside table (degenerate). Keep card extent.
      // Product: leave rect as AABB(card ∪ hosts) — no vertical invent.
    }
    return rect;
  }

  // Pure: dismiss when pointer is outside the hit rect.
  function shouldDismissOutsideHitRect(x, y, rect) {
    return !pointInHitRect(x, y, rect);
  }

  // Pure hover state for latch / scheduled-hide helpers.
  function isInsideHitGroup(state) {
    var s = state || {};
    if (s.insideHitRect) return true;
    return !!(s.overHost || s.overTip);
  }

  function shouldDismissOnLeaveHitGroup(state) {
    return !isInsideHitGroup(state);
  }

  // Pure: scheduled hide never runs while latched inside (legacy + T187).
  function shouldRunScheduledHide(state) {
    if (isInsideHitGroup(state)) return false;
    return true;
  }

  function shouldScheduleHideOnHostLeave(state) {
    if (state && state.overTip) return false;
    if (state && state.insideHitRect) return false;
    return true;
  }

  function shouldScheduleHideOnTipLeave(state) {
    if (state && (state.overHost || state.overTip || state.insideHitRect)) return false;
    return true;
  }

  function isHoverLatchedState(state, visible) {
    if (!visible) return false;
    return isInsideHitGroup(state);
  }

  // relatedTarget still inside el? Nested children / scroll chrome (🎯T187).
  function relatedStillInside(el, related) {
    if (!el || !related) return false;
    if (el === related) return true;
    if (typeof el.contains === 'function') {
      try {
        return el.contains(related);
      } catch (_) {
        return false;
      }
    }
    return false;
  }

  function tipTextOrEmpty(text) {
    if (text == null) return '';
    return String(text).trim();
  }

  // placeLeftOfPointerRect — pure placement math for 🎯T181/T186 cards.
  function placeLeftOfPointerRect(args) {
    var a = args || {};
    var px = Number(a.pointerX) || 0;
    var py = Number(a.pointerY) || 0;
    var tw = Math.max(0, Number(a.tipW) || 0);
    var th = Math.max(0, Number(a.tipH) || 0);
    var vw = Math.max(0, Number(a.viewW) || 0);
    var vh = Math.max(0, Number(a.viewH) || 0);
    var gap = a.gap != null ? Number(a.gap) : POINTER_GAP;
    var pad = a.pad != null ? Number(a.pad) : EDGE_PAD;
    var hasMaxRight = a.maxRight != null && isFinite(Number(a.maxRight));
    var maxRight = hasMaxRight ? Number(a.maxRight) : null;

    var side = 'left';
    var left = px - gap - tw;
    var maxWidth = null;

    if (hasMaxRight) {
      if (left + tw > maxRight) {
        left = maxRight - tw;
      }
      if (left < pad) {
        left = pad;
        var avail = Math.max(0, maxRight - pad);
        if (avail > 0 && (tw <= 0 || avail < tw)) {
          maxWidth = Math.floor(avail);
          tw = maxWidth;
          left = maxRight - tw;
          if (left < pad) left = pad;
        } else if (avail <= 0) {
          left = pad;
          maxWidth = 0;
        }
      }
    } else {
      if (left < pad) {
        side = 'right';
        left = px + gap;
      }
      if (vw > 0) {
        if (left + tw > vw - pad) left = Math.max(pad, vw - pad - tw);
        if (left < pad) left = pad;
      }
    }

    if (hasMaxRight && vw > 0) {
      if (left < pad) left = pad;
      if (left + tw > vw - pad) {
        left = Math.max(pad, Math.min(left, vw - pad - tw));
      }
    }

    var top = py - th / 2;
    if (vh > 0) {
      if (top + th > vh - pad) top = Math.max(pad, vh - pad - th);
      if (top < pad) top = pad;
    } else if (top < pad) {
      top = pad;
    }

    var out = {
      left: Math.round(left),
      top: Math.round(top),
      side: side,
    };
    if (maxWidth != null) out.maxWidth = maxWidth;
    return out;
  }

  function resolveClampRight(args) {
    var a = args || {};
    if (a.maxRight != null && isFinite(Number(a.maxRight))) {
      return Number(a.maxRight);
    }
    var gap = a.clampGap != null ? Number(a.clampGap)
      : (a.gap != null ? Number(a.gap) : DEFAULT_CLAMP_GAP);
    if (a.clampRect && typeof a.clampRect.left === 'number') {
      return a.clampRect.left - gap;
    }
    var el = a.clampEl || null;
    if (!el && a.doc && typeof a.doc.querySelector === 'function') {
      var sels = a.clampSelectors || DEFAULT_CLAMP_SELECTORS;
      if (typeof sels === 'string') sels = [sels];
      for (var i = 0; i < sels.length; i++) {
        try {
          el = a.doc.querySelector(sels[i]);
        } catch (_) {
          el = null;
        }
        if (el) break;
      }
    }
    if (el && typeof el.getBoundingClientRect === 'function') {
      var r = el.getBoundingClientRect();
      if (r && typeof r.left === 'number') return r.left - gap;
    }
    return null;
  }

  function placeTip(tip, host) {
    if (!tip || !host || typeof host.getBoundingClientRect !== 'function') return;
    var r = host.getBoundingClientRect();
    var tw = tip.offsetWidth || 0;
    var th = tip.offsetHeight || 0;
    var left = r.left;
    var top = r.top - th - 4;
    if (top < 4) top = r.bottom + 4;
    if (left + tw > (typeof window !== 'undefined' && window.innerWidth
      ? window.innerWidth - 4 : left + tw)) {
      left = Math.max(4, (typeof window !== 'undefined' && window.innerWidth
        ? window.innerWidth - tw - 4 : left));
    }
    if (left < 4) left = 4;
    tip.style.left = Math.round(left) + 'px';
    tip.style.top = Math.round(top) + 'px';
  }

  function placeTipLeftOfPointer(tip, event, opts) {
    if (!tip) return null;
    var o = opts || {};
    var px = 0;
    var py = 0;
    if (event) {
      if (typeof event.clientX === 'number') px = event.clientX;
      if (typeof event.clientY === 'number') py = event.clientY;
    }
    if ((!px && !py) && o.host && typeof o.host.getBoundingClientRect === 'function') {
      var r = o.host.getBoundingClientRect();
      px = r.left + (r.width || 0) / 2;
      py = r.top + (r.height || 0) / 2;
    }
    var vw = (typeof window !== 'undefined' && window.innerWidth) ? window.innerWidth : 0;
    var vh = (typeof window !== 'undefined' && window.innerHeight) ? window.innerHeight : 0;
    var tipW = tip.offsetWidth || 0;
    var tipH = tip.offsetHeight || 0;

    var maxRight = resolveClampRight({
      maxRight: o.maxRight,
      clampEl: o.clampEl,
      clampRect: o.clampRect,
      clampSelectors: o.clampSelectors,
      clampGap: o.clampGap,
      gap: o.gap,
      doc: o.doc || (tip.ownerDocument || null),
    });

    if (maxRight != null) {
      var pad = o.pad != null ? Number(o.pad) : EDGE_PAD;
      var avail = Math.max(0, maxRight - pad);
      if (avail > 0 && tipW > avail && tip.style) {
        tip.style.maxWidth = Math.floor(avail) + 'px';
        tipW = tip.offsetWidth || avail;
        tipH = tip.offsetHeight || tipH;
      }
    }

    var pos = placeLeftOfPointerRect({
      pointerX: px,
      pointerY: py,
      tipW: tipW,
      tipH: tipH,
      viewW: vw,
      viewH: vh,
      gap: o.gap,
      pad: o.pad,
      maxRight: maxRight,
    });
    if (tip.style) {
      tip.style.left = pos.left + 'px';
      tip.style.top = pos.top + 'px';
      if (pos.maxWidth != null) {
        tip.style.maxWidth = pos.maxWidth + 'px';
      }
    }
    return pos;
  }

  function claimOpen(tip) {
    if (!tip) return;
    for (var i = 0; i < openTips.length; i++) {
      if (openTips[i] === tip) return;
    }
    openTips.push(tip);
  }

  function releaseOpen(tip) {
    if (!tip) return;
    for (var i = openTips.length - 1; i >= 0; i--) {
      if (openTips[i] === tip) openTips.splice(i, 1);
    }
  }

  function forceHideTip(tip) {
    if (!tip) return false;
    if (typeof tip._instantTipForceHide === 'function') {
      tip._instantTipForceHide();
      return true;
    }
    return hideTip(tip);
  }

  function dismissOtherTips(keep) {
    var snapshot = openTips.slice();
    var n = 0;
    for (var i = 0; i < snapshot.length; i++) {
      var t = snapshot[i];
      if (!t || t === keep) continue;
      forceHideTip(t);
      n++;
    }
    return n;
  }

  function openTipsCount() {
    return openTips.length;
  }

  function getOpenTips() {
    return openTips.slice();
  }

  function isHoverLatched(tip) {
    if (!tip) return false;
    if (!isVisible(tip)) return false;
    var s = null;
    if (typeof tip._instantTipHoverState === 'function') {
      try {
        s = tip._instantTipHoverState();
      } catch (_) {
        s = null;
      }
    }
    if (!s) return true;
    return isHoverLatchedState(s, true);
  }

  function anyHoverLatched() {
    for (var i = 0; i < openTips.length; i++) {
      if (isHoverLatched(openTips[i])) return true;
    }
    return false;
  }

  function discardDetachedTips(doc) {
    var d = doc || (typeof document !== 'undefined' ? document : null);
    var removed = 0;
    var preserved = 0;
    if (!d || typeof d.querySelectorAll !== 'function') {
      return { removed: 0, preserved: 0 };
    }
    var nodes = d.querySelectorAll('.' + TIP_CLASS);
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      if (!el) continue;
      if (isHoverLatched(el)) {
        preserved++;
        continue;
      }
      forceHideTip(el);
      if (el.parentNode) {
        try {
          el.parentNode.removeChild(el);
        } catch (_) { /* detached already */ }
      }
      removed++;
    }
    return { removed: removed, preserved: preserved };
  }

  function showTip(tip, host, opts) {
    if (!tip) return false;
    dismissOtherTips(tip);
    if (tip.style) {
      tip.style.display = 'block';
      tip.style.pointerEvents = 'auto';
    }
    if (tip.classList && tip.classList.add) tip.classList.add(SHOW_CLASS);
    else if (typeof tip.className === 'string' && tip.className.indexOf(SHOW_CLASS) < 0) {
      tip.className = (tip.className ? tip.className + ' ' : '') + SHOW_CLASS;
    }
    var o = opts || {};
    var placement = o.placement || PLACE_ABOVE_HOST;
    if (placement === PLACE_LEFT_OF_POINTER) {
      placeTipLeftOfPointer(tip, o.event, {
        host: host || o.host,
        gap: o.gap,
        pad: o.pad,
        maxRight: o.maxRight,
        clampEl: o.clampEl,
        clampRect: o.clampRect,
        clampSelectors: o.clampSelectors,
        clampGap: o.clampGap,
        doc: o.doc,
      });
    } else if (host) {
      placeTip(tip, host);
    }
    claimOpen(tip);
    return true;
  }

  function hideTip(tip) {
    if (!tip) return false;
    releaseOpen(tip);
    if (tip.style) {
      tip.style.display = 'none';
      tip.style.pointerEvents = 'none';
    }
    if (tip.classList && tip.classList.remove) tip.classList.remove(SHOW_CLASS);
    else if (typeof tip.className === 'string') {
      tip.className = tip.className.replace(new RegExp('\\b' + SHOW_CLASS + '\\b', 'g'), '').trim();
    }
    return true;
  }

  function isVisible(tip) {
    if (!tip) return false;
    if (tip.classList && tip.classList.contains) return tip.classList.contains(SHOW_CLASS);
    if (typeof tip.className === 'string') {
      return (' ' + tip.className + ' ').indexOf(' ' + SHOW_CLASS + ' ') >= 0;
    }
    return tip.style && tip.style.display === 'block';
  }

  // Layout the single invisible hit-layer element from a rect.
  function applyHitLayerLayout(layer, rect) {
    if (!layer || !layer.style) return;
    var r = normalizeRect(rect);
    if (!r) {
      layer.style.display = 'none';
      layer.style.pointerEvents = 'none';
      return;
    }
    var w = Math.max(0, r.right - r.left);
    var h = Math.max(0, r.bottom - r.top);
    layer.style.display = w > 0 && h > 0 ? 'block' : 'none';
    layer.style.position = 'fixed';
    layer.style.left = Math.round(r.left) + 'px';
    layer.style.top = Math.round(r.top) + 'px';
    layer.style.width = Math.round(w) + 'px';
    layer.style.height = Math.round(h) + 'px';
    layer.style.pointerEvents = 'none'; // sampling only; tip/hosts receive events
    layer.style.background = 'transparent';
    layer.style.zIndex = '198';
    layer.style.border = 'none';
    layer.style.padding = '0';
    layer.style.margin = '0';
  }

  // attach(host, text, opts) — custom tip on host; strips native title=; 0ms show.
  //
  // opts:
  //   doc, mount — document / mount node (tests inject mocks)
  //   html — when true, set innerHTML instead of textContent (rich cards)
  //   ariaLabel — plain string for aria-label
  //   placement — 'above-host' (default) | 'left-of-pointer' (🎯T181)
  //   className — extra class on tip (e.g. instant-tip-card)
  //   sticky — default true: stay open while inside hit rect
  //   hitGroup — 🎯T231 default true for sticky: single hit-rect, grace 0
  //   groupHosts — id + name cells of the active row (same hit rect)
  //   hideGraceMs — only when hitGroup:false (legacy tests); product = 0
  //   timers — { setTimeout, clearTimeout } for hermetic tests
  //   maxRight / clampEl / clampSelectors / clampGap — 🎯T186 table clamp
  //   tableEl / tableRect — optional table bounds for hermetic inject
  //   hitRect — optional pure inject for hermetic hit-rect tests
  function attach(host, text, opts) {
    var label = tipTextOrEmpty(text);
    if (!host || !label) return null;
    var o = opts || {};
    var doc = o.doc || host.ownerDocument || (typeof document !== 'undefined' ? document : null);
    if (!doc || typeof doc.createElement !== 'function') return null;

    if (typeof host.removeAttribute === 'function') host.removeAttribute('title');
    else if ('title' in host) host.title = '';

    var aria = tipTextOrEmpty(o.ariaLabel) || label.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim();
    if (typeof host.setAttribute === 'function') {
      host.setAttribute('aria-label', aria);
    }
    if (host.classList && host.classList.add) host.classList.add(HOST_CLASS);
    else if (typeof host.className === 'string' && host.className.indexOf(HOST_CLASS) < 0) {
      host.className = (host.className ? host.className + ' ' : '') + HOST_CLASS;
    }

    var tip = doc.createElement('div');
    var extra = tipTextOrEmpty(o.className);
    tip.className = TIP_CLASS + (extra ? (' ' + extra) : '');
    tip.setAttribute('role', 'tooltip');
    if (o.html) {
      tip.innerHTML = label;
    } else {
      tip.textContent = label;
    }
    tip.style.display = 'none';
    tip.style.position = 'fixed';
    tip.style.zIndex = '200';
    tip.style.pointerEvents = 'none';

    var mount = o.mount || (doc.body || null);
    if (mount && typeof mount.appendChild === 'function') {
      mount.appendChild(tip);
    }

    var placement = o.placement || PLACE_ABOVE_HOST;
    var sticky = o.sticky !== false;
    // 🎯T231: hit-rect product path default for sticky; grace always 0.
    var useHitGroup = o.hitGroup != null ? !!o.hitGroup : sticky;
    var graceMs = resolveHideGraceMs({ hitGroup: useHitGroup, hideGraceMs: o.hideGraceMs });
    if (useHitGroup) graceMs = 0;

    var timers = o.timers || null;
    var schedule = timers && typeof timers.setTimeout === 'function'
      ? timers.setTimeout
      : (typeof setTimeout === 'function' ? setTimeout : function (fn) { fn(); return 0; });
    var cancel = timers && typeof timers.clearTimeout === 'function'
      ? timers.clearTimeout
      : (typeof clearTimeout === 'function' ? clearTimeout : function () {});

    // Group hosts: primary + optional siblings (id + name).
    var groupHosts = [];
    if (o.groupHosts && o.groupHosts.length) {
      for (var gi = 0; gi < o.groupHosts.length; gi++) {
        if (o.groupHosts[gi]) groupHosts.push(o.groupHosts[gi]);
      }
    }
    if (groupHosts.indexOf(host) < 0) groupHosts.unshift(host);
    for (var hj = 0; hj < groupHosts.length; hj++) {
      var gh = groupHosts[hj];
      if (gh && gh !== host) {
        if (typeof gh.removeAttribute === 'function') gh.removeAttribute('title');
        if (gh.classList && gh.classList.add) gh.classList.add(HOST_CLASS);
        if (typeof gh.setAttribute === 'function' && aria) {
          gh.setAttribute('aria-label', aria);
        }
      }
    }

    // Single invisible hit-layer (geometry debug / hermetic inspect only).
    // pointer-events:none — product predicate is pointInHitRect on sample.
    var hitLayer = null;
    if (useHitGroup && sticky && typeof doc.createElement === 'function') {
      hitLayer = doc.createElement('div');
      hitLayer.className = HIT_LAYER_CLASS;
      hitLayer.setAttribute('aria-hidden', 'true');
      hitLayer.style.display = 'none';
      hitLayer.style.pointerEvents = 'none';
      if (mount && typeof mount.appendChild === 'function') {
        mount.appendChild(hitLayer);
      }
    }

    var overHost = false;
    var overTip = false;
    var tipEngaged = false;
    var hitRect = o.hitRect ? normalizeRect(o.hitRect) : null;
    var insideHitRect = false;
    var hideTimer = null;
    var tracking = false;
    var sampleTarget = null; // doc or window for pointermove

    function clearHideTimer() {
      if (hideTimer != null) {
        cancel(hideTimer);
        hideTimer = null;
      }
    }

    function hoverState() {
      // insideHitRect is the product latch for hit-group mode.
      var inside = useHitGroup
        ? !!(insideHitRect || overHost || overTip)
        : !!(overHost || overTip);
      return {
        overHost: overHost,
        overTip: overTip,
        tipEngaged: tipEngaged,
        insideHitRect: useHitGroup ? insideHitRect : inside,
        insideHitGroup: inside,
      };
    }

    function readCardRect() {
      if (typeof tip.getBoundingClientRect === 'function') {
        try {
          var r = tip.getBoundingClientRect();
          if (r && (r.width || r.height || r.right > r.left)) return r;
        } catch (_) { /* mock */ }
      }
      // Mock / pre-layout: synthesize from style + offset.
      if (tip.style) {
        var left = parseFloat(tip.style.left) || 0;
        var top = parseFloat(tip.style.top) || 0;
        var w = tip.offsetWidth || 0;
        var h = tip.offsetHeight || 0;
        return { left: left, top: top, right: left + w, bottom: top + h, width: w, height: h };
      }
      return null;
    }

    function readHostRects() {
      var out = [];
      for (var i = 0; i < groupHosts.length; i++) {
        var h = groupHosts[i];
        if (h && typeof h.getBoundingClientRect === 'function') {
          out.push(h.getBoundingClientRect());
        }
      }
      return out;
    }

    function resolveTableRect() {
      if (o.tableRect) return normalizeRect(o.tableRect);
      var el = o.tableEl || null;
      if (!el && doc && typeof doc.querySelector === 'function') {
        var sels = o.clampSelectors || DEFAULT_CLAMP_SELECTORS;
        if (typeof sels === 'string') sels = [sels];
        for (var i = 0; i < sels.length; i++) {
          try {
            el = doc.querySelector(sels[i]);
          } catch (_) {
            el = null;
          }
          if (el) break;
        }
      }
      if (el && typeof el.getBoundingClientRect === 'function') {
        return normalizeRect(el.getBoundingClientRect());
      }
      return null;
    }

    function recomputeHitRect() {
      if (o.hitRect) {
        hitRect = normalizeRect(o.hitRect);
      } else {
        hitRect = computeHitRect({
          cardRect: readCardRect(),
          hostRects: readHostRects(),
          tableRect: resolveTableRect(),
        });
      }
      if (hitLayer) applyHitLayerLayout(hitLayer, hitRect);
      return hitRect;
    }

    function stopTracking() {
      if (!tracking) return;
      tracking = false;
      var t = sampleTarget;
      sampleTarget = null;
      if (t && typeof t.removeEventListener === 'function') {
        t.removeEventListener('pointermove', onPointerSample);
        t.removeEventListener('mousemove', onPointerSample);
      }
    }

    function startTracking() {
      if (tracking || !useHitGroup) return;
      tracking = true;
      // Prefer doc; fall back to host ownerDocument.
      sampleTarget = doc;
      if (sampleTarget && typeof sampleTarget.addEventListener === 'function') {
        sampleTarget.addEventListener('pointermove', onPointerSample);
        sampleTarget.addEventListener('mousemove', onPointerSample);
      }
    }

    function doHide() {
      clearHideTimer();
      stopTracking();
      tipEngaged = false;
      overTip = false;
      overHost = false;
      insideHitRect = false;
      hitRect = o.hitRect ? normalizeRect(o.hitRect) : null;
      if (hitLayer && hitLayer.style) {
        hitLayer.style.display = 'none';
        hitLayer.style.pointerEvents = 'none';
      }
      hideTip(tip);
    }

    tip._instantTipForceHide = doHide;

    function placeOpts(ev) {
      return {
        placement: placement,
        event: ev,
        host: host,
        gap: o.gap,
        pad: o.pad,
        maxRight: o.maxRight,
        clampEl: o.clampEl,
        clampRect: o.clampRect,
        clampSelectors: o.clampSelectors || (placement === PLACE_LEFT_OF_POINTER
          ? DEFAULT_CLAMP_SELECTORS
          : null),
        clampGap: o.clampGap,
        doc: doc,
      };
    }

    // 🎯T231: sample pointer against the ONE hit rect — dismiss immediately
    // when outside. No grace. No multi-flag bridge.
    function samplePointer(x, y) {
      if (!isVisible(tip)) return false;
      recomputeHitRect();
      var inside = pointInHitRect(x, y, hitRect);
      insideHitRect = inside;
      if (inside) {
        clearHideTimer();
        return true;
      }
      // Outside hit rect → dismiss now (grace 0).
      doHide();
      return false;
    }

    function onPointerSample(ev) {
      if (!ev) return;
      var x = typeof ev.clientX === 'number' ? ev.clientX : null;
      var y = typeof ev.clientY === 'number' ? ev.clientY : null;
      if (x == null || y == null) return;
      samplePointer(x, y);
    }

    function onHostEnter(ev) {
      overHost = true;
      clearHideTimer();
      if (SHOW_DELAY_MS > 0) return;
      showTip(tip, host, placeOpts(ev));
      recomputeHitRect();
      if (useHitGroup) {
        startTracking();
        if (ev && typeof ev.clientX === 'number') {
          insideHitRect = pointInHitRect(ev.clientX, ev.clientY, hitRect);
        } else {
          insideHitRect = true; // host enter without coords — treat latched
        }
      }
    }

    function onHostLeave(ev) {
      overHost = false;
      if (useHitGroup) {
        // Product: leave only if sample is outside hit rect. Host leave alone
        // into the card (still inside AABB) must not dismiss.
        var x = ev && typeof ev.clientX === 'number' ? ev.clientX : null;
        var y = ev && typeof ev.clientY === 'number' ? ev.clientY : null;
        // relatedTarget inside tip → stay (pointer path into card).
        var related = ev && (ev.relatedTarget != null ? ev.relatedTarget : ev.toElement);
        if (relatedStillInside(tip, related)) {
          overTip = true;
          tipEngaged = true;
          insideHitRect = true;
          clearHideTimer();
          return;
        }
        for (var i = 0; i < groupHosts.length; i++) {
          if (relatedStillInside(groupHosts[i], related)) {
            overHost = true;
            insideHitRect = true;
            clearHideTimer();
            return;
          }
        }
        if (x != null && y != null) {
          samplePointer(x, y);
          return;
        }
        // No coords: recompute; if we have a hit rect, stay open until next
        // sample (pointermove will dismiss). Do not schedule grace.
        recomputeHitRect();
        // Without coords and not related to tip/host — treat as leave rect
        // for hermetic pointerleave dispatches (tests omit clientX).
        if (!related) {
          insideHitRect = false;
          doHide();
        }
        return;
      }
      // Legacy non-hitGroup sticky path (tests may opt out).
      if (!shouldScheduleHideOnHostLeave(hoverState())) {
        clearHideTimer();
        return;
      }
      requestHideLegacy();
    }

    function onTipEnter(ev) {
      overTip = true;
      tipEngaged = true;
      insideHitRect = true;
      clearHideTimer();
      if (!isVisible(tip)) {
        showTip(tip, host, placeOpts(ev || null));
      }
      recomputeHitRect();
      if (useHitGroup) startTracking();
    }

    function onTipLeave(ev) {
      // Nested children still inside tip → ignore.
      var related = ev && (ev.relatedTarget != null ? ev.relatedTarget : ev.toElement);
      if (relatedStillInside(tip, related)) return;
      overTip = false;
      if (useHitGroup) {
        for (var i = 0; i < groupHosts.length; i++) {
          if (relatedStillInside(groupHosts[i], related)) {
            overHost = true;
            insideHitRect = true;
            clearHideTimer();
            return;
          }
        }
        var x = ev && typeof ev.clientX === 'number' ? ev.clientX : null;
        var y = ev && typeof ev.clientY === 'number' ? ev.clientY : null;
        if (x != null && y != null) {
          samplePointer(x, y);
          return;
        }
        if (!related) {
          insideHitRect = false;
          doHide();
        }
        return;
      }
      if (relatedStillInside(host, related)) {
        overHost = true;
        clearHideTimer();
        return;
      }
      if (!shouldScheduleHideOnTipLeave(hoverState())) {
        clearHideTimer();
        return;
      }
      requestHideLegacy();
    }

    // Legacy delayed hide only when hitGroup:false and graceMs > 0.
    function requestHideLegacy() {
      if (sticky) {
        if (!shouldRunScheduledHide(hoverState())) {
          clearHideTimer();
          return;
        }
        if (graceMs <= 0) {
          clearHideTimer();
          doHide();
          return;
        }
        clearHideTimer();
        hideTimer = schedule(function () {
          hideTimer = null;
          if (!shouldRunScheduledHide(hoverState())) return;
          doHide();
        }, graceMs);
        return;
      }
      doHide();
    }

    for (var wi = 0; wi < groupHosts.length; wi++) {
      var wh = groupHosts[wi];
      if (wh && typeof wh.addEventListener === 'function') {
        wh.addEventListener('pointerenter', onHostEnter);
        wh.addEventListener('pointerleave', onHostLeave);
        wh.addEventListener('mouseenter', onHostEnter);
        wh.addEventListener('mouseleave', onHostLeave);
      }
    }

    if (sticky && typeof tip.addEventListener === 'function') {
      tip.addEventListener('pointerenter', onTipEnter);
      tip.addEventListener('pointerleave', onTipLeave);
      tip.addEventListener('mouseenter', onTipEnter);
      tip.addEventListener('mouseleave', onTipLeave);
      tip.addEventListener('wheel', function () {
        overTip = true;
        tipEngaged = true;
        insideHitRect = true;
        clearHideTimer();
      }, { passive: true });
    }

    host._instantTip = tip;
    host._instantTipShow = onHostEnter;
    host._instantTipHide = onHostLeave;
    host._instantTipPlacement = placement;
    host._instantTipSticky = sticky;
    host._instantTipHitGroup = useHitGroup;
    tip._instantTipOnEnter = onTipEnter;
    tip._instantTipOnLeave = onTipLeave;
    tip._instantTipHoverState = hoverState;
    tip._instantTipGroupHosts = groupHosts.slice();
    tip._instantTipHitLayer = hitLayer;
    tip._instantTipGetHitRect = function () { return hitRect ? {
      left: hitRect.left, top: hitRect.top, right: hitRect.right, bottom: hitRect.bottom,
    } : null; };
    tip._instantTipRecomputeHitRect = recomputeHitRect;
    tip._instantTipSamplePointer = samplePointer;
    tip._instantTipSetHitRect = function (r) {
      hitRect = normalizeRect(r);
      o.hitRect = hitRect;
      if (hitLayer) applyHitLayerLayout(hitLayer, hitRect);
    };
    return tip;
  }

  return {
    SHOW_DELAY_MS: SHOW_DELAY_MS,
    HIDE_GRACE_MS: HIDE_GRACE_MS,
    TIP_CLASS: TIP_CLASS,
    SHOW_CLASS: SHOW_CLASS,
    HOST_CLASS: HOST_CLASS,
    CARD_CLASS: CARD_CLASS,
    HIT_LAYER_CLASS: HIT_LAYER_CLASS,
    PLACE_ABOVE_HOST: PLACE_ABOVE_HOST,
    PLACE_LEFT_OF_POINTER: PLACE_LEFT_OF_POINTER,
    DEFAULT_CLAMP_GAP: DEFAULT_CLAMP_GAP,
    DEFAULT_CLAMP_SELECTORS: DEFAULT_CLAMP_SELECTORS,
    showSchedule: showSchedule,
    hideSchedule: hideSchedule,
    resolveHideGraceMs: resolveHideGraceMs,
    normalizeRect: normalizeRect,
    unionHitRect: unionHitRect,
    pointInHitRect: pointInHitRect,
    computeHitRect: computeHitRect,
    shouldDismissOutsideHitRect: shouldDismissOutsideHitRect,
    isInsideHitGroup: isInsideHitGroup,
    shouldDismissOnLeaveHitGroup: shouldDismissOnLeaveHitGroup,
    shouldRunScheduledHide: shouldRunScheduledHide,
    shouldScheduleHideOnHostLeave: shouldScheduleHideOnHostLeave,
    shouldScheduleHideOnTipLeave: shouldScheduleHideOnTipLeave,
    isHoverLatchedState: isHoverLatchedState,
    isHoverLatched: isHoverLatched,
    anyHoverLatched: anyHoverLatched,
    discardDetachedTips: discardDetachedTips,
    relatedStillInside: relatedStillInside,
    tipTextOrEmpty: tipTextOrEmpty,
    placeLeftOfPointerRect: placeLeftOfPointerRect,
    resolveClampRight: resolveClampRight,
    placeTipLeftOfPointer: placeTipLeftOfPointer,
    showTip: showTip,
    hideTip: hideTip,
    forceHideTip: forceHideTip,
    dismissOtherTips: dismissOtherTips,
    openTipsCount: openTipsCount,
    getOpenTips: getOpenTips,
    isVisible: isVisible,
    attach: attach,
  };
}));
