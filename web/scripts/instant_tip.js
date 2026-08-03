// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Instant custom hover tips (🎯T175). Replaces delayed native title= tooltips
// for dense chrome (frontier status / fanout / truncated name).
// Show on pointerenter with 0ms delay — never setTimeout before show.
//
// 🎯T181: optional HTML content + left-of-pointer placement for rich cards.
// Default remains above-host text tips (status/fanout).
//
// 🎯T186: sticky hover surface — tip stays open while pointer is over host OR
// tip (grace hide on leave both). Tip receives pointer events so wheel scroll
// works. Optional maxRight clamp so card never covers #frontier-table.
//
// 🎯T187: never auto-timeout while over the card. Gap grace is only for the
// host→tip bridge (not an idle timer). After the tip is entered, leave-host
// alone does not dismiss; hide only on leave-tip (or leave both without tip
// engagement). Nested scroll/wheel must not re-arm hide. No setInterval.
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
  // 🎯T186/T187: grace only to cross host→tip gap (not idle auto-hide).
  // 100ms was too short — pointer can take longer; 300–500ms bridge window.
  var HIDE_GRACE_MS = 400;
  var TIP_CLASS = 'instant-tip';
  var SHOW_CLASS = 'instant-tip-show';
  var HOST_CLASS = 'has-instant-tip';
  var CARD_CLASS = 'instant-tip-card';
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

  // Pure hide-grace descriptor (🎯T186/T187).
  // Grace is host→tip bridge only; cancel on tip enter; never idle timeout.
  function hideSchedule() {
    return {
      graceMs: HIDE_GRACE_MS,
      usesTimeout: true,
      usesInterval: false,
      event: 'pointerleave',
      cancelOn: 'pointerenter',
      gapOnly: true,
      neverWhileOverTip: true,
    };
  }

  // Pure: should a scheduled hide run given hover flags? (🎯T187)
  // While overTip (or overHost), hide must not run — no wall-clock dismiss.
  function shouldRunScheduledHide(state) {
    var s = state || {};
    if (s.overHost || s.overTip) return false;
    return true;
  }

  // Pure: host leave policy (🎯T187).
  // - overTip: never schedule (still on card)
  // - tipEngaged: host leave alone does not dismiss; tip leave owns dismiss
  // - else: schedule gap grace for host→tip bridge
  function shouldScheduleHideOnHostLeave(state) {
    var s = state || {};
    if (s.overTip) return false;
    if (s.tipEngaged) return false;
    return true;
  }

  // Pure: tip leave policy — hide only when not over host (or after grace).
  function shouldScheduleHideOnTipLeave(state) {
    var s = state || {};
    if (s.overHost || s.overTip) return false;
    return true;
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

  // Pure: given empty text, attach is a no-op.
  function tipTextOrEmpty(text) {
    if (text == null) return '';
    var s = String(text).trim();
    return s;
  }

  // placeLeftOfPointerRect — pure placement math for 🎯T181/T186 cards.
  // Prefer left of pointer, vertically centered on pointer.
  // If not enough room on the left (and no maxRight clamp), flip to the right.
  // Clamp to viewport so the card stays on-screen (near edges residual: clamp).
  //
  // 🎯T186 maxRight: card.right must be ≤ maxRight (e.g. frontier table left − gap).
  // Prefer left-of-pointer under that constraint; shrink maxWidth when needed.
  // Flip residual: when maxRight is set, do not flip over the table — shrink instead.
  //
  // args: { pointerX, pointerY, tipW, tipH, viewW, viewH, gap?, pad?, maxRight? }
  // returns: { left, top, side: 'left'|'right', maxWidth?: number }
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
      // Prefer left-of-pointer, but never cover the clamp region (table).
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
          // No room left of table: pin at pad with zero-width residual.
          left = pad;
          maxWidth = 0;
        }
      }
      // Do not flip to the right of the pointer when clamping to a table —
      // that would place the card over the frontier (owner residual: shrink).
    } else {
      if (left < pad) {
        // Flip to right of pointer when left would clip.
        side = 'right';
        left = px + gap;
      }
      // Clamp horizontally if still overflowing (narrow viewport residual).
      if (vw > 0) {
        if (left + tw > vw - pad) left = Math.max(pad, vw - pad - tw);
        if (left < pad) left = pad;
      }
    }

    // Viewport horizontal residual still applies with maxRight (pad left edge).
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

  // resolveClampRight — pure: left edge of clamp target minus gap.
  // Prefer first matching selector among clampSelectors (or clampEl rect).
  // args: { clampEl?, clampRect?, clampSelectors?, doc?, clampGap?, gap? }
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

  // Position tip above host using viewport coords (avoids overflow:hidden clip).
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

  // placeTipLeftOfPointer(tip, event) — card placement relative to pointer.
  // opts.maxRight / clampEl / clampSelectors / clampGap / doc for 🎯T186 clamp.
  function placeTipLeftOfPointer(tip, event, opts) {
    if (!tip) return null;
    var o = opts || {};
    var px = 0;
    var py = 0;
    if (event) {
      if (typeof event.clientX === 'number') px = event.clientX;
      if (typeof event.clientY === 'number') py = event.clientY;
    }
    // Fallback: host center if no pointer coords (programmatic show).
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

    // If clamp forces a shrink, apply max-width before measuring for final place.
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

  // 🎯T203 registry helpers.
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

  // Force-hide a tip: prefer attach's sticky reset (_instantTipForceHide).
  function forceHideTip(tip) {
    if (!tip) return false;
    if (typeof tip._instantTipForceHide === 'function') {
      tip._instantTipForceHide();
      return true;
    }
    return hideTip(tip);
  }

  // Hide every open tip except keep (🎯T203 singleton).
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

  // openTipsCount / getOpenTips — hermetic inspection of the singleton set.
  function openTipsCount() {
    return openTips.length;
  }

  function getOpenTips() {
    return openTips.slice();
  }

  // showTip(tip) — synchronous; no setTimeout. Used by attach + hermetic.
  // opts.placement / opts.event / opts.host for positioning.
  // 🎯T203: product-wide singleton — dismisses every other open InstantTip first.
  function showTip(tip, host, opts) {
    if (!tip) return false;
    dismissOtherTips(tip);
    if (tip.style) {
      tip.style.display = 'block';
      // 🎯T186: tip must receive pointer events (scroll, sticky hover bridge).
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

  // isVisible(tip) — hermetic visibility check.
  function isVisible(tip) {
    if (!tip) return false;
    if (tip.classList && tip.classList.contains) return tip.classList.contains(SHOW_CLASS);
    if (typeof tip.className === 'string') {
      return (' ' + tip.className + ' ').indexOf(' ' + SHOW_CLASS + ' ') >= 0;
    }
    return tip.style && tip.style.display === 'block';
  }

  // attach(host, text, opts) — custom tip on host; strips native title=; 0ms show.
  // Returns the tip element, or null if text empty.
  //
  // opts:
  //   doc, mount — document / mount node (tests inject mocks)
  //   html — when true, set innerHTML instead of textContent (rich cards)
  //   ariaLabel — plain string for aria-label (defaults to stripped text)
  //   placement — 'above-host' (default) | 'left-of-pointer' (🎯T181)
  //   className — extra class on tip (e.g. instant-tip-card)
  //   sticky — 🎯T186 default true: hide only after leave host AND tip (grace)
  //   hideGraceMs — override HIDE_GRACE_MS (tests may inject 0 or timers)
  //   timers — { setTimeout, clearTimeout } for hermetic sticky tests
  //   maxRight / clampEl / clampSelectors / clampGap — 🎯T186 table clamp
  function attach(host, text, opts) {
    var label = tipTextOrEmpty(text);
    if (!host || !label) return null;
    var o = opts || {};
    var doc = o.doc || host.ownerDocument || (typeof document !== 'undefined' ? document : null);
    if (!doc || typeof doc.createElement !== 'function') return null;

    // Ban delayed native title on this host for the explanation path.
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
    // 🎯T186: receive pointer events while open (set auto on show).
    tip.style.pointerEvents = 'none';

    var mount = o.mount || (doc.body || null);
    if (mount && typeof mount.appendChild === 'function') {
      mount.appendChild(tip);
    }

    var placement = o.placement || PLACE_ABOVE_HOST;
    var sticky = o.sticky !== false; // default sticky (🎯T186)
    var graceMs = o.hideGraceMs != null ? Number(o.hideGraceMs) : HIDE_GRACE_MS;
    if (!(graceMs >= 0)) graceMs = HIDE_GRACE_MS;
    var timers = o.timers || null;
    var schedule = timers && typeof timers.setTimeout === 'function'
      ? timers.setTimeout
      : (typeof setTimeout === 'function' ? setTimeout : function (fn) { fn(); return 0; });
    var cancel = timers && typeof timers.clearTimeout === 'function'
      ? timers.clearTimeout
      : (typeof clearTimeout === 'function' ? clearTimeout : function () {});

    var overHost = false;
    var overTip = false;
    // 🎯T187: once pointer has entered the tip, dismiss is tip-leave only.
    var tipEngaged = false;
    var hideTimer = null;

    function clearHideTimer() {
      if (hideTimer != null) {
        cancel(hideTimer);
        hideTimer = null;
      }
    }

    function hoverState() {
      return { overHost: overHost, overTip: overTip, tipEngaged: tipEngaged };
    }

    function doHide() {
      clearHideTimer();
      tipEngaged = false;
      overTip = false;
      overHost = false;
      hideTip(tip);
    }

    // 🎯T203: singleton dismiss from another tip's show path resets sticky state.
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

    // SHOW_DELAY_MS is 0: call showTip synchronously — never setTimeout for show.
    function onHostEnter(ev) {
      overHost = true;
      clearHideTimer();
      if (SHOW_DELAY_MS > 0) {
        // Dead branch by policy; kept only so a non-zero constant would be visible
        // in review. Product must keep SHOW_DELAY_MS === 0.
        return;
      }
      showTip(tip, host, placeOpts(ev));
    }

    function onHostLeave(ev) {
      var related = ev && (ev.relatedTarget != null ? ev.relatedTarget : ev.toElement);
      // Moving into the tip (or its children) is not a real host leave for hide.
      if (relatedStillInside(tip, related)) {
        overHost = false;
        overTip = true;
        tipEngaged = true;
        clearHideTimer();
        return;
      }
      overHost = false;
      // 🎯T187: after tip engaged, leave-host alone never dismisses.
      if (!shouldScheduleHideOnHostLeave(hoverState())) {
        clearHideTimer();
        return;
      }
      scheduleHide();
    }

    function onTipEnter() {
      overTip = true;
      tipEngaged = true;
      clearHideTimer();
      // Tip already open when entered from host; ensure visible if re-entered.
      if (!isVisible(tip)) {
        showTip(tip, host, placeOpts(null));
      }
    }

    function onTipLeave(ev) {
      var related = ev && (ev.relatedTarget != null ? ev.relatedTarget : ev.toElement);
      // Nested children / scroll chrome: still inside tip → ignore (🎯T187).
      if (relatedStillInside(tip, related)) {
        return;
      }
      // Moving back onto host: stay open; host enter may also fire.
      if (relatedStillInside(host, related)) {
        overTip = false;
        overHost = true;
        clearHideTimer();
        return;
      }
      overTip = false;
      if (!shouldScheduleHideOnTipLeave(hoverState())) {
        clearHideTimer();
        return;
      }
      scheduleHide();
    }

    function scheduleHide() {
      if (sticky) {
        // 🎯T187: while over tip (or host), never arm hide.
        if (!shouldRunScheduledHide(hoverState())) {
          clearHideTimer();
          return;
        }
        clearHideTimer();
        if (graceMs <= 0) {
          doHide();
          return;
        }
        // Gap grace only — not an idle timer. Callback re-checks overTip.
        hideTimer = schedule(function () {
          hideTimer = null;
          // Never hide while pointer is over tip/host (🎯T187).
          if (!shouldRunScheduledHide(hoverState())) return;
          doHide();
        }, graceMs);
        return;
      }
      // Non-sticky: immediate hide (legacy / explicit opt-out).
      doHide();
    }

    if (typeof host.addEventListener === 'function') {
      host.addEventListener('pointerenter', onHostEnter);
      host.addEventListener('pointerleave', onHostLeave);
      // mouseenter/leave for older paths / hermetic dispatch without PointerEvent.
      host.addEventListener('mouseenter', onHostEnter);
      host.addEventListener('mouseleave', onHostLeave);
    }

    if (sticky && typeof tip.addEventListener === 'function') {
      tip.addEventListener('pointerenter', onTipEnter);
      tip.addEventListener('pointerleave', onTipLeave);
      tip.addEventListener('mouseenter', onTipEnter);
      tip.addEventListener('mouseleave', onTipLeave);
      // Wheel/scroll over tip children must not dismiss (pointer stays over tip).
      // No hide path on wheel — only enter/leave flags drive dismiss.
      tip.addEventListener('wheel', function () {
        overTip = true;
        tipEngaged = true;
        clearHideTimer();
      }, { passive: true });
    }

    // Stash for tests / cleanup / hermetic overTip inspection.
    host._instantTip = tip;
    host._instantTipShow = onHostEnter;
    host._instantTipHide = onHostLeave;
    host._instantTipPlacement = placement;
    host._instantTipSticky = sticky;
    tip._instantTipOnEnter = onTipEnter;
    tip._instantTipOnLeave = onTipLeave;
    tip._instantTipHoverState = hoverState;
    return tip;
  }

  return {
    SHOW_DELAY_MS: SHOW_DELAY_MS,
    HIDE_GRACE_MS: HIDE_GRACE_MS,
    TIP_CLASS: TIP_CLASS,
    SHOW_CLASS: SHOW_CLASS,
    HOST_CLASS: HOST_CLASS,
    CARD_CLASS: CARD_CLASS,
    PLACE_ABOVE_HOST: PLACE_ABOVE_HOST,
    PLACE_LEFT_OF_POINTER: PLACE_LEFT_OF_POINTER,
    DEFAULT_CLAMP_GAP: DEFAULT_CLAMP_GAP,
    DEFAULT_CLAMP_SELECTORS: DEFAULT_CLAMP_SELECTORS,
    showSchedule: showSchedule,
    hideSchedule: hideSchedule,
    shouldRunScheduledHide: shouldRunScheduledHide,
    shouldScheduleHideOnHostLeave: shouldScheduleHideOnHostLeave,
    shouldScheduleHideOnTipLeave: shouldScheduleHideOnTipLeave,
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
