// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Cheap continuation routing (🎯T99): term/n-gram match over thread
// titles + digests. No agent pass. DOM-free for Node tests.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ThreadRoute = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const STOP = new Set([
    'a', 'an', 'the', 'is', 'are', 'was', 'were', 'be', 'been', 'being',
    'to', 'of', 'in', 'on', 'for', 'and', 'or', 'but', 'with', 'at', 'by',
    'from', 'as', 'it', 'this', 'that', 'these', 'those', 'i', 'you', 'we',
    'they', 'he', 'she', 'my', 'your', 'our', 'how', 'what', 'when', 'where',
    'why', 'who', 'which', 'do', 'does', 'did', 'doing', 'done', 'have',
    'has', 'had', 's', 're', 've', 'll', 'd', 'm', 't', 'going', 'go',
    'about', 'just', 'really', 'please', 'thanks', 'thank',
  ]);

  function tokens(text) {
    return String(text || '')
      .toLowerCase()
      .replace(/[^a-z0-9._+/-]+/g, ' ')
      .split(/\s+/)
      .filter(function (t) {
        return t.length >= 2 && !STOP.has(t);
      });
  }

  function scoreThread(queryTokens, thread) {
    if (!thread || !queryTokens.length) return 0;
    const hay = tokens([thread.title, thread.digest, thread.body, thread.id].join(' '));
    if (!hay.length) return 0;
    const set = new Set(hay);
    let hits = 0;
    for (let i = 0; i < queryTokens.length; i++) {
      if (set.has(queryTokens[i])) hits++;
    }
    // Prefer multi-token hits; single stop-ish matches score low.
    if (hits === 0) return 0;
    return hits / queryTokens.length + (hits >= 2 ? 0.25 : 0);
  }

  // route(message, threads, opts) → { threadId|null, score, reason }
  // threads: [{ id, title, digest, body, updatedAt? }]
  // Explicit prefixes (aside:, main:, target:, pursue:) disable auto-route.
  function route(message, threads, opts) {
    opts = opts || {};
    const raw = String(message || '').trim();
    if (!raw) return { threadId: null, score: 0, reason: 'empty' };

    if (/^\s*(aside|capture|park|main|pursue|target)\s*:/i.test(raw)) {
      return { threadId: null, score: 0, reason: 'explicit-prefix' };
    }

    const q = tokens(raw);
    if (!q.length) return { threadId: null, score: 0, reason: 'no-terms' };

    const list = Array.isArray(threads) ? threads : [];
    let best = null;
    let bestScore = 0;
    let second = 0;
    for (let i = 0; i < list.length; i++) {
      const t = list[i];
      if (!t || !t.id || t.id === 'main') continue;
      const sc = scoreThread(q, t);
      if (sc > bestScore) {
        second = bestScore;
        bestScore = sc;
        best = t;
      } else if (sc > second) {
        second = sc;
      }
    }

    const minScore = typeof opts.minScore === 'number' ? opts.minScore : 0.45;
    const margin = typeof opts.margin === 'number' ? opts.margin : 0.12;
    if (!best || bestScore < minScore) {
      return { threadId: null, score: bestScore, reason: 'no-match' };
    }
    if (bestScore - second < margin && second > 0) {
      return { threadId: null, score: bestScore, reason: 'ambiguous' };
    }
    return { threadId: best.id, score: bestScore, reason: 'match' };
  }

  return {
    tokens: tokens,
    scoreThread: scoreThread,
    route: route,
  };
}));
