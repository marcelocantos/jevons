// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T143 / T138 — pure reconnect + degraded-mode policy (DOM-free).
// Soft reconnect: keep transcript DOM; ignore history firehose until history_meta.
// Hard reconnect: wipe + full replay (first paint, empty transcript).

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ChatReconnect = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  /**
   * @param {object} o
   * @param {number} o.msgCount - existing .msg nodes in #messages
   * @param {boolean} o.everReady - true once we completed history_meta / ui_ready
   * @param {boolean} [o.forceHard] - version change / explicit hard reload
   */
  function shouldSoftReconnect(o) {
    o = o || {};
    if (o.forceHard) return false;
    if (!o.everReady) return false;
    const n = typeof o.msgCount === 'number' ? o.msgCount : 0;
    return n > 0;
  }

  /**
   * While soft-reconnecting, drop history/live stream frames until meta.
   * Always keep: conn, history_meta, error, status, agents_changed, rewound, pong.
   * 🎯T209: agent_transcript (inspect multiplex) is also kept so RHS inspect
   * does not go dark across soft reconnect while main firehose is suppressed.
   */
  function shouldSuppressFrame(softActive, msg) {
    if (!softActive) return false;
    const m = msg || {};
    const typ = m.type == null ? '' : String(m.type);
    if (typ === 'conn' || typ === 'history_meta' || typ === 'error' ||
        typ === 'status' || typ === 'agents_changed' || typ === 'rewound' ||
        typ === 'cost_alert' || typ === 'pong' || typ === 'agent_transcript' ||
        typ === 'frontier_changed') {
      return false;
    }
    // Plain chat stream / user echo / assistant tokens — suppress during soft.
    return true;
  }

  function isOverseerDownError(errText) {
    const s = String(errText || '').toLowerCase();
    if (s.indexOf('overseer') >= 0 && (
      s.indexOf('not running') >= 0 ||
      s.indexOf('down') >= 0
    )) return true;
    // T54 fresh-install reason — cockpit unusable, treat as degraded.
    if (s.indexOf('grok') >= 0 && s.indexOf('not installed') >= 0) return true;
    return false;
  }

  // Long backoff when degraded (ms). Index by attempt.
  const DEGRADED_BACKOFF_MS = [2000, 5000, 10000, 15000, 30000];

  function degradedBackoffMs(attempt) {
    const a = typeof attempt === 'number' && attempt > 0 ? attempt : 0;
    const i = Math.min(a, DEGRADED_BACKOFF_MS.length - 1);
    return DEGRADED_BACKOFF_MS[i];
  }

  return {
    shouldSoftReconnect: shouldSoftReconnect,
    shouldSuppressFrame: shouldSuppressFrame,
    isOverseerDownError: isOverseerDownError,
    degradedBackoffMs: degradedBackoffMs,
    DEGRADED_BACKOFF_MS: DEGRADED_BACKOFF_MS,
  };
}));
