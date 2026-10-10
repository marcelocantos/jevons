// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** 🎯T326 — 🎯Tn in chat HTML are hotspots that share the frontier card. */

import type { FrontierRow } from './table';

const TARGET_TOKEN_RE = /(?:🎯\s*)?(?:(?<repo>(?:[a-z][a-z0-9.-]*\/){0,2}[a-z][a-z0-9_-]*)\/(?:🎯\s*)?)?(?<id>T\d+(?:\.\d+)*)\b/g;

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

/** 🎯T1037 — embedded target id in a fleet seat name (repo prefix + tN[.N]*). */
export type SeatNameTargetParts = {
  prefix: string;
  matched: string;
  suffix: string;
  id: string;
};

const SEAT_NAME_TARGET_RE = /(^|-)(t\d+(?:\.\d+)*)(?=-|$)/i;

export function splitSeatNameTarget(name: string | null | undefined): SeatNameTargetParts | null {
  const s = String(name ?? '');
  if (!s) return null;
  const m = SEAT_NAME_TARGET_RE.exec(s);
  if (!m) return null;
  const delim = m[1];
  const matched = m[2];
  const rawStart = m.index + delim.length;
  const id = normalizeTargetID(matched);
  if (!id) return null;
  return {
    prefix: s.slice(0, rawStart),
    matched,
    suffix: s.slice(rawStart + matched.length),
    id,
  };
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

export function hotspotSpan(tid: string, label: string, repo?: string): string {
  return (
    '<span class="target-hotspot target-hotspot-finger" data-target-id="' +
    escapeAttr(tid) +
    '"' + (repo ? ' data-target-repo="' + escapeAttr(repo) + '"' : '') +
    ' role="button" tabindex="0">' +
    escapeText(label) +
    '</span>'
  );
}

export function linkifyTargetText(text: string, defaultRepo?: string): string {
  const s = String(text ?? '');
  if (!s) return s;
  TARGET_TOKEN_RE.lastIndex = 0;
  return s.replace(TARGET_TOKEN_RE, (full, ...args: unknown[]) => {
    const offset = args[args.length - 3] as number;
    const groups = args[args.length - 1] as { repo?: string; id: string };
    // A target inside a larger path or identifier is not an independent reference.
    if (offset > 0 && /[\w/.-]/.test(s[offset - 1])) return full;
    const tid = normalizeTargetID(groups.id);
    if (!tid) return full;
    if (groups.repo?.includes('/') && !groups.repo.startsWith('github.com/')) return full;
    const repo = groups.repo?.toLowerCase() || defaultRepo;
    return hotspotSpan(tid, repo ? `${repo}/🎯${tid}` : formatDisplayTargetID(tid), repo);
  });
}

/** Linkify target ids in HTML, skipping code/pre/a and existing hotspots. */
export function linkifyTargetIDsInHTML(html: string | null | undefined, defaultRepo?: string): string {
  if (html == null || html === '') return html == null ? '' : '';
  const s = String(html);
  if (!/T\d/.test(s)) return s;

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
    out += skipDepth > 0 ? chunk : linkifyTargetText(chunk, defaultRepo);
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

/** Seat names are not repo identifiers: scope is derived from the seat workdir. */
export function repoFromWorkdir(workdir?: string): string | undefined {
  const path = String(workdir || '').replace(/\/$/, '');
  // Isolated worktrees are siblings of the source checkout. Extract the
  // checkout identity from the path, never the seat name or its jv-/ge- prefix.
  const worktree = /\/\.([a-z][a-z0-9_-]*)-worktrees-[^/]+\/[^/]+$/.exec(path);
  const repo = worktree?.[1] || path.split('/').pop();
  if (!repo || !/^[a-z][a-z0-9_-]*$/.test(repo) || repo === 'jevons') return undefined;
  const parent = worktree ? path.slice(0, worktree.index) : path.slice(0, -(repo.length + 1));
  const identity = /\/(github\.com\/[^/]+)$/.exec(parent);
  return identity ? identity[1] + '/' + repo : repo;
}
