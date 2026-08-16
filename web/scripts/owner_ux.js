// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T361 — client half of the 🎯T355 owner-interaction UX-degrade path
// (DOM-free policy; the wiring lives in web/index.html).
//
// The server observes owner interaction and actuates: it publishes level
// truth, hints a UX yield/hydrate on the degrade class, and escalates an
// unrecovered gap to an owner-visible alert. Three client halves close the
// loop:
//
//   1. yield/hydrate — a status frame carrying ux:"yield_hydrate" is a
//      request to release the main thread and repaint from shells (🎯T349),
//      not chrome text to ignore.
//   2. sticky class banner — "owner interaction degraded: …" is its own
//      degraded class. T138's banner only knows overseer-down, so the alert
//      would otherwise land as one more error bubble in a transcript the
//      owner may not be looking at.
//   3. composer-blocked reporting — converge.OwnerObservation.ComposerBlocked
//      is a *client-reported* level. With no reporter it is always false and
//      UX degrade is inferred from heartbeat staleness alone, which cannot
//      see a live tab whose composer refuses to submit.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.OwnerUX = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Wire vocabulary, shared with internal/server/owner_health.go.
  const UX_YIELD_HYDRATE = 'yield_hydrate';
  const UX_RECOVERED = 'recovered';
  const DEGRADE_PREFIX = 'owner interaction degraded:';
  const RECOVERED_PREFIX = 'owner interaction recovered:';

  /**
   * A UX-coordinate hint from the server's owner-health actuator.
   * The hint rides a status frame (it also carries level truth), so the
   * chrome branches still run — this only adds the perf-path action.
   */
  function isYieldHydrateFrame(msg) {
    const m = msg || {};
    return String(m.type || '') === 'status' && String(m.ux || '') === UX_YIELD_HYDRATE;
  }

  /**
   * The last-rung owner alert for this class (an error frame the server also
   * journals). Reason is the alert text minus the class prefix.
   */
  function ownerDegradeReason(errText) {
    const s = String(errText == null ? '' : errText).trim();
    if (s.toLowerCase().indexOf(DEGRADE_PREFIX) !== 0) return '';
    const rest = s.slice(DEGRADE_PREFIX.length).trim();
    return rest || 'unspecified';
  }

  function isOwnerDegradeAlert(errText) {
    return ownerDegradeReason(errText) !== '';
  }

  /**
   * The recovery frame: the escalated gap was satisfied by an observation.
   * Carried on a status frame with no state, so it cannot settle working
   * chrome mid-turn (🎯T260) on its way to clearing the banner.
   */
  /**
   * Current owner-UX level on a status frame. "degraded" / "ok" are
   * levels; recovered is ok. Empty means this frame does not speak.
   */
  function ownerUXLevel(msg) {
    const m = msg || {};
    if (String(m.type || '') === 'history_meta') {
      const u = String(m.owner_ux || '');
      if (u === 'degraded' || u === 'ok') return u;
      return '';
    }
    if (isOwnerRecoveredFrame(m)) return 'ok';
    if (String(m.type || '') !== 'status') return '';
    const u = String(m.owner_ux || '');
    if (u === 'degraded' || u === 'ok') return u;
    return '';
  }

  function isOwnerRecoveredFrame(msg) {
    const m = msg || {};
    if (String(m.type || '') !== 'status') return false;
    if (String(m.ux || '') === UX_RECOVERED) return true;
    return String(m.text || '').trim().toLowerCase().indexOf(RECOVERED_PREFIX) === 0;
  }

  /**
   * Banner copy. Overseer-down (🎯T138) outranks the interaction class: a
   * dead overseer explains the degrade, and stacking two lines in one banner
   * reads as two faults.
   */
  function bannerText(state) {
    const s = state || {};
    if (s.overseerDown) {
      return s.overseerReason
        ? ('Cockpit degraded: ' + s.overseerReason + ' — history stays; send blocked until overseer is back.')
        : 'Cockpit degraded: overseer is not running.';
    }
    if (s.ownerDegraded) {
      return 'Owner interaction degraded: ' + (s.ownerReason || 'unspecified') +
        ' — chat may be stale until this clears.';
    }
    return '';
  }

  /**
   * Composer-blocked level. "Blocked" is the owner's question — can I submit
   * a turn right now? — not a widget attribute, so a disabled send button, a
   * hard-disabled input and the T138 degraded hold all count.
   *
   * A closed socket is deliberately NOT blocked-and-reportable: nothing can
   * be reported over a socket that is down, and the server already sees the
   * connection go. Callers gate reporting on socketOpen.
   */
  function composerBlocked(state) {
    const s = state || {};
    if (s.sendDisabled) return true;
    if (s.inputDisabled) return true;
    if (s.overseerDown) return true;
    return false;
  }

  function composerBlockedReason(state) {
    const s = state || {};
    if (s.overseerDown) return 'overseer_down';
    if (s.inputDisabled) return 'input_disabled';
    if (s.sendDisabled) return 'send_disabled';
    return '';
  }

  /**
   * Edge policy: report only when the level changes (or has never been
   * reported), so a healthy composer costs one frame per connection rather
   * than one per tick.
   */
  function shouldReportComposer(prev, blocked, socketOpen) {
    if (!socketOpen) return false;
    if (prev === null || prev === undefined) return true;
    return !!prev !== !!blocked;
  }

  function composerStateFrame(blocked, reason) {
    const frame = { type: 'ux_state', composer_blocked: !!blocked };
    if (blocked && reason) frame.reason = String(reason);
    return JSON.stringify(frame);
  }

  return {
    UX_YIELD_HYDRATE: UX_YIELD_HYDRATE,
    UX_RECOVERED: UX_RECOVERED,
    isYieldHydrateFrame: isYieldHydrateFrame,
    isOwnerDegradeAlert: isOwnerDegradeAlert,
    ownerDegradeReason: ownerDegradeReason,
    isOwnerRecoveredFrame: isOwnerRecoveredFrame,
    ownerUXLevel: ownerUXLevel,
    bannerText: bannerText,
    composerBlocked: composerBlocked,
    composerBlockedReason: composerBlockedReason,
    shouldReportComposer: shouldReportComposer,
    composerStateFrame: composerStateFrame,
  };
}));
