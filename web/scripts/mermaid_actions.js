// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Mermaid diagram actions (🎯T83.1): open-in-panel/tab, copy source, copy image.
// DOM-free pure helpers so Node can require(); browser glue in index.html.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.MermaidActions = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  /** Toolbar control definitions shown on each rendered .mermaid-diagram. */
  function toolbarButtons() {
    return [
      { action: 'open', label: 'Open', title: 'Open diagram in dedicated panel' },
      { action: 'copy-source', label: 'Copy source', title: 'Copy Mermaid source as text' },
      { action: 'copy-image', label: 'Copy image', title: 'Copy diagram as PNG (and Mermaid text when multi-MIME clipboard is available)' },
    ];
  }

  /**
   * Detect whether navigator.clipboard.write + ClipboardItem multi-type is usable.
   * Capabilities are injected so tests stay DOM-free.
   *
   * Browser limits (document for 🎯T83.1):
   * - Requires secure context (https or localhost) and a user gesture.
   * - ClipboardItem multi-type write is Chromium-complete; Safari is partial;
   *   Firefox often lacks image write or multi-type — callers fall back to
   *   text-only or sequential single-type writes.
   * - Some paste targets honor only one MIME type from a multi-type write.
   */
  function clipboardCapabilities(env) {
    const e = env || {};
    const hasClipboard = !!(e.clipboard && typeof e.clipboard.write === 'function');
    const hasWriteText = !!(e.clipboard && typeof e.clipboard.writeText === 'function');
    const hasClipboardItem = typeof e.ClipboardItem === 'function';
    const secure = e.isSecureContext !== false;
    return {
      secure: !!secure,
      write: hasClipboard && secure,
      writeText: hasWriteText && secure,
      multiType: hasClipboard && hasClipboardItem && secure,
    };
  }

  /**
   * Plan a clipboard write for diagram export.
   * @param {{ mermaidSrc: string, pngBlob?: *, wantMulti?: boolean, caps: object }} opts
   * @returns {{ mode: string, text?: string, types?: string[], reason?: string }}
   */
  function clipboardWritePlan(opts) {
    const o = opts || {};
    const caps = o.caps || clipboardCapabilities({});
    const src = String(o.mermaidSrc == null ? '' : o.mermaidSrc);
    const hasPng = !!o.pngBlob;
    const wantMulti = o.wantMulti !== false;

    if (wantMulti && hasPng && caps.multiType) {
      return {
        mode: 'multi',
        types: ['image/png', 'text/plain'],
        text: src,
      };
    }
    if (hasPng && caps.write) {
      return {
        mode: 'image',
        types: ['image/png'],
        text: src,
      };
    }
    if (caps.writeText || caps.write) {
      return {
        mode: 'text',
        types: ['text/plain'],
        text: src,
      };
    }
    return {
      mode: 'unavailable',
      types: [],
      text: src,
      reason: caps.secure
        ? 'Clipboard write API not available'
        : 'Clipboard requires a secure context (https or localhost)',
    };
  }

  /** Escape text for embedding in HTML text content / attributes. */
  function escapeHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  /**
   * Build a standalone HTML document that hosts the rendered SVG and source.
   * Used by window.open and as the panel body template.
   */
  function buildOpenDocumentHtml(opts) {
    const o = opts || {};
    const title = escapeHtml(o.title || 'Mermaid diagram');
    const svg = String(o.svgMarkup || '');
    const src = String(o.mermaidSrc == null ? '' : o.mermaidSrc);
    const dark = !!o.dark;
    const bg = dark ? '#1a0e0e' : '#f8f8fa';
    const fg = dark ? '#f0e8e8' : '#2c2c34';
    const surface = dark ? '#261515' : '#ffffff';
    const border = dark ? 'rgba(255,180,180,0.12)' : 'rgba(0,0,0,0.08)';
    const muted = dark ? '#b4a0a0' : '#6e6e7a';
    return (
      '<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">' +
      '<meta name="viewport" content="width=device-width, initial-scale=1">' +
      '<title>' + title + '</title>' +
      '<style>' +
      'html,body{margin:0;padding:0;background:' + bg + ';color:' + fg + ';' +
      'font-family:system-ui,-apple-system,sans-serif;min-height:100%;}' +
      'header{padding:10px 14px;border-bottom:1px solid ' + border + ';' +
      'background:' + surface + ';font-size:13px;font-weight:600;}' +
      'main{padding:16px;overflow:auto;text-align:center;}' +
      'main svg{max-width:100%;height:auto;}' +
      'details{margin:12px 14px 20px;text-align:left;}' +
      'summary{cursor:pointer;color:' + muted + ';font-size:12px;}' +
      'pre{white-space:pre-wrap;word-break:break-word;background:' + surface + ';' +
      'border:1px solid ' + border + ';border-radius:8px;padding:12px;' +
      'font-size:12px;text-align:left;}' +
      '</style></head><body>' +
      '<header>' + title + '</header>' +
      '<main class="mermaid-open-host">' + svg + '</main>' +
      '<details><summary>Mermaid source</summary>' +
      '<pre class="mermaid-open-source">' + escapeHtml(src) + '</pre>' +
      '</details></body></html>'
    );
  }

  /**
   * Normalize SVG markup for blob/data URL conversion.
   * Ensures xmlns so browsers can decode standalone SVG images.
   */
  function normalizeSvgMarkup(svgMarkup) {
    let s = String(svgMarkup || '').trim();
    if (!s) return '';
    if (!/\sxmlns=/.test(s)) {
      s = s.replace(/<svg\b/, '<svg xmlns="http://www.w3.org/2000/svg"');
    }
    return s;
  }

  function svgMarkupToDataUrl(svgMarkup) {
    const s = normalizeSvgMarkup(svgMarkup);
    if (!s) return '';
    // Prefer encodeURIComponent over btoa so unicode in labels is safe.
    return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(s);
  }

  /**
   * Decide open placement. Thin interim under T83: in-app bottom-right panel
   * is primary; new window is available when window.open works under a gesture.
   */
  function openPlacement(prefs) {
    const p = prefs || {};
    if (p.preferWindow) return 'window';
    return 'panel';
  }

  return {
    toolbarButtons: toolbarButtons,
    clipboardCapabilities: clipboardCapabilities,
    clipboardWritePlan: clipboardWritePlan,
    escapeHtml: escapeHtml,
    buildOpenDocumentHtml: buildOpenDocumentHtml,
    normalizeSvgMarkup: normalizeSvgMarkup,
    svgMarkupToDataUrl: svgMarkupToDataUrl,
    openPlacement: openPlacement,
  };
}));
