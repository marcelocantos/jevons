// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Aside-wire user bodies (🎯T250 / 🎯T264 / 🎯T265).
 * Vanilla: web/scripts/attention_threads.js — ported, not imported.
 *
 * Main #messages must not paint [attention:] / [target-aside:] owner
 * wires. Sidebar inspect may show the stripped body.
 */

export type AsideWireKind = 'attention' | 'target-aside';

export type AsideWire = {
  kind: AsideWireKind;
  id: string;
  title: string;
  body: string;
  displayText: string;
};

const LEADING_IMAGE_MARKERS_RE = /^\s*(?:\[image:\s*[^\]]*\]\s*)+/i;

export function parseAsideWireUserText(text: string): AsideWire | null {
  const raw = String(text ?? '');
  if (!raw) return null;
  const att = raw.match(
    /^\s*\[attention\s*:\s*([^|\]\r\n]+)\|([^\]]*)\]\s*(?:\r?\n([\s\S]*))?$/i,
  );
  if (att) {
    const id = String(att[1] || '').trim();
    const title = String(att[2] || '').trim();
    const body = String(att[3] != null ? att[3] : '').replace(/^\r?\n/, '');
    if (!id) return null;
    return {
      kind: 'attention',
      id,
      title,
      body,
      displayText: body.trim() || title || id,
    };
  }
  const tgt = raw.match(
    /^\s*\[target-aside\s*:\s*([^|\]]+?)\s*\|\s*([^\]]*)\]\s*(?:\r?\n([\s\S]*))?$/i,
  );
  if (tgt) {
    const id = String(tgt[1] || '').trim();
    const title = String(tgt[2] || '').trim();
    let body = String(tgt[3] != null ? tgt[3] : '').replace(/^\r?\n/, '');
    body = body.replace(/\n\n\(Ceremony:[\s\S]*$/i, '').trim();
    if (!id) return null;
    return {
      kind: 'target-aside',
      id,
      title,
      body,
      displayText: body || title || id,
    };
  }
  return null;
}

/** 🎯T264: first non-empty line is an aside header — flash-safe vs strict parse. */
export function looksLikeAsideWireMarker(text: string): boolean {
  const raw = String(text ?? '');
  if (!raw) return false;
  let t = raw.replace(LEADING_IMAGE_MARKERS_RE, '');
  t = t.replace(/^\s+/, '');
  if (/^\[attention\s*:/i.test(t) || /^\[target-aside\s*:/i.test(t)) return true;
  const lines = t.split(/\r?\n/);
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    if (!line) continue;
    if (/^\[attention\s*:/i.test(line) || /^\[target-aside\s*:/i.test(line)) return true;
    return false;
  }
  return false;
}

export function isAsideWireUserText(text: string): boolean {
  if (parseAsideWireUserText(text) != null) return true;
  return looksLikeAsideWireMarker(text);
}

/** Main transcript paints this user body (false for aside wires). */
export function shouldPaintMainUserText(text: string): boolean {
  return !isAsideWireUserText(text);
}

/** 🎯T265: sidebar inspect strips the raw wire header. */
export function inspectDisplayUserText(text: string): string {
  const raw = String(text ?? '');
  const parsed = parseAsideWireUserText(raw);
  if (parsed) return parsed.displayText;
  return raw.replace(/^\s*\[(?:attention|target-aside)\s*:[^\]]*\]\s*(?:\r?\n)?/i, '').trim();
}
