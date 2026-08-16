// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Chat image lightbox + multi-id carousel model (🎯T258).
// Pure helpers are DOM-free so Node hermetic tests can require() them.
// Browser glue (overlay DOM, click/keyboard) lives in index.html.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ImageLightbox = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  var IMAGE_MARKER_RE = /\[image:\s*([a-f0-9]+)(?:\s+(\d+)x(\d+))?\]/gi;
  // Half the old 240px max-height — thumbs are clickable, not readable.
  var CHAT_THUMB_HEIGHT_PX = 120;
  var CHAT_THUMB_MAX_WIDTH_PX = 320;
  var CHAT_THUMB_DEFAULT_ASPECT = 4 / 3;
  var sizeCache = Object.create(null);

  /**
   * Normalize a chat image id (hex string, lowercased).
   * @param {string|null|undefined} id
   * @returns {string}
   */
  function normalizeImageId(id) {
    if (id == null) return '';
    return String(id).trim().toLowerCase();
  }

  /**
   * Full-resolution image URL (on-demand load in the lightbox).
   * @param {string} id
   * @returns {string}
   */
  function imageFullSrc(id) {
    var n = normalizeImageId(id);
    return n ? '/api/images/' + n : '';
  }

  /**
   * Thumbnail URL (chat bubble hydrate path — 🎯T224).
   * @param {string} id
   * @returns {string}
   */
  function imageThumbSrc(id) {
    var n = normalizeImageId(id);
    return n ? '/api/images/' + n + '/thumb' : '';
  }

  /**
   * Extract ordered unique image ids from message text with [image: id] markers.
   * @param {string|null|undefined} text
   * @returns {string[]}
   */
  /**
   * Display box for an in-bubble thumb. Height is the standard 120px
   * except ultra-wide images that hit MAX_WIDTH (then height shrinks).
   * Unknown aspect uses 4:3 so the <img> can carry width+height before load.
   */
  function thumbDisplaySize(naturalW, naturalH) {
    var H = CHAT_THUMB_HEIGHT_PX;
    var maxW = CHAT_THUMB_MAX_WIDTH_PX;
    var nw = Number(naturalW);
    var nh = Number(naturalH);
    if (!(nw > 0) || !(nh > 0)) {
      return { width: Math.round(H * CHAT_THUMB_DEFAULT_ASPECT), height: H };
    }
    var w = Math.round(H * nw / nh);
    if (w < 1) w = 1;
    if (w > maxW) {
      var h = Math.round(maxW * nh / nw);
      if (h < 1) h = 1;
      return { width: maxW, height: h };
    }
    return { width: w, height: H };
  }

  function rememberSize(id, naturalW, naturalH) {
    var n = normalizeImageId(id);
    var nw = Number(naturalW);
    var nh = Number(naturalH);
    if (!n || !(nw > 0) || !(nh > 0)) return null;
    sizeCache[n] = { width: nw, height: nh };
    return sizeCache[n];
  }

  function lookupSize(id) {
    var n = normalizeImageId(id);
    return n ? (sizeCache[n] || null) : null;
  }

  function parseImageMarker(raw) {
    var s = String(raw == null ? '' : raw);
    var re = /\[image:\s*([a-f0-9]+)(?:\s+(\d+)x(\d+))?\]/i;
    var m = re.exec(s);
    if (!m) return null;
    var id = normalizeImageId(m[1]);
    var nw = m[2] ? parseInt(m[2], 10) : 0;
    var nh = m[3] ? parseInt(m[3], 10) : 0;
    if (nw > 0 && nh > 0) rememberSize(id, nw, nh);
    return { id: id, width: nw || 0, height: nh || 0 };
  }

  function extractImageIdsFromText(text) {
    var s = String(text == null ? '' : text);
    var out = [];
    var seen = Object.create(null);
    IMAGE_MARKER_RE.lastIndex = 0;
    var m;
    while ((m = IMAGE_MARKER_RE.exec(s)) !== null) {
      var id = normalizeImageId(m[1]);
      if (!id || seen[id]) continue;
      seen[id] = true;
      out.push(id);
    }
    return out;
  }

  /**
   * Collect ordered unique ids from img nodes (data-image-id or src parse).
   * @param {ArrayLike<{getAttribute?: function, dataset?: object, src?: string}>|null|undefined} imgs
   * @returns {string[]}
   */
  function siblingIdsFromImgs(imgs) {
    if (!imgs || !imgs.length) return [];
    var out = [];
    var seen = Object.create(null);
    for (var i = 0; i < imgs.length; i++) {
      var el = imgs[i];
      var id = '';
      if (el && typeof el.getAttribute === 'function') {
        id = el.getAttribute('data-image-id') || '';
      }
      if (!id && el && el.dataset && el.dataset.imageId) {
        id = el.dataset.imageId;
      }
      if (!id && el && el.src) {
        id = parseIdFromSrc(String(el.src));
      }
      id = normalizeImageId(id);
      if (!id || seen[id]) continue;
      seen[id] = true;
      out.push(id);
    }
    return out;
  }

  /**
   * Parse image id from /api/images/{id} or /api/images/{id}/thumb URL.
   * @param {string} src
   * @returns {string}
   */
  function parseIdFromSrc(src) {
    if (!src) return '';
    var m = String(src).match(/\/api\/images\/([a-f0-9]+)(?:\/thumb)?(?:\?|$)/i);
    return m ? normalizeImageId(m[1]) : '';
  }

  /**
   * Clamp index into [0, len).
   * @param {number} index
   * @param {number} len
   * @returns {number}
   */
  function clampIndex(index, len) {
    if (len <= 0) return 0;
    var i = index | 0;
    if (i < 0) return 0;
    if (i >= len) return len - 1;
    return i;
  }

  /**
   * Open a lightbox session.
   * @param {{ ids?: string[], index?: number, id?: string }} opts
   * @returns {{ open: boolean, ids: string[], index: number }}
   */
  function open(opts) {
    var o = opts || {};
    var ids = Array.isArray(o.ids) ? o.ids.map(normalizeImageId).filter(Boolean) : [];
    // Dedupe while preserving order.
    var seen = Object.create(null);
    var uniq = [];
    for (var i = 0; i < ids.length; i++) {
      if (seen[ids[i]]) continue;
      seen[ids[i]] = true;
      uniq.push(ids[i]);
    }
    ids = uniq;
    var index = 0;
    if (o.id) {
      var want = normalizeImageId(o.id);
      var found = ids.indexOf(want);
      if (found >= 0) {
        index = found;
      } else if (want) {
        ids = [want].concat(ids);
        index = 0;
      }
    } else if (typeof o.index === 'number') {
      index = clampIndex(o.index, ids.length);
    }
    if (!ids.length) {
      return { open: false, ids: [], index: 0 };
    }
    return { open: true, ids: ids, index: clampIndex(index, ids.length) };
  }

  /**
   * @param {{ open?: boolean, ids?: string[], index?: number }|null|undefined} session
   * @returns {{ open: boolean, ids: string[], index: number }}
   */
  function close(session) {
    var ids = session && Array.isArray(session.ids) ? session.ids.slice() : [];
    var index = session && typeof session.index === 'number'
      ? clampIndex(session.index, ids.length)
      : 0;
    return { open: false, ids: ids, index: index };
  }

  /**
   * @param {{ open?: boolean, ids?: string[], index?: number }|null|undefined} session
   * @returns {boolean}
   */
  function isOpen(session) {
    return !!(session && session.open && session.ids && session.ids.length);
  }

  /**
   * @param {{ ids?: string[], index?: number }|null|undefined} session
   * @returns {string}
   */
  function currentId(session) {
    if (!session || !session.ids || !session.ids.length) return '';
    return session.ids[clampIndex(session.index, session.ids.length)] || '';
  }

  /**
   * @param {{ ids?: string[] }|null|undefined} session
   * @returns {boolean}
   */
  function hasMultiple(session) {
    return !!(session && session.ids && session.ids.length > 1);
  }

  /**
   * @param {{ open?: boolean, ids?: string[], index?: number }|null|undefined} session
   * @param {number} delta
   * @returns {{ open: boolean, ids: string[], index: number }}
   */
  function step(session, delta) {
    if (!isOpen(session)) {
      return session && typeof session === 'object'
        ? { open: false, ids: (session.ids || []).slice(), index: session.index | 0 }
        : { open: false, ids: [], index: 0 };
    }
    var ids = session.ids.slice();
    var len = ids.length;
    if (len <= 1) {
      return { open: true, ids: ids, index: 0 };
    }
    var next = ((session.index | 0) + (delta | 0)) % len;
    if (next < 0) next += len;
    return { open: true, ids: ids, index: next };
  }

  function next(session) {
    return step(session, 1);
  }

  function prev(session) {
    return step(session, -1);
  }

  /**
   * @param {{ open?: boolean, ids?: string[], index?: number }|null|undefined} session
   * @param {number} index
   * @returns {{ open: boolean, ids: string[], index: number }}
   */
  function goTo(session, index) {
    if (!isOpen(session)) {
      return close(session);
    }
    return {
      open: true,
      ids: session.ids.slice(),
      index: clampIndex(index, session.ids.length),
    };
  }

  /**
   * Keyboard handler for lightbox: Esc close; arrows carousel when multi.
   * @param {{ open?: boolean, ids?: string[], index?: number }|null|undefined} session
   * @param {string} key  KeyboardEvent.key (Escape, ArrowLeft, ArrowRight, …)
   * @returns {{ session: object, action: string|null }}
   */
  function handleKey(session, key) {
    if (!isOpen(session)) {
      return { session: close(session), action: null };
    }
    var k = String(key || '');
    if (k === 'Escape' || k === 'Esc') {
      return { session: close(session), action: 'close' };
    }
    if (k === 'ArrowRight' || k === 'Right') {
      if (!hasMultiple(session)) {
        return { session: session, action: null };
      }
      return { session: next(session), action: 'next' };
    }
    if (k === 'ArrowLeft' || k === 'Left') {
      if (!hasMultiple(session)) {
        return { session: session, action: null };
      }
      return { session: prev(session), action: 'prev' };
    }
    return { session: session, action: null };
  }

  /**
   * Resolve sibling set + start id from a clicked chat image element.
   * Prefer message-scoped .chat-img siblings; fall back to data-image-id alone.
   * @param {Element|null|undefined} img
   * @returns {{ ids: string[], id: string }}
   */
  function resolveFromClick(img) {
    if (!img) return { ids: [], id: '' };
    var id = '';
    if (typeof img.getAttribute === 'function') {
      id = normalizeImageId(img.getAttribute('data-image-id') || '');
    }
    if (!id && img.src) id = parseIdFromSrc(String(img.src));
    var root = null;
    if (typeof img.closest === 'function') {
      root = img.closest('.msg') || img.closest('.msg-body') || null;
    }
    var list = null;
    if (root && typeof root.querySelectorAll === 'function') {
      list = root.querySelectorAll('img.chat-img');
    }
    var ids = siblingIdsFromImgs(list);
    if (!ids.length && id) ids = [id];
    if (id && ids.indexOf(id) < 0) ids = [id].concat(ids);
    return { ids: ids, id: id || (ids[0] || '') };
  }

  return {
    normalizeImageId: normalizeImageId,
    imageFullSrc: imageFullSrc,
    imageThumbSrc: imageThumbSrc,
    extractImageIdsFromText: extractImageIdsFromText,
    siblingIdsFromImgs: siblingIdsFromImgs,
    parseIdFromSrc: parseIdFromSrc,
    open: open,
    close: close,
    isOpen: isOpen,
    currentId: currentId,
    hasMultiple: hasMultiple,
    next: next,
    prev: prev,
    goTo: goTo,
    handleKey: handleKey,
    resolveFromClick: resolveFromClick,
    CHAT_THUMB_HEIGHT_PX: CHAT_THUMB_HEIGHT_PX,
    CHAT_THUMB_MAX_WIDTH_PX: CHAT_THUMB_MAX_WIDTH_PX,
    thumbDisplaySize: thumbDisplaySize,
    rememberSize: rememberSize,
    lookupSize: lookupSize,
    parseImageMarker: parseImageMarker,
  };
}));
