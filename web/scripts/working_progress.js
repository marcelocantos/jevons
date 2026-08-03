// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Mid-turn working-progress fleet rollup (🎯T202). DOM-free for Node tests.
// Fleet counts replace a single trailing rollup segment; they never append.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.WorkingProgress = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // "fleet N running · M stopped" — matches product chrome wording.
  var FLEET_SEGMENT_RE = /fleet \d+ running · \d+ stopped/g;
  var SEP = ' · ';

  function formatFleetRollup(running, stopped) {
    var r = Number(running);
    var s = Number(stopped);
    if (!isFinite(r) || r < 0) r = 0;
    if (!isFinite(s) || s < 0) s = 0;
    return 'fleet ' + Math.floor(r) + ' running · ' + Math.floor(s) + ' stopped';
  }

  /** Remove every fleet rollup segment; collapse leftover separators. */
  function stripFleetRollup(progress) {
    var s = String(progress == null ? '' : progress);
    s = s.replace(FLEET_SEGMENT_RE, '');
    // Collapse " · · " / leading / trailing seps left by removal.
    s = s.replace(/(?:\s*·\s*)+/g, SEP);
    s = s.replace(/^(?:\s*·\s*)+|(?:\s*·\s*)+$/g, '');
    return s.trim();
  }

  /**
   * Merge tool-step (or other) progress with a single fleet rollup.
   * Two successive calls with different counts still yield one fleet segment.
   *
   * @param {string} currentProgress
   * @param {number} running
   * @param {number} stopped
   * @returns {string}
   */
  function mergeFleetProgress(currentProgress, running, stopped) {
    var base = stripFleetRollup(currentProgress);
    var fleet = formatFleetRollup(running, stopped);
    return base ? base + SEP + fleet : fleet;
  }

  /** Count fleet rollup segments in a progress string (oracle helper). */
  function countFleetSegments(progress) {
    var s = String(progress == null ? '' : progress);
    var m = s.match(FLEET_SEGMENT_RE);
    return m ? m.length : 0;
  }

  return {
    formatFleetRollup: formatFleetRollup,
    stripFleetRollup: stripFleetRollup,
    mergeFleetProgress: mergeFleetProgress,
    countFleetSegments: countFleetSegments,
  };
}));
