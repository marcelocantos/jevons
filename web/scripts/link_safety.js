// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Chat/UI hyperlink safety (🎯T151): content links open in a new tab.
// target=_blank + rel=noopener noreferrer so navigation never steals the
// chat session (and avoids reverse-tabnabbing).
//
// Keep this file free of browser globals so Node can require() it for
// hermetic tests. DOM helpers accept an element/container when present.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.LinkSafety = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const TARGET = '_blank';
  const REL = 'noopener noreferrer';
  const ATTRS_HTML = ' target="_blank" rel="noopener noreferrer"';

  /**
   * Ensure every opening <a …> in an HTML string has target=_blank and a
   * safe rel. Overwrites existing target/rel on anchors (content surfaces
   * must not open same-tab). Idempotent.
   *
   * @param {string} html
   * @returns {string}
   */
  function ensureHtmlAnchors(html) {
    if (html == null || html === '') return html == null ? html : '';
    if (typeof html !== 'string') html = String(html);
    return html.replace(/<a\b([^>]*?)>/gi, function (_m, attrs) {
      let cleaned = String(attrs || '');
      cleaned = cleaned
        .replace(/\s*\btarget\s*=\s*(["'])[\s\S]*?\1/gi, '')
        .replace(/\s*\btarget\s*=\s*[^\s>]+/gi, '')
        .replace(/\s*\brel\s*=\s*(["'])[\s\S]*?\1/gi, '')
        .replace(/\s*\brel\s*=\s*[^\s>]+/gi, '');
      return '<a' + cleaned + ATTRS_HTML + '>';
    });
  }

  /**
   * Set target/rel on a single DOM element if it is an <a>.
   * @param {Element|null|undefined} el
   * @returns {Element|null|undefined}
   */
  function decorateAnchor(el) {
    if (!el || !el.setAttribute) return el;
    const tag = el.tagName || el.nodeName;
    if (tag && String(tag).toUpperCase() === 'A') {
      el.setAttribute('target', TARGET);
      el.setAttribute('rel', REL);
    }
    return el;
  }

  /**
   * Decorate all anchors under a container (post-innerHTML safety net).
   * @param {ParentNode|null|undefined} root
   */
  function decorateContainer(root) {
    if (!root || typeof root.querySelectorAll !== 'function') return;
    const list = root.querySelectorAll('a');
    for (let i = 0; i < list.length; i++) decorateAnchor(list[i]);
  }

  /**
   * Wrap an smd default_renderer so LINK/RAW_URL anchors get safe attrs
   * at create time (progressive stream paint). Does not edit vendored smd.
   *
   * @param {object} smd streaming-markdown module
   * @param {object} renderer result of smd.default_renderer(root)
   * @returns {object} same renderer, patched
   */
  function wrapSmdDefaultRenderer(smd, renderer) {
    if (!smd || !renderer || typeof renderer.add_token !== 'function') return renderer;
    const linkTok = smd.LINK;
    const rawTok = smd.RAW_URL;
    const orig = renderer.add_token;
    renderer.add_token = function (data, type) {
      orig.call(this, data, type);
      if (type === linkTok || type === rawTok) {
        try {
          const nodes = data && data.nodes;
          const idx = data && data.index;
          if (nodes && typeof idx === 'number') decorateAnchor(nodes[idx]);
        } catch (_) { /* ignore */ }
      }
    };
    return renderer;
  }

  /**
   * Apply safe attrs onto a string-renderer attr map when the node is <a>.
   * Used by the hermetic/DOM-free progressive path.
   *
   * @param {object} attrs
   * @param {string} tag
   * @returns {object}
   */
  function decorateStringAttrs(attrs, tag) {
    const a = attrs && typeof attrs === 'object' ? attrs : {};
    if (tag === 'a') {
      a.target = TARGET;
      a.rel = REL;
    }
    return a;
  }

  return {
    TARGET: TARGET,
    REL: REL,
    ensureHtmlAnchors: ensureHtmlAnchors,
    decorateAnchor: decorateAnchor,
    decorateContainer: decorateContainer,
    wrapSmdDefaultRenderer: wrapSmdDefaultRenderer,
    decorateStringAttrs: decorateStringAttrs,
  };
}));
