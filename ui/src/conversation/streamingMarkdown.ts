// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import * as smd from 'streaming-markdown';

export type StreamSession = {
  writeFull: (raw: string, normalizeFn?: (s: string) => string) => void;
  end: () => void;
  destroy: () => void;
  readonly written: number;
  readonly text: string;
  readonly ended: boolean;
};

function normalizeText(raw: string, normalizeFn?: (s: string) => string): string {
  let text = raw == null ? '' : String(raw);
  if (typeof normalizeFn === 'function') {
    const n = normalizeFn(text);
    if (n != null) text = String(n);
  }
  return text;
}

function decorateAnchor(el: Element | undefined): void {
  if (!el || !el.setAttribute) return;
  if (String(el.tagName || '').toUpperCase() !== 'A') return;
  el.setAttribute('target', '_blank');
  el.setAttribute('rel', 'noopener noreferrer');
  el.setAttribute('tabindex', '-1');
}

function wrapSafeLinks(renderer: smd.Default_Renderer): smd.Default_Renderer {
  const orig = renderer.add_token;
  renderer.add_token = function addToken(data, type) {
    orig.call(this, data, type);
    if (type === smd.LINK || type === smd.RAW_URL) {
      decorateAnchor(data.nodes[data.index]);
    }
  };
  return renderer;
}

/**
 * Incremental smd session on one unsealed assistant root (🎯T150 / T64.4).
 * Pure append → delta write; rewrite / first write restarts the parser.
 */
export function createSession(rootEl: HTMLElement): StreamSession | null {
  if (!rootEl || typeof smd.parser !== 'function') return null;

  let parser: smd.Parser | null = null;
  let lastNormalized = '';
  let ended = false;

  function start(): void {
    rootEl.innerHTML = '';
    const renderer = wrapSafeLinks(smd.default_renderer(rootEl));
    parser = smd.parser(renderer);
    lastNormalized = '';
    ended = false;
  }

  start();

  return {
    writeFull(raw, normalizeFn) {
      if (ended || !parser) return;
      const text = normalizeText(raw, normalizeFn);
      if (text === lastNormalized) return;
      if (lastNormalized && text.startsWith(lastNormalized)) {
        const delta = text.slice(lastNormalized.length);
        if (delta) smd.parser_write(parser, delta);
        lastNormalized = text;
        return;
      }
      start();
      if (text && parser) smd.parser_write(parser, text);
      lastNormalized = text;
    },
    end() {
      if (ended || !parser) return;
      smd.parser_end(parser);
      ended = true;
    },
    destroy() {
      ended = true;
      parser = null;
      lastNormalized = '';
    },
    get written() {
      return lastNormalized.length;
    },
    get text() {
      return lastNormalized;
    },
    get ended() {
      return ended;
    },
  };
}
