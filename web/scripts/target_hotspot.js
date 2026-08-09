// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T326 — Bullseye target ids in owner chat / product HTML as hover hotspots
// that share the frontier target-card renderer (formatTargetCardMarkdown +
// InstantTip). Pure helpers are DOM-free so Node hermetics can require().
//
// Layout (product pin): smaller finger (the hotspot) sits to the LEFT of the
// card; the card opens to the RIGHT of the hotspot (InstantTip
// PLACE_RIGHT_OF_HOST). Content path is FrontierTable.formatTargetCardMarkdown
// — never a forked card.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.TargetHotspot = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Capital T + digits/dots. Optional leading 🎯. Worker names use lowercase t.
  var TARGET_TOKEN_RE = /(?:🎯\s*)?(T\d+(?:\.\d+)*)\b/g;

  // Tags whose text content must not become hotspots.
  var SKIP_TAGS = {
    CODE: true, PRE: true, A: true, SCRIPT: true, STYLE: true,
    TEXTAREA: true, 'TARGET-HOTSPOT': true,
  };

  var HOTSPOT_CLASS = 'target-hotspot';
  var FINGER_CLASS = 'target-hotspot-finger';
  var CARD_TIP_CLASS = 'instant-tip-card target-card-tip';

  function escHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function normalizeTargetID(raw) {
    var s = raw == null ? '' : String(raw).trim();
    if (!s) return '';
    s = s.replace(/^🎯\s*/, '').trim();
    if (!s) return '';
    if (s.charAt(0) === 't') s = 'T' + s.slice(1);
    return s;
  }

  /** Agent/CLI display form: always 🎯Tn. */
  function formatDisplayTargetID(raw) {
    var id = normalizeTargetID(raw);
    return id ? ('🎯' + id) : '';
  }

  /**
   * Pure: rewrite plain text so each bullseye id is a hotspot span.
   * Leaves existing HTML tags alone when applied only to text nodes.
   */
  function linkifyTargetText(text) {
    var s = text == null ? '' : String(text);
    if (!s) return s;
    TARGET_TOKEN_RE.lastIndex = 0;
    return s.replace(TARGET_TOKEN_RE, function (full, id) {
      var tid = normalizeTargetID(id);
      if (!tid) return full;
      var label = formatDisplayTargetID(tid);
      return '<span class="' + HOTSPOT_CLASS + ' ' + FINGER_CLASS +
        '" data-target-id="' + escHtml(tid) +
        '" role="button" tabindex="0">' + escHtml(label) + '</span>';
    });
  }

  /**
   * Pure: linkify target ids in an HTML string, skipping code/pre/a bodies.
   * Idempotent for already-wrapped hotspots (class target-hotspot).
   */
  function linkifyTargetIDsInHTML(html) {
    if (html == null || html === '') return html == null ? html : '';
    var s = String(html);
    // Fast path: nothing that looks like a T-id.
    if (!/(?:🎯\s*)?T\d/.test(s)) return s;

    var out = '';
    var i = 0;
    var n = s.length;
    var skipDepth = 0;
    var skipTag = '';

    while (i < n) {
      if (s.charAt(i) === '<') {
        var close = s.indexOf('>', i);
        if (close < 0) {
          out += s.slice(i);
          break;
        }
        var tag = s.slice(i, close + 1);
        out += tag;
        var mOpen = /^<\s*([a-zA-Z0-9:-]+)/.exec(tag);
        var mClose = /^<\s*\/\s*([a-zA-Z0-9:-]+)/.exec(tag);
        var selfClose = /\/\s*>$/.test(tag);
        if (mClose) {
          var cname = mClose[1].toUpperCase();
          if (skipDepth > 0 && cname === skipTag) {
            skipDepth--;
            if (skipDepth === 0) skipTag = '';
          }
        } else if (mOpen && !selfClose) {
          var oname = mOpen[1].toUpperCase();
          // Already a hotspot span — treat interior as skip.
          if (oname === 'SPAN' && /\btarget-hotspot\b/i.test(tag)) {
            skipDepth++;
            skipTag = 'SPAN';
          } else if (SKIP_TAGS[oname]) {
            skipDepth++;
            skipTag = oname;
          }
        }
        i = close + 1;
        continue;
      }

      // Text run until next tag.
      var next = s.indexOf('<', i);
      if (next < 0) next = n;
      var chunk = s.slice(i, next);
      if (skipDepth > 0) {
        out += chunk;
      } else {
        out += linkifyTargetText(chunk);
      }
      i = next;
    }
    return out;
  }

  /** Pure: find a frontier-style row by id (🎯-tolerant). */
  function findRowByTargetID(rows, targetId) {
    var want = normalizeTargetID(targetId);
    if (!want || !Array.isArray(rows)) return null;
    for (var i = 0; i < rows.length; i++) {
      var r = rows[i];
      if (!r) continue;
      var id = normalizeTargetID(r.id);
      if (id && id === want) return r;
    }
    return null;
  }

  /**
   * Pure: InstantTip opts for a chat hotspot card.
   * Placement: card opens to the RIGHT of the smaller left finger (hotspot).
   * Content must still be supplied by the caller via formatTargetCardMarkdown.
   */
  function hotspotCardOpts(extra) {
    var o = {
      html: true,
      placement: 'right-of-host',
      className: CARD_TIP_CLASS,
      sticky: true,
      hitGroup: true,
      // No frontier-table clamp — chat lives in the left column.
      clampSelectors: [],
    };
    if (extra && typeof extra === 'object') {
      for (var k in extra) {
        if (Object.prototype.hasOwnProperty.call(extra, k) && extra[k] !== undefined) {
          o[k] = extra[k];
        }
      }
    }
    return o;
  }

  /**
   * Pure: shared render path descriptor for hermetics.
   * Product attach must call FrontierTable.formatTargetCardMarkdown (not a fork).
   */
  function sharedCardRenderPath() {
    return {
      markdownBuilder: 'FrontierTable.formatTargetCardMarkdown',
      plainBuilder: 'FrontierTable.formatTargetCardPlain',
      tipAttach: 'InstantTip.attach',
      placement: 'right-of-host',
      fingerLeftOfCard: true,
      cardOpensRightOfHotspot: true,
      hotspotClass: HOTSPOT_CLASS,
      fingerClass: FINGER_CLASS,
      cardClass: CARD_TIP_CLASS,
    };
  }

  /**
   * Pure: minimal row when ledger cache has no match (still uses shared formatter).
   */
  function minimalRowForID(targetId) {
    var id = normalizeTargetID(targetId);
    if (!id) return null;
    return { id: id, name: '', status: '' };
  }

  /**
   * Pure: markdown body for a hotspot — shared formatter when available,
   * else a one-line 🎯id stub so attach still has content.
   */
  function cardMarkdownForRow(row, formatFn) {
    if (typeof formatFn === 'function') {
      var md = formatFn(row);
      if (md) return md;
    }
    if (!row || !row.id) return '';
    return '**' + formatDisplayTargetID(row.id) + '**' +
      (row.name ? (' — ' + String(row.name)) : '');
  }

  return {
    HOTSPOT_CLASS: HOTSPOT_CLASS,
    FINGER_CLASS: FINGER_CLASS,
    CARD_TIP_CLASS: CARD_TIP_CLASS,
    normalizeTargetID: normalizeTargetID,
    formatDisplayTargetID: formatDisplayTargetID,
    linkifyTargetText: linkifyTargetText,
    linkifyTargetIDsInHTML: linkifyTargetIDsInHTML,
    findRowByTargetID: findRowByTargetID,
    hotspotCardOpts: hotspotCardOpts,
    sharedCardRenderPath: sharedCardRenderPath,
    minimalRowForID: minimalRowForID,
    cardMarkdownForRow: cardMarkdownForRow,
  };
}));
