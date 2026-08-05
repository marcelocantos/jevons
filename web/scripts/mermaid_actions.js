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

  /**
   * 🎯T274: pause chat earlier-history hydrate while the large Frontier Graph
   * overlay is open. Layout thrash + IntersectionObserver re-fire was flashing
   * "Loading earlier…" above the empty/slow orthograph graph panel.
   * Compact chrome (mvp-large absent) does not pause hydrate.
   * @param {{ classList?: { contains: (c: string) => boolean }, hidden?: boolean }|null} panel
   */
  function shouldPauseHistoryHydrate(panel) {
    if (!isMermaidPanelOpen(panel)) return false;
    const cl = panel && panel.classList;
    if (cl && typeof cl.contains === 'function') {
      return !!cl.contains('mvp-large');
    }
    return false;
  }

  /** Default Mermaid render budget for frontier/panel diagrams (🎯T274). */
  var MERMAID_RENDER_TIMEOUT_MS = 12000;

  /**
   * Race a render promise against a timeout so the panel never infinite-loads
   * on a hung dagre layout (large single-component residual).
   * @param {Promise} renderPromise
   * @param {number} [ms]
   * @returns {Promise}
   */
  function withMermaidRenderTimeout(renderPromise, ms) {
    var budget = typeof ms === 'number' && ms > 0 ? ms : MERMAID_RENDER_TIMEOUT_MS;
    var p = renderPromise && typeof renderPromise.then === 'function'
      ? renderPromise
      : Promise.resolve(renderPromise);
    return new Promise(function (resolve, reject) {
      var settled = false;
      var timer = setTimeout(function () {
        if (settled) return;
        settled = true;
        var err = new Error('Mermaid render timed out after ' + budget + 'ms');
        err.kind = 'timeout';
        reject(err);
      }, budget);
      p.then(function (v) {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        resolve(v);
      }, function (e) {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        reject(e);
      });
    });
  }

  // ── 🎯T268: single-graph scale-to-fill ─────────────────────────────────
  // Frontier Graph (mvp-large) must expand the rendered SVG to fill the pane
  // (contain: use full width or height). Shrink-only max-width:100% leaves
  // tiny graphs floating in empty margins — fail.
  // 🎯T276: multi-diagram = pack natural boxes into pane aspect, then scale
  // the composite (not wrap-grid minmax(320px) micro-islands).
  // 🎯T277: natural SVG aspect survives every step — uniform scale only.
  // Never stretch SVG to chrome-inflated placement aspect (T276 false-fix).
  // Multi-diagram *count* is T190/T274 split+orphans, not T277 inventing boxes.

  /** Fixed title+pad chrome per pack block (not scaled into SVG aspect). 🎯T277 */
  const PACK_BLOCK_CHROME_H = 48; // title ~28 + block pad ~20

  /** Positive finite number or 0. */
  function positiveNumber(n) {
    const x = typeof n === 'number' ? n : parseFloat(n);
    return isFinite(x) && x > 0 ? x : 0;
  }

  /**
   * 🎯T277: true when SVG display size preserves natural aspect (uniform scale).
   * Fails chrome-inflated stretch (displayH includes title/pad empty).
   */
  function placementSvgAspectMatchesNatural(placement, eps) {
    const p = placement || {};
    const nw = positiveNumber(p.naturalW != null ? p.naturalW : p.w);
    const nh = positiveNumber(p.naturalH != null ? p.naturalH : p.h);
    const dw = positiveNumber(p.svgDisplayW != null ? p.svgDisplayW : p.displayW);
    const dh = positiveNumber(p.svgDisplayH != null ? p.svgDisplayH : p.displayH);
    if (!nw || !nh || !dw || !dh) return false;
    const tol = eps != null && isFinite(eps) ? Math.abs(eps) : 1e-6;
    return Math.abs(dw / dh - nw / nh) <= tol;
  }

  /**
   * 🎯T277 false-fix model: pack chrome-inflated boxes then size SVG to the
   * full placement (title+pad in height). Produces tall-skinny stretch —
   * placementSvgAspectMatchesNatural must fail.
   */
  function planChromeInflatedStretchOracle(boxes, opts) {
    const o = opts || {};
    const chromeH = o.chromeH != null ? Math.max(0, positiveNumber(o.chromeH) || 0) : PACK_BLOCK_CHROME_H;
    const list = Array.isArray(boxes) ? boxes : [];
    const inflated = [];
    const naturals = [];
    for (let i = 0; i < list.length; i++) {
      const b = list[i] || {};
      const w = positiveNumber(b.w);
      const h = positiveNumber(b.h);
      if (!w || !h) continue;
      naturals.push({ w: w, h: h, id: b.id, i: i });
      inflated.push({ w: w, h: h + chromeH, id: b.id });
    }
    const packed = packBoxesIntoPaneAspect(inflated, {
      paneW: o.paneW,
      paneH: o.paneH,
      gap: o.gap,
    });
    const paneW = positiveNumber(o.paneW);
    const paneH = positiveNumber(o.paneH);
    if (!packed.placements.length || !packed.compositeW || !packed.compositeH || !paneW || !paneH) {
      return { mode: 'chrome-inflated-stretch', placements: [], scale: 1, fillsPane: false };
    }
    const scale = computeContainScale(packed.compositeW, packed.compositeH, paneW, paneH);
    const placements = packed.placements.map(function (p) {
      const nat = naturals[p.i] || { w: p.w, h: Math.max(1, p.h - chromeH) };
      return {
        i: p.i,
        id: p.id,
        naturalW: nat.w,
        naturalH: nat.h,
        // Bug path: SVG stretched to full placement (chrome in aspect).
        displayW: p.w * scale,
        displayH: p.h * scale,
        svgDisplayW: p.w * scale,
        svgDisplayH: p.h * scale,
      };
    });
    return {
      mode: 'chrome-inflated-stretch',
      scale: scale,
      placements: placements,
      fillsPane: Math.max(
        (packed.compositeW * scale) / paneW,
        (packed.compositeH * scale) / paneH
      ) >= 0.95,
    };
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
   * Multi-diagram goes through planMultiDiagramPackScaleToFill (🎯T276).
   * @param {{
   *   svgW: number, svgH: number, paneW: number, paneH: number,
   *   padding?: number, diagramCount?: number
   * }} opts
   * @returns {{
   *   mode: 'scale-to-fill'|'skip',
   *   scale: number,
   *   displayW: number,
   *   displayH: number,
   *   fillsPane: boolean
   * }}
   */
  function planSingleGraphScaleToFill(opts) {
    const o = opts || {};
    const count = o.diagramCount != null ? Math.floor(o.diagramCount) : 1;
    if (count > 1) {
      // Caller should use planMultiDiagramPackScaleToFill; keep single-path pure.
      return {
        mode: 'skip',
        scale: 1,
        displayW: positiveNumber(o.svgW),
        displayH: positiveNumber(o.svgH),
        fillsPane: false,
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
   * 🎯T276/T277: shelf-pack **natural** diagram boxes into a nominal rectangle
   * whose aspect matches the pane. First-fit decreasing shelf (height-major).
   * Boxes must be natural SVG size — do not pre-inflate with title/pad chrome
   * (chrome is fixed after scale; 🎯T277).
   * @param {Array<{w:number,h:number,id?:string}>} boxes
   * @param {{ paneW: number, paneH: number, gap?: number }} opts
   * @returns {{
   *   placements: Array<{i:number,id?:string,x:number,y:number,w:number,h:number,row:number}>,
   *   compositeW: number,
   *   compositeH: number,
   *   shelfWidth: number,
   *   rowCount: number
   * }}
   */
  function packBoxesIntoPaneAspect(boxes, opts) {
    const o = opts || {};
    const paneW = positiveNumber(o.paneW);
    const paneH = positiveNumber(o.paneH);
    const gap = o.gap != null ? Math.max(0, positiveNumber(o.gap) || 0) : 12;
    const list = Array.isArray(boxes) ? boxes : [];
    const items = [];
    for (let i = 0; i < list.length; i++) {
      const b = list[i] || {};
      const w = positiveNumber(b.w);
      const h = positiveNumber(b.h);
      if (!w || !h) continue;
      items.push({ i: i, id: b.id, w: w, h: h });
    }
    if (!items.length) {
      return { placements: [], compositeW: 0, compositeH: 0, shelfWidth: 0, rowCount: 0 };
    }
    // Target shelf width from pane aspect so packed rows fill a pane-shaped bin.
    // floor at largest box width so every item can sit on a shelf.
    let maxW = 0;
    let area = 0;
    for (let j = 0; j < items.length; j++) {
      if (items[j].w > maxW) maxW = items[j].w;
      area += items[j].w * items[j].h;
    }
    let shelfWidth = maxW;
    if (paneW > 0 && paneH > 0) {
      // Ideal width for a bin with pane aspect and total area (plus gap slack).
      const aspect = paneW / paneH;
      const idealW = Math.sqrt(area * aspect);
      shelfWidth = Math.max(maxW, idealW);
      // Prefer not wider than pane natural; still ≥ maxW.
      if (paneW >= maxW) shelfWidth = Math.min(Math.max(shelfWidth, maxW), Math.max(paneW, maxW));
      else shelfWidth = maxW;
    }
    // Sort tallest-first for denser shelves (stable by original index).
    items.sort(function (a, b) {
      if (b.h !== a.h) return b.h - a.h;
      return a.i - b.i;
    });
    const placements = [];
    let cursorY = 0;
    let shelfH = 0;
    let cursorX = 0;
    let usedW = 0;
    let row = 0;
    for (let k = 0; k < items.length; k++) {
      const it = items[k];
      if (cursorX > 0 && cursorX + it.w > shelfWidth + 1e-9) {
        // New shelf.
        cursorY += shelfH + gap;
        cursorX = 0;
        shelfH = 0;
        row += 1;
      }
      placements.push({
        i: it.i,
        id: it.id,
        x: cursorX,
        y: cursorY,
        w: it.w,
        h: it.h,
        row: row,
      });
      cursorX += it.w + gap;
      if (it.h > shelfH) shelfH = it.h;
      const rowRight = cursorX - gap;
      if (rowRight > usedW) usedW = rowRight;
    }
    const compositeH = cursorY + shelfH;
    const compositeW = Math.max(usedW, maxW);
    const rowCount = row + 1;
    // Restore placement order by original diagram index.
    placements.sort(function (a, b) { return a.i - b.i; });
    return {
      placements: placements,
      compositeW: compositeW,
      compositeH: compositeH,
      shelfWidth: shelfWidth,
      rowCount: rowCount,
    };
  }

  /**
   * 🎯T276/T277: measure natural SVG boxes → pack into pane form-factor →
   * contain-scale composite. SVG display always preserves natural aspect
   * (uniform scale). Placement height = scaled natural SVG + **fixed** chrome
   * (title/pad constant) — never stretch SVG to chrome-inflated aspect.
   *
   * Multi-diagram count is T190/T274 split+orphans; this only packs what it
   * is given.
   *
   * @param {{
   *   boxes: Array<{w:number,h:number,id?:string}>,
   *   paneW: number, paneH: number,
   *   padding?: number, gap?: number, chromeH?: number
   * }} opts
   * @returns {{
   *   mode: 'pack-scale-to-fill'|'skip',
   *   scale: number,
   *   compositeW: number,
   *   compositeH: number,
   *   displayW: number,
   *   displayH: number,
   *   fillsPane: boolean,
   *   chromeH: number,
   *   placements: Array<{
   *     i:number,id?:string,x:number,y:number,w:number,h:number,row?:number,
   *     naturalW:number,naturalH:number,
   *     displayX:number,displayY:number,displayW:number,displayH:number,
   *     svgDisplayW:number,svgDisplayH:number
   *   }>
   * }}
   */
  function planMultiDiagramPackScaleToFill(opts) {
    const o = opts || {};
    const pad = o.padding != null ? Math.max(0, positiveNumber(o.padding) || 0) : 0;
    const paneW = Math.max(0, positiveNumber(o.paneW) - pad);
    const paneH = Math.max(0, positiveNumber(o.paneH) - pad);
    const gap = o.gap != null ? Math.max(0, positiveNumber(o.gap) || 0) : 12;
    const chromeH = o.chromeH != null
      ? Math.max(0, positiveNumber(o.chromeH) || 0)
      : PACK_BLOCK_CHROME_H;
    // Natural SVG boxes only — chrome is fixed after scale (🎯T277).
    const packed = packBoxesIntoPaneAspect(o.boxes || [], {
      paneW: paneW,
      paneH: paneH,
      gap: gap,
    });
    if (!packed.placements.length || !packed.compositeW || !packed.compositeH || !paneW || !paneH) {
      return {
        mode: 'skip',
        scale: 1,
        compositeW: packed.compositeW || 0,
        compositeH: packed.compositeH || 0,
        displayW: packed.compositeW || 0,
        displayH: packed.compositeH || 0,
        fillsPane: false,
        chromeH: chromeH,
        placements: [],
      };
    }
    const cW = packed.compositeW;
    const cH = packed.compositeH;
    const nRows = packed.rowCount > 0 ? packed.rowCount : 1;
    // finalW = cW * S; finalH = cH * S + nRows * chromeH (chrome fixed per row).
    // Solve S so composite fits the pane (contain).
    let scaleW = paneW / cW;
    let scaleH;
    if (chromeH > 0 && nRows * chromeH < paneH) {
      scaleH = (paneH - nRows * chromeH) / cH;
    } else if (chromeH <= 0) {
      scaleH = paneH / cH;
    } else {
      // Chrome alone would overflow; shrink SVG aggressively.
      scaleH = paneH / (cH + nRows * chromeH);
    }
    let scale = Math.min(scaleW, scaleH);
    if (!(scale > 0) || !isFinite(scale)) scale = 1;

    // Group natural placements by shelf row; rebuild Y with fixed chrome so
    // blocks do not overlap after chrome is added.
    const byRow = {};
    const rowOrder = [];
    for (let pi = 0; pi < packed.placements.length; pi++) {
      const p = packed.placements[pi];
      const r = p.row != null ? p.row : 0;
      if (!byRow[r]) {
        byRow[r] = [];
        rowOrder.push(r);
      }
      byRow[r].push(p);
    }
    rowOrder.sort(function (a, b) { return a - b; });

    const placements = [];
    let cursorY = 0;
    let usedW = 0;
    for (let ri = 0; ri < rowOrder.length; ri++) {
      const rowPls = byRow[rowOrder[ri]];
      let rowSvgH = 0;
      for (let j = 0; j < rowPls.length; j++) {
        if (rowPls[j].h > rowSvgH) rowSvgH = rowPls[j].h;
      }
      const rowSvgDispH = rowSvgH * scale;
      for (let j = 0; j < rowPls.length; j++) {
        const p = rowPls[j];
        const svgDisplayW = p.w * scale;
        const svgDisplayH = p.h * scale;
        const displayW = svgDisplayW;
        const displayH = svgDisplayH + chromeH;
        const displayX = p.x * scale;
        const right = displayX + displayW;
        if (right > usedW) usedW = right;
        placements.push({
          i: p.i,
          id: p.id,
          x: p.x,
          y: p.y,
          w: p.w,
          h: p.h,
          row: p.row,
          naturalW: p.w,
          naturalH: p.h,
          displayX: displayX,
          displayY: cursorY,
          displayW: displayW,
          displayH: displayH,
          svgDisplayW: svgDisplayW,
          svgDisplayH: svgDisplayH,
        });
      }
      cursorY += rowSvgDispH + chromeH;
      if (ri < rowOrder.length - 1) cursorY += gap * scale;
    }
    placements.sort(function (a, b) { return a.i - b.i; });

    const displayW = usedW > 0 ? usedW : cW * scale;
    const displayH = cursorY;
    const coverW = displayW / paneW;
    const coverH = displayH / paneH;
    const fillsPane = Math.max(coverW, coverH) >= 0.95;
    return {
      mode: 'pack-scale-to-fill',
      scale: scale,
      compositeW: cW,
      compositeH: cH,
      displayW: displayW,
      displayH: displayH,
      fillsPane: fillsPane,
      chromeH: chromeH,
      placements: placements,
    };
  }

  /**
   * 🎯T280: tall-empty-column layout oracle (DOM-metric pure).
   * Owner trainwreck: N tall grey Component columns with tiny SVG content.
   * Fails (isTallEmpty: true) when ≥2 blocks are tall (h/w ≥ maxAspect)
   * and SVG area covers < minCover of the block.
   * @param {Array<{blockW:number,blockH:number,svgW:number,svgH:number}>} blocks
   * @param {{ maxAspect?: number, minCover?: number, minTallEmpty?: number }} opts
   * @returns {{
   *   isTallEmpty: boolean,
   *   tallEmptyCount: number,
   *   blockCount: number,
   *   mode: string,
   *   details: Array<object>
   * }}
   */
  function assessTallEmptyColumnLayout(blocks, opts) {
    const o = opts || {};
    const maxAspect = o.maxAspect != null ? positiveNumber(o.maxAspect) : 2.0;
    const minCover = o.minCover != null ? positiveNumber(o.minCover) : 0.35;
    const minTallEmpty = o.minTallEmpty != null ? Math.max(1, Math.floor(o.minTallEmpty)) : 2;
    const list = Array.isArray(blocks) ? blocks : [];
    const details = [];
    let tallEmptyCount = 0;
    for (let i = 0; i < list.length; i++) {
      const b = list[i] || {};
      const blockW = positiveNumber(b.blockW);
      const blockH = positiveNumber(b.blockH);
      const svgW = positiveNumber(b.svgW);
      const svgH = positiveNumber(b.svgH);
      const aspect = blockW > 0 ? blockH / blockW : 0;
      const blockArea = blockW * blockH;
      const svgArea = svgW * svgH;
      const cover = blockArea > 0 ? svgArea / blockArea : 0;
      const isTall = aspect >= maxAspect;
      const lowCover = cover < minCover;
      const isTallEmpty = isTall && lowCover && blockW > 0 && blockH > 0;
      if (isTallEmpty) tallEmptyCount++;
      details.push({
        i: i,
        aspect: aspect,
        cover: cover,
        isTall: isTall,
        lowCover: lowCover,
        isTallEmpty: isTallEmpty,
      });
    }
    return {
      isTallEmpty: tallEmptyCount >= minTallEmpty,
      tallEmptyCount: tallEmptyCount,
      blockCount: list.length,
      mode: 'tall-empty-columns',
      details: details,
    };
  }

  /**
   * 🎯T280: single-pane SVG cover oracle — good owner default after scale-to-fill.
   * Passes when one SVG covers ≥ minCover of the larger pane axis (contain fill).
   * @param {{ paneW:number, paneH:number, svgDisplayW:number, svgDisplayH:number }} metrics
   * @param {{ minCover?: number }} opts
   */
  function assessSingleGraphPaneCover(metrics, opts) {
    const o = opts || {};
    const m = metrics || {};
    const minCover = o.minCover != null ? positiveNumber(o.minCover) : 0.75;
    const paneW = positiveNumber(m.paneW);
    const paneH = positiveNumber(m.paneH);
    const svgW = positiveNumber(m.svgDisplayW);
    const svgH = positiveNumber(m.svgDisplayH);
    if (!paneW || !paneH || !svgW || !svgH) {
      return { ok: false, cover: 0, mode: 'single-pane-cover' };
    }
    const cover = Math.max(svgW / paneW, svgH / paneH);
    return {
      ok: cover >= minCover,
      cover: cover,
      mode: 'single-pane-cover',
    };
  }

  /**
   * Oracle helper: wrap-grid micro-island layout never fills the pane.
   * Models T190 CSS minmax(min(100%, 320px), 1fr) — each cell ≤320, no
   * composite scale. Used by hermetics so the old path fails ≥95% cover.
   * @param {Array<{w:number,h:number}>} boxes
   * @param {{ paneW: number, paneH: number, cellMax?: number }} opts
   * @returns {{ fillsPane: boolean, maxCellCover: number, mode: string }}
   */
  function planWrapGridMicroIslandOracle(boxes, opts) {
    const o = opts || {};
    const paneW = positiveNumber(o.paneW);
    const paneH = positiveNumber(o.paneH);
    const cellMax = o.cellMax != null ? positiveNumber(o.cellMax) : 320;
    const list = Array.isArray(boxes) ? boxes : [];
    if (!paneW || !paneH || !list.length) {
      return { fillsPane: false, maxCellCover: 0, mode: 'wrap-grid-micro' };
    }
    // Each diagram sits in a ≤cellMax column; SVG max-width 100% of cell, no scale-up.
    let maxCover = 0;
    for (let i = 0; i < list.length; i++) {
      const bw = positiveNumber(list[i] && list[i].w);
      const bh = positiveNumber(list[i] && list[i].h);
      if (!bw || !bh) continue;
      const cellW = Math.min(cellMax, paneW);
      // Shrink-only into cell; no scale-up past natural.
      const s = Math.min(1, cellW / bw);
      const dw = bw * s;
      const dh = bh * s;
      const cover = Math.max(dw / paneW, dh / paneH);
      if (cover > maxCover) maxCover = cover;
    }
    return {
      fillsPane: maxCover >= 0.95,
      maxCellCover: maxCover,
      mode: 'wrap-grid-micro',
    };
  }

  /**
   * Inline style object for the fitted SVG (browser applies after render).
   * Clears max-width so CSS shrink-only cannot undo scale-up.
   * Accepts scale-to-fill (T268) and pack-scale-to-fill placements (T276).
   */
  function svgScaleToFillStyle(plan) {
    const p = plan || {};
    if (
      (p.mode !== 'scale-to-fill' && p.mode !== 'pack-scale-to-fill') ||
      !(p.displayW > 0) ||
      !(p.displayH > 0)
    ) {
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

  /**
   * Apply one T276/T277 placement to a pack block + its SVG (DOM-free shape).
   * SVG display uses natural-aspect svgDisplayW×svgDisplayH (or natural×scale).
   * Never stretches SVG to chrome-inflated block displayH (🎯T277).
   * @returns {boolean}
   */
  function applyPackPlacement(block, svg, placement, scale) {
    if (!placement) return false;
    const s = positiveNumber(scale) || 1;
    const natW = positiveNumber(
      placement.naturalW != null ? placement.naturalW : placement.w
    );
    const natH = positiveNumber(
      placement.naturalH != null ? placement.naturalH : placement.h
    );
    // Prefer explicit svg display from plan; else natural × uniform scale.
    let svgW = positiveNumber(placement.svgDisplayW);
    let svgH = positiveNumber(placement.svgDisplayH);
    if (!svgW || !svgH) {
      if (natW && natH) {
        svgW = natW * s;
        svgH = natH * s;
      } else {
        // Last resort: block content size without assuming chrome stretch.
        svgW = positiveNumber(placement.displayW);
        svgH = positiveNumber(placement.displayH);
      }
    }
    if (block && block.style && typeof block.style === 'object') {
      block.style.position = 'absolute';
      block.style.left = Math.round(placement.displayX * 1000) / 1000 + 'px';
      block.style.top = Math.round(placement.displayY * 1000) / 1000 + 'px';
      block.style.width = Math.round(placement.displayW * 1000) / 1000 + 'px';
      // Height follows scaled SVG + fixed chrome content; auto avoids empty stretch.
      block.style.height = placement.displayH > 0
        ? Math.round(placement.displayH * 1000) / 1000 + 'px'
        : 'auto';
      block.style.margin = '0';
      block.style.gridColumn = 'auto';
      block.style.overflow = 'hidden';
    }
    if (svg && svgW > 0 && svgH > 0) {
      applySvgScaleToFill(svg, {
        mode: 'scale-to-fill',
        scale: s,
        displayW: svgW,
        displayH: svgH,
      });
    }
    if (block && typeof block.setAttribute === 'function') {
      block.setAttribute('data-mvp-pack-placed', '1');
      block.setAttribute('data-mvp-pack-scale', String(s));
      if (svgW > 0 && svgH > 0) {
        block.setAttribute('data-mvp-svg-aspect', String(svgW / svgH));
      }
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
    // 🎯T274
    shouldPauseHistoryHydrate: shouldPauseHistoryHydrate,
    MERMAID_RENDER_TIMEOUT_MS: MERMAID_RENDER_TIMEOUT_MS,
    withMermaidRenderTimeout: withMermaidRenderTimeout,
    // 🎯T268
    parseSvgNaturalSize: parseSvgNaturalSize,
    computeContainScale: computeContainScale,
    planSingleGraphScaleToFill: planSingleGraphScaleToFill,
    svgScaleToFillStyle: svgScaleToFillStyle,
    applySvgScaleToFill: applySvgScaleToFill,
    // 🎯T276 / 🎯T277
    PACK_BLOCK_CHROME_H: PACK_BLOCK_CHROME_H,
    packBoxesIntoPaneAspect: packBoxesIntoPaneAspect,
    planMultiDiagramPackScaleToFill: planMultiDiagramPackScaleToFill,
    planWrapGridMicroIslandOracle: planWrapGridMicroIslandOracle,
    planChromeInflatedStretchOracle: planChromeInflatedStretchOracle,
    placementSvgAspectMatchesNatural: placementSvgAspectMatchesNatural,
    applyPackPlacement: applyPackPlacement,
    // 🎯T280 visual layout oracles
    assessTallEmptyColumnLayout: assessTallEmptyColumnLayout,
    assessSingleGraphPaneCover: assessSingleGraphPaneCover,
  };
}));
