// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Progressive streaming markdown sessions for assistant bubbles (🎯T150).
// Wraps thetarnav/streaming-markdown (vendored as smd.js): delta parser_write
// on each coalesce frame; seal path stays full marked (mermaid/highlight).
//
// No product path uses textContent for mid-stream assistant paint.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory(
      typeof globalThis !== 'undefined' ? globalThis : root,
      typeof require === 'function' ? require : null,
    );
  } else {
    root.StreamingMarkdown = factory(root, null);
  }
}(typeof self !== 'undefined' ? self : this, function (root, req) {
  'use strict';

  function resolveSmd(smdLib) {
    if (smdLib) return smdLib;
    if (root && root.smd) return root.smd;
    if (req) {
      try { return req('./smd.js'); } catch (_) { /* Node without sibling */ }
    }
    return null;
  }

  function normalizeText(raw, normalizeFn) {
    let text = raw == null ? '' : String(raw);
    if (typeof normalizeFn === 'function') {
      const n = normalizeFn(text);
      if (n != null) text = String(n);
    }
    return text;
  }

  /**
   * Bind a progressive parser to a DOM root (browser default_renderer).
   * writeFull feeds only the delta when the normalized text is a pure append;
   * otherwise restarts the parser (selection may reset on rewrite only).
   *
   * @param {HTMLElement} rootEl
   * @param {object} [smdLib]
   * @returns {object|null} session or null if smd unavailable
   */
  function createSession(rootEl, smdLib) {
    const smd = resolveSmd(smdLib);
    if (!smd || !rootEl || typeof smd.parser !== 'function') return null;

    let parser = null;
    let lastNormalized = '';
    let ended = false;

    function start() {
      if (typeof rootEl.innerHTML === 'string') rootEl.innerHTML = '';
      let renderer = smd.default_renderer(rootEl);
      // 🎯T151: progressive <a> open in new tab (do not edit vendored smd.js).
      if (root.LinkSafety && typeof root.LinkSafety.wrapSmdDefaultRenderer === 'function') {
        renderer = root.LinkSafety.wrapSmdDefaultRenderer(smd, renderer);
      }
      parser = smd.parser(renderer);
      lastNormalized = '';
      ended = false;
    }

    start();

    return {
      /**
       * @param {string} raw full accumulated stream text
       * @param {function(string):string} [normalizeFn] e.g. ensureFenceNewlines
       */
      writeFull: function writeFull(raw, normalizeFn) {
        if (ended || !parser) return;
        const text = normalizeText(raw, normalizeFn);
        if (text === lastNormalized) return;
        // Pure append → delta write (keeps selection on already-painted nodes).
        if (lastNormalized && text.startsWith(lastNormalized)) {
          const delta = text.slice(lastNormalized.length);
          if (delta) smd.parser_write(parser, delta);
          lastNormalized = text;
          return;
        }
        // Rewrite / first write / normalization inserted mid-string.
        start();
        if (text) smd.parser_write(parser, text);
        lastNormalized = text;
      },

      end: function end() {
        if (ended || !parser) return;
        smd.parser_end(parser);
        ended = true;
      },

      destroy: function destroy() {
        ended = true;
        parser = null;
        lastNormalized = '';
      },

      get written() { return lastNormalized.length; },
      get text() { return lastNormalized; },
      get ended() { return ended; },
    };
  }

  /**
   * DOM-free progressive render for hermetic Node tests.
   * Feeds successive chunks (each full text so far, or true deltas if
   * options.deltas is true) through a string renderer.
   *
   * @param {string[]} chunks
   * @param {object} [opts]
   * @param {object} [opts.smd]
   * @param {function(string):string} [opts.normalize]
   * @param {boolean} [opts.deltas] if true, chunks are raw deltas not full text
   * @returns {{ html: string, text: string }}
   */
  function renderProgressive(chunks, opts) {
    opts = opts || {};
    const smd = resolveSmd(opts.smd);
    if (!smd) throw new Error('streaming-markdown (smd) not available');

    const renderer = stringRenderer(smd);
    const parser = smd.parser(renderer);
    let last = '';
    const list = Array.isArray(chunks) ? chunks : [];

    for (let i = 0; i < list.length; i++) {
      let piece = list[i] == null ? '' : String(list[i]);
      if (typeof opts.normalize === 'function') {
        // For full-text mode normalize each snapshot; for deltas only first path.
        if (!opts.deltas) piece = normalizeText(piece, opts.normalize);
      }
      if (opts.deltas) {
        if (piece) smd.parser_write(parser, piece);
        last += piece;
      } else {
        if (piece === last) continue;
        if (last && piece.startsWith(last)) {
          const delta = piece.slice(last.length);
          if (delta) smd.parser_write(parser, delta);
        } else {
          // restart
          // string renderer has no DOM clear — rebuild via new parser path
          return renderProgressiveRestart(list.slice(0, i + 1), opts, smd);
        }
        last = piece;
      }
    }
    smd.parser_end(parser);
    return { html: renderer.toHTML(), text: renderer.toText() };
  }

  function renderProgressiveRestart(chunks, opts, smd) {
    // Write only the final full snapshot when history is non-monotonic.
    const last = chunks[chunks.length - 1] == null ? '' : String(chunks[chunks.length - 1]);
    const text = typeof opts.normalize === 'function' ? normalizeText(last, opts.normalize) : last;
    const renderer = stringRenderer(smd);
    const parser = smd.parser(renderer);
    if (text) smd.parser_write(parser, text);
    smd.parser_end(parser);
    return { html: renderer.toHTML(), text: renderer.toText() };
  }

  /**
   * Snapshot at each full-text step without ending the parser until the last
   * call — used to assert mid-stream DOM shape after the closer arrives.
   *
   * @param {string[]} fullTexts successive full accumulated strings
   * @param {object} [opts]
   * @returns {{ steps: Array<{html:string,text:string}>, final: {html:string,text:string} }}
   */
  function renderSteps(fullTexts, opts) {
    opts = opts || {};
    const smd = resolveSmd(opts.smd);
    if (!smd) throw new Error('streaming-markdown (smd) not available');

    const renderer = stringRenderer(smd);
    const parser = smd.parser(renderer);
    let last = '';
    const steps = [];
    const list = Array.isArray(fullTexts) ? fullTexts : [];

    for (let i = 0; i < list.length; i++) {
      let piece = list[i] == null ? '' : String(list[i]);
      if (typeof opts.normalize === 'function') piece = normalizeText(piece, opts.normalize);
      if (piece !== last) {
        if (last && piece.startsWith(last)) {
          const delta = piece.slice(last.length);
          if (delta) smd.parser_write(parser, delta);
        } else if (!last) {
          if (piece) smd.parser_write(parser, piece);
        } else {
          // Non-monotonic: fall back to single-shot final only for remainder.
          return renderStepsNonMonotonic(list, opts, smd);
        }
        last = piece;
      }
      steps.push({ html: renderer.toHTML(), text: renderer.toText() });
    }
    smd.parser_end(parser);
    const final = { html: renderer.toHTML(), text: renderer.toText() };
    return { steps: steps, final: final };
  }

  function renderStepsNonMonotonic(list, opts, smd) {
    const steps = [];
    for (let i = 0; i < list.length; i++) {
      const r = renderProgressive(list.slice(0, i + 1), opts);
      steps.push(r);
    }
    return { steps: steps, final: steps[steps.length - 1] };
  }

  function stringRenderer(smd) {
    const stack = [{ tag: '#root', children: [], attrs: {} }];

    function tagFor(type) {
      switch (type) {
        case smd.PARAGRAPH: return 'p';
        case smd.HEADING_1: return 'h1';
        case smd.HEADING_2: return 'h2';
        case smd.HEADING_3: return 'h3';
        case smd.HEADING_4: return 'h4';
        case smd.HEADING_5: return 'h5';
        case smd.HEADING_6: return 'h6';
        case smd.CODE_BLOCK: return 'pre';
        case smd.CODE_FENCE: return 'pre';
        case smd.CODE_INLINE: return 'code';
        case smd.ITALIC_AST:
        case smd.ITALIC_UND: return 'em';
        case smd.STRONG_AST:
        case smd.STRONG_UND: return 'strong';
        case smd.STRIKE: return 's';
        case smd.LINK:
        case smd.RAW_URL: return 'a';
        case smd.IMAGE: return 'img';
        case smd.BLOCKQUOTE: return 'blockquote';
        case smd.LINE_BREAK: return 'br';
        case smd.RULE: return 'hr';
        case smd.LIST_UNORDERED: return 'ul';
        case smd.LIST_ORDERED: return 'ol';
        case smd.LIST_ITEM: return 'li';
        case smd.CHECKBOX: return 'input';
        case smd.TABLE: return 'table';
        case smd.TABLE_ROW: return 'tr';
        case smd.TABLE_CELL: return 'td';
        case smd.EQUATION_BLOCK: return 'equation-block';
        case smd.EQUATION_INLINE: return 'equation-inline';
        default: return 'span';
      }
    }

    function ser(n) {
      if (n.tag === '#text') return n.text;
      if (n.tag === '#root') return n.children.map(ser).join('');
      const attrs = Object.keys(n.attrs).map(function (k) {
        return ' ' + k + '="' + String(n.attrs[k]).replace(/"/g, '&quot;') + '"';
      }).join('');
      const voidish = { br: 1, hr: 1, img: 1, input: 1 };
      if (voidish[n.tag]) return '<' + n.tag + attrs + '>';
      return '<' + n.tag + attrs + '>' + n.children.map(ser).join('') + '</' + n.tag + '>';
    }

    function textOf(n) {
      if (n.tag === '#text') return n.text;
      return (n.children || []).map(textOf).join('');
    }

    return {
      data: stack,
      add_token: function (_data, type) {
        if (type === smd.DOCUMENT) return;
        const node = { tag: tagFor(type), children: [], attrs: {} };
        if (type === smd.CHECKBOX) {
          node.attrs.type = 'checkbox';
          node.attrs.disabled = 'disabled';
        }
        // CODE_FENCE/BLOCK: mirror default_renderer pre>code nesting lightly.
        if (type === smd.CODE_FENCE || type === smd.CODE_BLOCK) {
          const pre = { tag: 'pre', children: [], attrs: {} };
          const code = { tag: 'code', children: [], attrs: {} };
          stack[stack.length - 1].children.push(pre);
          pre.children.push(code);
          stack.push(pre);
          stack.push(code);
          return;
        }
        // 🎯T151: hermetic/string path — anchors carry target=_blank + safe rel.
        if (node.tag === 'a') {
          node.attrs.target = '_blank';
          node.attrs.rel = 'noopener noreferrer';
        }
        stack[stack.length - 1].children.push(node);
        stack.push(node);
      },
      end_token: function () {
        if (stack.length > 1) stack.pop();
      },
      add_text: function (_data, text) {
        stack[stack.length - 1].children.push({ tag: '#text', text: String(text) });
      },
      set_attr: function (_data, type, value) {
        const cur = stack[stack.length - 1];
        try {
          const name = smd.attr_to_html_attr(type);
          if (type === smd.LANG) {
            cur.attrs['class'] = 'language-' + value;
          } else if (name) {
            cur.attrs[name] = value;
          }
        } catch (_) {
          cur.attrs['data-attr'] = value;
        }
        // Re-assert after href arrives so ensure attrs survive set_attr.
        if (cur.tag === 'a') {
          cur.attrs.target = '_blank';
          cur.attrs.rel = 'noopener noreferrer';
        }
      },
      toHTML: function () { return ser(stack[0]); },
      toText: function () { return textOf(stack[0]); },
    };
  }

  return {
    createSession: createSession,
    renderProgressive: renderProgressive,
    renderSteps: renderSteps,
    resolveSmd: resolveSmd,
  };
}));
