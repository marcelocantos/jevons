// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Mermaid diagram actions (🎯T83.1 + 🎯T83 durable chrome):
// open-in-panel/tab, copy source, copy image, pin last graph, paste/load.
// DOM-free pure helpers so Node can require(); browser glue in index.html.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.MermaidActions = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  /** localStorage key for the last pinned project/chat graph (🎯T83). */
  const PIN_STORAGE_KEY = 'jevons-mermaid-viz-v1';

  /** Toolbar control definitions shown on each rendered .mermaid-diagram. */
  function toolbarButtons() {
    return [
      { action: 'open', label: 'Open', title: 'Open diagram in dedicated panel' },
      { action: 'copy-source', label: 'Copy source', title: 'Copy Mermaid source as text' },
      { action: 'copy-image', label: 'Copy image', title: 'Copy diagram as PNG (and Mermaid text when multi-MIME clipboard is available)' },
    ];
  }

  /**
   * Durable panel header controls (🎯T83 chrome, not chat-bubble toolbar).
   * Manual refresh / paste / load-last; event-driven refresh is deferred.
   */
  function panelChromeButtons() {
    return [
      { action: 'load-last', label: 'Load last', title: 'Restore the last pinned graph' },
      { action: 'paste', label: 'Paste', title: 'Paste Mermaid / bullseye graph export' },
      { action: 'render', label: 'Render', title: 'Render pasted Mermaid source' },
      { action: 'pin', label: 'Pin', title: 'Pin current graph as last' },
      { action: 'popout', label: 'New tab', title: 'Open in a new browser tab' },
      { action: 'close', label: 'Close', title: 'Close panel' },
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

  /**
   * Strip optional ```mermaid fences and surrounding whitespace.
   * Accepts raw bullseye graph export or a fenced chat paste.
   */
  function stripMermaidFence(text) {
    let s = String(text == null ? '' : text).replace(/^\uFEFF/, '').trim();
    if (!s) return '';
    // Full fence: ```mermaid ... ``` or ``` ... ```
    const fenced = s.match(/^```(?:mermaid)?\s*\r?\n([\s\S]*?)\r?\n```\s*$/i);
    if (fenced) return String(fenced[1] || '').trim();
    // Leading fence only (paste without closing fence)
    const lead = s.match(/^```(?:mermaid)?\s*\r?\n([\s\S]*)$/i);
    if (lead) {
      let body = String(lead[1] || '');
      body = body.replace(/\r?\n```\s*$/, '');
      return body.trim();
    }
    return s;
  }

  /** True when source looks like something mermaid might accept (non-empty). */
  function isRenderableSource(src) {
    return stripMermaidFence(src).length > 0;
  }

  /**
   * Normalize a pin payload for storage / panel state.
   * @returns {{ version: number, src: string, svgMarkup: string, title: string, updatedAt: number }|null}
   */
  function normalizePinnedGraph(raw) {
    if (!raw || typeof raw !== 'object') return null;
    const src = stripMermaidFence(raw.src);
    const svgMarkup = String(raw.svgMarkup == null ? '' : raw.svgMarkup);
    if (!src && !svgMarkup) return null;
    const title = String(raw.title == null ? '' : raw.title).trim() || 'Graph';
    const updatedAt = typeof raw.updatedAt === 'number' && isFinite(raw.updatedAt)
      ? raw.updatedAt
      : Date.now();
    return {
      version: 1,
      src: src,
      svgMarkup: svgMarkup,
      title: title,
      updatedAt: updatedAt,
    };
  }

  /**
   * Load pinned graph from a Storage-like object (injectable for tests).
   * @param {{ getItem?: function(string): ?string }} storage
   */
  function loadPinnedGraph(storage) {
    if (!storage || typeof storage.getItem !== 'function') return null;
    let raw;
    try {
      raw = storage.getItem(PIN_STORAGE_KEY);
    } catch (_) {
      return null;
    }
    if (!raw) return null;
    try {
      return normalizePinnedGraph(JSON.parse(raw));
    } catch (_) {
      return null;
    }
  }

  /**
   * Persist a pinned graph. Returns the normalized payload or null.
   * @param {{ setItem?: function(string, string): void }} storage
   */
  function savePinnedGraph(storage, state) {
    const n = normalizePinnedGraph(state);
    if (!n || !storage || typeof storage.setItem !== 'function') return null;
    try {
      storage.setItem(PIN_STORAGE_KEY, JSON.stringify(n));
    } catch (_) {
      return null;
    }
    return n;
  }

  /** Remove pinned graph from storage. */
  function clearPinnedGraph(storage) {
    if (!storage || typeof storage.removeItem !== 'function') return false;
    try {
      storage.removeItem(PIN_STORAGE_KEY);
      return true;
    } catch (_) {
      return false;
    }
  }

  /**
   * Empty-state copy when the durable panel has no diagram yet (🎯T83).
   * Instructs paste of bullseye Mermaid export or open from chat.
   */
  function emptyStateHtml() {
    return (
      '<div class="mvp-empty" data-mvp-empty="1">' +
      '<p class="mvp-empty-title">Project graph viz</p>' +
      '<p class="mvp-empty-body">No graph loaded. Open a Mermaid diagram from chat, ' +
      'use <strong>Load last</strong> for the pinned graph, or <strong>Paste</strong> a ' +
      'bullseye Mermaid export (```mermaid fence or raw source).</p>' +
      '<p class="mvp-empty-hint">Manual refresh only — live event refresh is deferred.</p>' +
      '</div>'
    );
  }

  /**
   * Recovery when chrome product API is missing/stale (404/5xx) or unreachable (🎯T196).
   * Owner-facing; matches daily daemon bounce path.
   */
  const PRODUCT_FETCH_RECOVERY_HINT =
    'Rebuild jevonsd if needed, then run scripts/restart-daily-jevonsd.sh and hard-reload the UI.';

  /** Short status-line recovery fragment (status bar is one line). */
  const PRODUCT_FETCH_RECOVERY_SHORT = 'rebuild / restart-daily';

  /**
   * 🎯T196: Actionable chrome fetch failure view — HTTP code + recovery.
   * Must never look like emptyStateHtml (generic paste shell).
   *
   * @param {{
   *   resource?: string,
   *   status?: number,
   *   message?: string,
   *   kind?: 'http'|'network'|'unknown',
   *   recoveryHint?: string,
   * }} info
   * @returns {{
   *   title: string,
   *   status: string,
   *   bodyHtml: string,
   *   httpStatus: number|null,
   *   kind: string,
   *   recoveryHint: string,
   * }}
   */
  function productFetchFailureView(info) {
    const o = info || {};
    const resource = String(o.resource || 'Request').trim() || 'Request';
    let httpStatus = null;
    if (typeof o.status === 'number' && isFinite(o.status) && o.status > 0) {
      httpStatus = Math.floor(o.status);
    }
    let detail = String(o.message == null ? '' : o.message).trim();
    const httpOnly = /^HTTP\s+(\d{3})\b/i.exec(detail);
    if (httpOnly) {
      if (httpStatus == null) httpStatus = parseInt(httpOnly[1], 10);
      detail = detail.replace(/^HTTP\s+\d{3}\s*[:.\-–—]?\s*/i, '').trim();
    }
    let kind = o.kind || '';
    if (!kind) {
      if (httpStatus != null) kind = 'http';
      else if (/failed to fetch|networkerror|network error|load failed|econnrefused/i.test(detail)) {
        kind = 'network';
      } else {
        kind = 'unknown';
      }
    }
    const recovery = String(o.recoveryHint || PRODUCT_FETCH_RECOVERY_HINT).trim()
      || PRODUCT_FETCH_RECOVERY_HINT;
    const codeLabel = httpStatus != null ? ('HTTP ' + httpStatus) : '';
    let statusCore;
    if (codeLabel && detail && detail !== codeLabel) {
      statusCore = resource + ' failed: ' + codeLabel + ' — ' + detail;
    } else if (codeLabel) {
      statusCore = resource + ' failed: ' + codeLabel;
    } else if (detail) {
      statusCore = resource + ' failed: ' + detail;
    } else if (kind === 'network') {
      statusCore = resource + ' failed: network error';
    } else {
      statusCore = resource + ' failed';
    }
    const status = statusCore + ' · ' + PRODUCT_FETCH_RECOVERY_SHORT;

    let bodyDetail;
    if (codeLabel && detail) {
      bodyDetail = '<strong>' + escapeHtml(codeLabel) + '</strong> — ' + escapeHtml(detail);
    } else if (codeLabel) {
      bodyDetail = '<strong>' + escapeHtml(codeLabel) + '</strong>';
    } else if (detail) {
      bodyDetail = '<strong>Error</strong> — ' + escapeHtml(detail);
    } else if (kind === 'network') {
      bodyDetail = '<strong>Network error</strong> — request unreachable';
    } else {
      bodyDetail = '<strong>Error</strong> — request failed';
    }

    const bodyHtml =
      '<div class="mvp-error" data-mvp-fetch-error="1">' +
      '<p class="mvp-error-title">' + escapeHtml(resource) + ' could not load</p>' +
      '<p class="mvp-error-body">' + bodyDetail + '</p>' +
      '<p class="mvp-error-hint">' + escapeHtml(recovery) + '</p>' +
      '</div>';

    return {
      title: resource,
      status: status,
      bodyHtml: bodyHtml,
      httpStatus: httpStatus,
      kind: kind,
      recoveryHint: recovery,
    };
  }

  /**
   * Map a thrown Error (optionally with httpStatus/kind) into productFetchFailureView.
   * @param {Error|{message?: string, httpStatus?: number, kind?: string}|string|null} err
   * @param {{ resource?: string, status?: number, kind?: string, recoveryHint?: string }} [defaults]
   */
  function productFetchFailureFromError(err, defaults) {
    const d = defaults || {};
    let message = '';
    let status = typeof d.status === 'number' ? d.status : null;
    let kind = d.kind || '';
    if (err && typeof err === 'object') {
      if (err.message != null) message = String(err.message);
      else message = String(err);
      if (typeof err.httpStatus === 'number' && isFinite(err.httpStatus)) {
        status = err.httpStatus;
      }
      if (err.kind) kind = String(err.kind);
    } else if (err != null) {
      message = String(err);
    }
    if (status == null) {
      const m = /HTTP\s+(\d{3})\b/i.exec(message);
      if (m) status = parseInt(m[1], 10);
    }
    if (!kind) {
      if (status != null) kind = 'http';
      else if (/failed to fetch|networkerror|network error|load failed|econnrefused/i.test(message)) {
        kind = 'network';
      }
    }
    return productFetchFailureView({
      resource: d.resource || 'Request',
      status: status != null ? status : undefined,
      message: message,
      kind: kind || undefined,
      recoveryHint: d.recoveryHint,
    });
  }

  /**
   * Decide what the durable panel should show on open-from-chrome.
   * @returns {{ mode: 'pinned'|'empty', pin?: object }}
   */
  function openFromChromePlan(storage) {
    const pin = loadPinnedGraph(storage);
    if (pin && (pin.src || pin.svgMarkup)) {
      return { mode: 'pinned', pin: pin };
    }
    return { mode: 'empty' };
  }

  /**
   * Whether #mermaid-viz-panel is currently shown (compact chrome or mvp-large).
   * Accepts a minimal panel shape so hermetic tests stay DOM-free (🎯T189).
   * @param {{ classList?: { contains: (c: string) => boolean }, hidden?: boolean }|null} panel
   */
  function isMermaidPanelOpen(panel) {
    if (!panel) return false;
    if (panel.hidden) return false;
    const cl = panel.classList;
    if (cl && typeof cl.contains === 'function') {
      return !!cl.contains('open');
    }
    // Fallback: hidden cleared without classList (tests / partial mocks).
    return panel.hidden === false;
  }

  /**
   * Escape closes the durable mermaid panel when it is open (same as Close).
   * When closed, Escape must not be claimed — other UI (interrupt, edit
   * cancel, etc.) keeps the key (🎯T189).
   * @param {string} key event.key
   * @param {{ classList?: { contains: (c: string) => boolean }, hidden?: boolean }|null} panel
   * @returns {boolean} true → caller should closeMermaidPanel + preventDefault
   */
  function shouldCloseMermaidOnEscape(key, panel) {
    return String(key || '') === 'Escape' && isMermaidPanelOpen(panel);
  }

  // ── 🎯T268: single-graph scale-to-fill ─────────────────────────────────
  // Frontier Graph (mvp-large) must expand the rendered SVG to fill the pane
  // (contain: use full width or height). Shrink-only max-width:100% leaves
  // tiny graphs floating in empty margins — fail. Multi-component pack is a
  // residual (later bin-pack shelf/skyline); single graph ships first.

  /** Positive finite number or 0. */
  function positiveNumber(n) {
    const x = typeof n === 'number' ? n : parseFloat(n);
    return isFinite(x) && x > 0 ? x : 0;
  }

  /**
   * Natural SVG size from attrs / viewBox (DOM-free input shape).
   * @param {{ width?: *, height?: *, viewBox?: string|null, getAttribute?: function }} svg
   * @returns {{ w: number, h: number }}
   */
  function parseSvgNaturalSize(svg) {
    if (!svg || typeof svg !== 'object') return { w: 0, h: 0 };
    let w = 0;
    let h = 0;
    if (typeof svg.getAttribute === 'function') {
      w = positiveNumber(svg.getAttribute('width'));
      h = positiveNumber(svg.getAttribute('height'));
      if ((!w || !h) && svg.getAttribute('viewBox')) {
        const vb = String(svg.getAttribute('viewBox') || '').trim().split(/[\s,]+/);
        if (vb.length >= 4) {
          if (!w) w = positiveNumber(vb[2]);
          if (!h) h = positiveNumber(vb[3]);
        }
      }
    } else {
      w = positiveNumber(svg.width);
      h = positiveNumber(svg.height);
      if ((!w || !h) && svg.viewBox) {
        const vb = String(svg.viewBox).trim().split(/[\s,]+/);
        if (vb.length >= 4) {
          if (!w) w = positiveNumber(vb[2]);
          if (!h) h = positiveNumber(vb[3]);
        }
      }
    }
    return { w: w, h: h };
  }

  /**
   * Contain scale: largest scale that keeps the whole graph in the pane.
   * Scales UP small graphs and DOWN large ones. Never returns 0.
   * @returns {number}
   */
  function computeContainScale(svgW, svgH, paneW, paneH) {
    const sw = positiveNumber(svgW);
    const sh = positiveNumber(svgH);
    const pw = positiveNumber(paneW);
    const ph = positiveNumber(paneH);
    if (!sw || !sh || !pw || !ph) return 1;
    return Math.min(pw / sw, ph / sh);
  }

  /**
   * 🎯T268 fit plan for a single graph in the frontier pane.
   * @param {{
   *   svgW: number, svgH: number, paneW: number, paneH: number,
   *   padding?: number, diagramCount?: number
   * }} opts
   * @returns {{
   *   mode: 'scale-to-fill'|'pack-residual'|'skip',
   *   scale: number,
   *   displayW: number,
   *   displayH: number,
   *   fillsPane: boolean,
   *   residual?: string
   * }}
   */
  function planSingleGraphScaleToFill(opts) {
    const o = opts || {};
    const count = o.diagramCount != null ? Math.floor(o.diagramCount) : 1;
    if (count > 1) {
      return {
        mode: 'pack-residual',
        scale: 1,
        displayW: positiveNumber(o.svgW),
        displayH: positiveNumber(o.svgH),
        fillsPane: false,
        residual:
          'Multi-component: later bin-pack (shelf/skyline) into pane aspect; ' +
          'single-graph scale-to-fill is the T268 product path.',
      };
    }
    const pad = o.padding != null ? Math.max(0, positiveNumber(o.padding) || 0) : 24;
    // padding is total inset (both sides); floor at 0 usable.
    const paneW = Math.max(0, positiveNumber(o.paneW) - pad);
    const paneH = Math.max(0, positiveNumber(o.paneH) - pad);
    const svgW = positiveNumber(o.svgW);
    const svgH = positiveNumber(o.svgH);
    if (!svgW || !svgH || !paneW || !paneH) {
      return {
        mode: 'skip',
        scale: 1,
        displayW: svgW,
        displayH: svgH,
        fillsPane: false,
      };
    }
    const scale = computeContainScale(svgW, svgH, paneW, paneH);
    const displayW = svgW * scale;
    const displayH = svgH * scale;
    // Fills pane when at least one axis uses ≥95% of usable pane (contain).
    const coverW = displayW / paneW;
    const coverH = displayH / paneH;
    const fillsPane = Math.max(coverW, coverH) >= 0.95;
    return {
      mode: 'scale-to-fill',
      scale: scale,
      displayW: displayW,
      displayH: displayH,
      fillsPane: fillsPane,
    };
  }

  /**
   * Inline style object for the fitted SVG (browser applies after render).
   * Clears max-width so CSS shrink-only cannot undo scale-up.
   */
  function svgScaleToFillStyle(plan) {
    const p = plan || {};
    if (p.mode !== 'scale-to-fill' || !(p.displayW > 0) || !(p.displayH > 0)) {
      return null;
    }
    return {
      width: Math.round(p.displayW * 1000) / 1000 + 'px',
      height: Math.round(p.displayH * 1000) / 1000 + 'px',
      maxWidth: 'none',
      maxHeight: 'none',
    };
  }

  /**
   * Apply plan styles to an SVG-like element (injectable for tests).
   * @returns {boolean} true when styles were applied
   */
  function applySvgScaleToFill(svg, plan) {
    const style = svgScaleToFillStyle(plan);
    if (!svg || !style) return false;
    if (svg.style && typeof svg.style === 'object') {
      svg.style.width = style.width;
      svg.style.height = style.height;
      svg.style.maxWidth = style.maxWidth;
      svg.style.maxHeight = style.maxHeight;
    }
    if (typeof svg.removeAttribute === 'function') {
      // Mermaid sets absolute width/height attrs that fight CSS scale-up.
      svg.removeAttribute('width');
      svg.removeAttribute('height');
    }
    if (typeof svg.setAttribute === 'function') {
      svg.setAttribute('data-mvp-scale-fill', String(plan.scale));
    }
    return true;
  }

  return {
    PIN_STORAGE_KEY: PIN_STORAGE_KEY,
    toolbarButtons: toolbarButtons,
    panelChromeButtons: panelChromeButtons,
    clipboardCapabilities: clipboardCapabilities,
    clipboardWritePlan: clipboardWritePlan,
    escapeHtml: escapeHtml,
    buildOpenDocumentHtml: buildOpenDocumentHtml,
    normalizeSvgMarkup: normalizeSvgMarkup,
    svgMarkupToDataUrl: svgMarkupToDataUrl,
    openPlacement: openPlacement,
    stripMermaidFence: stripMermaidFence,
    isRenderableSource: isRenderableSource,
    normalizePinnedGraph: normalizePinnedGraph,
    loadPinnedGraph: loadPinnedGraph,
    savePinnedGraph: savePinnedGraph,
    clearPinnedGraph: clearPinnedGraph,
    emptyStateHtml: emptyStateHtml,
    PRODUCT_FETCH_RECOVERY_HINT: PRODUCT_FETCH_RECOVERY_HINT,
    PRODUCT_FETCH_RECOVERY_SHORT: PRODUCT_FETCH_RECOVERY_SHORT,
    productFetchFailureView: productFetchFailureView,
    productFetchFailureFromError: productFetchFailureFromError,
    openFromChromePlan: openFromChromePlan,
    isMermaidPanelOpen: isMermaidPanelOpen,
    shouldCloseMermaidOnEscape: shouldCloseMermaidOnEscape,
    // 🎯T268
    parseSvgNaturalSize: parseSvgNaturalSize,
    computeContainScale: computeContainScale,
    planSingleGraphScaleToFill: planSingleGraphScaleToFill,
    svgScaleToFillStyle: svgScaleToFillStyle,
    applySvgScaleToFill: applySvgScaleToFill,
  };
}));
