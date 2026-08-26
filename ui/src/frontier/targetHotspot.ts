// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** 🎯T326 — 🎯Tn in chat HTML are hotspots that share the frontier card. */

import type { FrontierRow } from './table';

const TARGET_TOKEN_RE = /(?:🎯\s*)?(T\d+(?:\.\d+)*)\b/g;

const SKIP_TAGS: Record<string, boolean> = {
  CODE: true,
  PRE: true,
  A: true,
  SCRIPT: true,
  STYLE: true,
  TEXTAREA: true,
};

export function normalizeTargetID(raw: string | null | undefined): string {
  let s = raw == null ? '' : String(raw).trim();
  if (!s) return '';
  s = s.replace(/^🎯\s*/, '').trim();
  if (!s) return '';
  if (s.charAt(0) === 't') s = 'T' + s.slice(1);
  return s;
}

export function formatDisplayTargetID(raw: string | null | undefined): string {
  const id = normalizeTargetID(raw);
  return id ? '🎯' + id : '';
}

function escapeAttr(s: string): string {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

function escapeText(s: string): string {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

export function hotspotSpan(tid: string, label: string): string {
  return (
    '<span class="target-hotspot target-hotspot-finger" data-target-id="' +
    escapeAttr(tid) +
    '" role="button" tabindex="0">' +
    escapeText(label) +
    '</span>'
  );
}

export function linkifyTargetText(text: string): string {
  const s = String(text ?? '');
  if (!s) return s;
  TARGET_TOKEN_RE.lastIndex = 0;
  return s.replace(TARGET_TOKEN_RE, (full, id: string) => {
    const tid = normalizeTargetID(id);
    if (!tid) return full;
    return hotspotSpan(tid, formatDisplayTargetID(tid));
  });
}

/** Linkify target ids in HTML, skipping code/pre/a and existing hotspots. */
export function linkifyTargetIDsInHTML(html: string | null | undefined): string {
  if (html == null || html === '') return html == null ? '' : '';
  const s = String(html);
  if (!/(?:🎯\s*)?T\d/.test(s)) return s;

  let out = '';
  let i = 0;
  const n = s.length;
  let skipDepth = 0;
  let skipTag = '';

  while (i < n) {
    if (s.charAt(i) === '<') {
      const close = s.indexOf('>', i);
      if (close < 0) {
        out += s.slice(i);
        break;
      }
      const tag = s.slice(i, close + 1);
      out += tag;
      const mOpen = /^<\s*([a-zA-Z0-9:-]+)/.exec(tag);
      const mClose = /^<\s*\/\s*([a-zA-Z0-9:-]+)/.exec(tag);
      const selfClose = /\/\s*>$/.test(tag);
      if (mClose) {
        const cname = mClose[1].toUpperCase();
        if (skipDepth > 0 && cname === skipTag) {
          skipDepth--;
          if (skipDepth === 0) skipTag = '';
        }
      } else if (mOpen && !selfClose) {
        const oname = mOpen[1].toUpperCase();
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

    const next = s.indexOf('<', i);
    const end = next < 0 ? n : next;
    const chunk = s.slice(i, end);
    out += skipDepth > 0 ? chunk : linkifyTargetText(chunk);
    i = end;
  }
  return out;
}

export function findRowByTargetID(
  rows: ReadonlyArray<Partial<FrontierRow> | null | undefined> | null | undefined,
  targetId: string,
): Partial<FrontierRow> | null {
  const want = normalizeTargetID(targetId);
  if (!want || !rows) return null;
  for (const r of rows) {
    if (!r) continue;
    if (normalizeTargetID(r.id) === want) return r;
  }
  return null;
}

export function minimalRowForID(targetId: string): FrontierRow | null {
  const id = normalizeTargetID(targetId);
  if (!id) return null;
  return { id, name: '', status: '' };
}
