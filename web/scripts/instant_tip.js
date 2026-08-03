// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Instant custom hover tips (🎯T175). Replaces delayed native title= tooltips
// for dense chrome (frontier status / fanout / truncated name).
// Show on pointerenter with 0ms delay — never setTimeout before show.

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

  // showTip(tip) — synchronous; no setTimeout. Used by attach + hermetic.
  function showTip(tip, host) {
    if (!tip) return false;
    if (tip.style) tip.style.display = 'block';
    if (tip.classList && tip.classList.add) tip.classList.add(SHOW_CLASS);
    else if (typeof tip.className === 'string' && tip.className.indexOf(SHOW_CLASS) < 0) {
      tip.className = (tip.className ? tip.className + ' ' : '') + SHOW_CLASS;
    }
    if (host) placeTip(tip, host);
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

  // attach(host, text) — custom tip on host; strips native title=; 0ms show.
  // Returns the tip element, or null if text empty.
  // opts.doc: document for createElement (tests inject a mock).
  function attach(host, text, opts) {
    var label = tipTextOrEmpty(text);
    if (!host || !label) return null;
    var o = opts || {};
    var doc = o.doc || host.ownerDocument || (typeof document !== 'undefined' ? document : null);
    if (!doc || typeof doc.createElement !== 'function') return null;

    // Ban delayed native title on this host for the explanation path.
    if (typeof host.removeAttribute === 'function') host.removeAttribute('title');
    else if ('title' in host) host.title = '';

    if (typeof host.setAttribute === 'function') {
      host.setAttribute('aria-label', label);
    }
    if (host.classList && host.classList.add) host.classList.add(HOST_CLASS);
    else if (typeof host.className === 'string' && host.className.indexOf(HOST_CLASS) < 0) {
      host.className = (host.className ? host.className + ' ' : '') + HOST_CLASS;
    }

    var tip = doc.createElement('div');
    tip.className = TIP_CLASS;
    tip.setAttribute('role', 'tooltip');
    tip.textContent = label;
    tip.style.display = 'none';
    tip.style.position = 'fixed';
    tip.style.zIndex = '200';
    tip.style.pointerEvents = 'none';

    var mount = o.mount || (doc.body || null);
    if (mount && typeof mount.appendChild === 'function') {
      mount.appendChild(tip);
    }

    // SHOW_DELAY_MS is 0: call showTip synchronously — never setTimeout.
    function onEnter() {
      if (SHOW_DELAY_MS > 0) {
        // Dead branch by policy; kept only so a non-zero constant would be visible
        // in review. Product must keep SHOW_DELAY_MS === 0.
        return;
      }
      showTip(tip, host);
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
    return tip;
  }

  return {
    SHOW_DELAY_MS: SHOW_DELAY_MS,
    TIP_CLASS: TIP_CLASS,
    SHOW_CLASS: SHOW_CLASS,
    HOST_CLASS: HOST_CLASS,
    showSchedule: showSchedule,
    tipTextOrEmpty: tipTextOrEmpty,
    showTip: showTip,
    hideTip: hideTip,
    isVisible: isVisible,
    attach: attach,
  };
}));
