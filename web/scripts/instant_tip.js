// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Instant custom hover tips (🎯T175). Replaces delayed native title= tooltips
// for dense chrome (frontier status / fanout / truncated name).
// Show on pointerenter with 0ms delay — never setTimeout before show.
//
// 🎯T181: optional HTML content + left-of-pointer placement for rich cards.
// Default remains above-host text tips (status/fanout).

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
  var TIP_CLASS = 'instant-tip';
  var SHOW_CLASS = 'instant-tip-show';
  var HOST_CLASS = 'has-instant-tip';
  var CARD_CLASS = 'instant-tip-card';
  var PLACE_ABOVE_HOST = 'above-host';
  var PLACE_LEFT_OF_POINTER = 'left-of-pointer';
  var EDGE_PAD = 4;
  var POINTER_GAP = 12;

  // Pure schedule descriptor for hermetic checks (no timer).
  function showSchedule() {
    return {
      delayMs: SHOW_DELAY_MS,
      usesTimeout: false,
      event: 'pointerenter',
    };
  }

  // Pure: given empty text, attach is a no-op.
  function tipTextOrEmpty(text) {
    if (text == null) return '';
    var s = String(text).trim();
    return s;
  }

  // placeLeftOfPointerRect — pure placement math for 🎯T181 cards.
  // Prefer left of pointer, vertically centered on pointer.
  // If not enough room on the left, flip to the right of the pointer.
  // Clamp to viewport so the card stays on-screen (near edges residual: clamp).
  //
  // args: { pointerX, pointerY, tipW, tipH, viewW, viewH, gap?, pad? }
  // returns: { left, top, side: 'left'|'right' }
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

    var side = 'left';
    var left = px - gap - tw;
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

    var top = py - th / 2;
    if (vh > 0) {
      if (top + th > vh - pad) top = Math.max(pad, vh - pad - th);
      if (top < pad) top = pad;
    } else if (top < pad) {
      top = pad;
    }

    return {
      left: Math.round(left),
      top: Math.round(top),
      side: side,
    };
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
    var pos = placeLeftOfPointerRect({
      pointerX: px,
      pointerY: py,
      tipW: tip.offsetWidth || 0,
      tipH: tip.offsetHeight || 0,
      viewW: vw,
      viewH: vh,
      gap: o.gap,
      pad: o.pad,
    });
    if (tip.style) {
      tip.style.left = pos.left + 'px';
      tip.style.top = pos.top + 'px';
    }
    return pos;
  }

  // showTip(tip) — synchronous; no setTimeout. Used by attach + hermetic.
  // opts.placement / opts.event / opts.host for positioning.
  function showTip(tip, host, opts) {
    if (!tip) return false;
    if (tip.style) tip.style.display = 'block';
    if (tip.classList && tip.classList.add) tip.classList.add(SHOW_CLASS);
    else if (typeof tip.className === 'string' && tip.className.indexOf(SHOW_CLASS) < 0) {
      tip.className = (tip.className ? tip.className + ' ' : '') + SHOW_CLASS;
    }
    var o = opts || {};
    var placement = o.placement || PLACE_ABOVE_HOST;
    if (placement === PLACE_LEFT_OF_POINTER) {
      placeTipLeftOfPointer(tip, o.event, { host: host || o.host, gap: o.gap, pad: o.pad });
    } else if (host) {
      placeTip(tip, host);
    }
    return true;
  }

  function hideTip(tip) {
    if (!tip) return false;
    if (tip.style) tip.style.display = 'none';
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
    tip.style.pointerEvents = 'none';

    var mount = o.mount || (doc.body || null);
    if (mount && typeof mount.appendChild === 'function') {
      mount.appendChild(tip);
    }

    var placement = o.placement || PLACE_ABOVE_HOST;

    // SHOW_DELAY_MS is 0: call showTip synchronously — never setTimeout.
    function onEnter(ev) {
      if (SHOW_DELAY_MS > 0) {
        // Dead branch by policy; kept only so a non-zero constant would be visible
        // in review. Product must keep SHOW_DELAY_MS === 0.
        return;
      }
      showTip(tip, host, { placement: placement, event: ev, host: host });
    }
    function onLeave() {
      hideTip(tip);
    }

    if (typeof host.addEventListener === 'function') {
      host.addEventListener('pointerenter', onEnter);
      host.addEventListener('pointerleave', onLeave);
      // mouseenter/leave for older paths / hermetic dispatch without PointerEvent.
      host.addEventListener('mouseenter', onEnter);
      host.addEventListener('mouseleave', onLeave);
    }

    // Stash for tests / cleanup.
    host._instantTip = tip;
    host._instantTipShow = onEnter;
    host._instantTipHide = onLeave;
    host._instantTipPlacement = placement;
    return tip;
  }

  return {
    SHOW_DELAY_MS: SHOW_DELAY_MS,
    TIP_CLASS: TIP_CLASS,
    SHOW_CLASS: SHOW_CLASS,
    HOST_CLASS: HOST_CLASS,
    CARD_CLASS: CARD_CLASS,
    PLACE_ABOVE_HOST: PLACE_ABOVE_HOST,
    PLACE_LEFT_OF_POINTER: PLACE_LEFT_OF_POINTER,
    showSchedule: showSchedule,
    tipTextOrEmpty: tipTextOrEmpty,
    placeLeftOfPointerRect: placeLeftOfPointerRect,
    placeTipLeftOfPointer: placeTipLeftOfPointer,
    showTip: showTip,
    hideTip: hideTip,
    isVisible: isVisible,
    attach: attach,
  };
}));
