// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Markdown pre-parse normalization for the chat UI (🎯T145, 🎯T146).
// Keep this file free of DOM so Node can require() it.
//
// Models often glue intro blurb to a fenced code block with no newline:
//   Here's a snippet:```cpp
//   int x;
//   ```
// marked then fails to open a code fence (fence sticks to the sentence).
// ensureFenceNewlines inserts a blank line before an opening fence when
// it is glued to a preceding non-newline character *and* already starts a
// multi-line fence (fence token followed by a newline).
//
// 🎯T146: mid-prose examples of fence markers must not be promoted into
// openers. "use ``` in docs" stays on one line — only prose:```lang\n…
// (real smushed block) is rewritten. Without the newline lookahead, the
// blank-line insert would open an empty/broken code block through EOF.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.MarkdownNormalize = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Opening fence glued to prose: ``` or ```lang (lang: alnum / _ / + / -),
  // and already line-terminated (real block start). Lookahead keeps mid-prose
  // ``` illustrations (space/punct after ticks) unchanged (🎯T146).
  // Capture prior non-newline char (no lookbehind) for older engines.
  const SMUSHED_OPEN_FENCE = /([^\n\r])(```[a-zA-Z0-9_+-]*)(?=\r?\n)/g;

  /**
   * Ensure markdown fenced code blocks are not fused to preceding prose.
   * Idempotent for already-well-formed markdown (fence after newline).
   * Does not alter fence lines that already start at column 0 of a line.
   * Does not treat mid-prose ``` / ```lang examples as fence openers (🎯T146).
   *
   * @param {string} text
   * @returns {string}
   */
  function ensureFenceNewlines(text) {
    if (text == null || text === '') return text == null ? text : '';
    if (typeof text !== 'string') text = String(text);
    return text.replace(SMUSHED_OPEN_FENCE, '$1\n\n$2');
  }

  return {
    ensureFenceNewlines: ensureFenceNewlines,
  };
}));
