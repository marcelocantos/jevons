// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { marked } from 'marked';
import { linkifyTargetIDsInHTML } from '../frontier/targetHotspot';

const SMUSHED_OPEN_FENCE = /([^\n\r])(```[a-zA-Z0-9_+-]*)(?=\r?\n)/g;

/** Lifted from web/scripts/markdown_normalize.js (🎯T145 / 🎯T146). */
export function ensureFenceNewlines(text: string | null | undefined): string {
  if (text == null || text === '') return text == null ? '' : '';
  return String(text).replace(SMUSHED_OPEN_FENCE, '$1\n\n$2');
}

/**
 * Same seal path as web/index.html parseAssistantMarkdown: normalize smushed
 * fences, then marked.parse with default GFM (breaks off). Single newlines
 * stay in the HTML; CSS white-space:normal collapses them. Two-space line
 * ends (`  \\n`) are GFM hard breaks — that is how the golden wraps, not a
 * marked `breaks: true` fork.
 */
export function parseAssistantMarkdown(text: string): string {
  const raw = ensureFenceNewlines(text);
  const html = marked.parse(raw == null ? '' : raw, { async: false }) as string;
  return linkifyTargetIDsInHTML(html);
}
