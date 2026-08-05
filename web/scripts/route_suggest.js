// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T135: opt-in route switch helpers. Match is suggestion-only; default
// send stays on main. DOM-free for Node tests.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.RouteSuggest = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // True when a ThreadRoute hit should surface switch chrome.
  function shouldShowSwitch(hit) {
    if (!hit) return false;
    if (hit.reason !== 'match') return false;
    const id = hit.threadId;
    return id != null && id !== '' && id !== 'main';
  }

  // 🎯T247: after handleComposer for target:/aside:/capture: (or any already-
  // routed send), never plan a Continue-in / create-aside affordance.
  // composerResult is the AttentionThreads.handleComposer return value.
  function shouldSkipRouteSuggest(composerResult) {
    if (!composerResult || typeof composerResult !== 'object') return false;
    if (composerResult.routed) return true;
    if (composerResult.purpose === 'file-target') return true;
    if (composerResult.threadId &&
        (composerResult.kind === 'send' || composerResult.kind === 'local')) {
      // Capture is local+threadId; aside/target are send+threadId+routed.
      return true;
    }
    return false;
  }

  // Label for the affordance: Continue in: «title»
  function formatSwitchLabel(threadOrTitle) {
    let title = '';
    if (threadOrTitle == null) {
      title = '';
    } else if (typeof threadOrTitle === 'string') {
      title = threadOrTitle;
    } else if (typeof threadOrTitle === 'object') {
      title = threadOrTitle.title != null ? String(threadOrTitle.title) : '';
    }
    title = String(title || '').replace(/\s+/g, ' ').trim();
    if (!title) title = 'aside';
    // Cap long titles so the chip stays scannable.
    if (title.length > 48) title = title.slice(0, 45).trim() + '…';
    return 'Continue in: «' + title + '»';
  }

  // planMainSend(routeHit, opts) → always main wire; optional suggestion.
  // opts.threads: optional list to resolve title for the hit threadId.
  // opts.body: optional message body stored on suggestion for later switch.
  // opts.composerResult: handleComposer outcome — when already open/routed
  // (🎯T247), suggestion stays null (no create/continue affordance).
  // Default path NEVER rewrites wire into an attention thread (🎯T135).
  function planMainSend(routeHit, opts) {
    opts = opts || {};
    const hit = routeHit || {};
    const out = {
      wireMode: 'main',
      suggestion: null,
    };
    if (shouldSkipRouteSuggest(opts.composerResult)) return out;
    if (!shouldShowSwitch(hit)) return out;

    let title = opts.title != null ? String(opts.title) : '';
    if (!title && Array.isArray(opts.threads)) {
      const th = opts.threads.find(function (t) {
        return t && String(t.id) === String(hit.threadId);
      });
      if (th && th.title != null) title = String(th.title);
    }

    out.suggestion = {
      threadId: String(hit.threadId),
      title: title || '',
      score: typeof hit.score === 'number' ? hit.score : Number(hit.score) || 0,
      reason: 'match',
      body: opts.body != null ? String(opts.body) : '',
    };
    return out;
  }

  // planAutoRouteAction(hit, opts) → { steal: false, suggestion|null }
  // Hermetic contract for the send() default path (🎯T135): match never steals.
  function planAutoRouteAction(hit, opts) {
    const plan = planMainSend(hit, opts);
    return {
      steal: false,
      suggestion: plan.suggestion,
    };
  }

  // planOptInSwitch(suggestion, body) → interrupt main + deliver to thread.
  // Only when owner clicks the switch affordance.
  function planOptInSwitch(suggestion, body) {
    const s = suggestion || {};
    const id = s.threadId != null ? String(s.threadId) : '';
    if (!id || id === 'main') {
      return {
        ok: false,
        interruptMain: false,
        deliverTo: null,
        body: '',
        reason: 'no-thread',
      };
    }
    const text = body != null && body !== ''
      ? String(body)
      : (s.body != null ? String(s.body) : '');
    if (!String(text || '').trim()) {
      return {
        ok: false,
        interruptMain: false,
        deliverTo: null,
        body: '',
        reason: 'empty-body',
      };
    }
    return {
      ok: true,
      interruptMain: true,
      deliverTo: id,
      body: text,
      title: s.title != null ? String(s.title) : '',
      reason: 'opt-in',
    };
  }

  return {
    shouldShowSwitch: shouldShowSwitch,
    shouldSkipRouteSuggest: shouldSkipRouteSuggest,
    formatSwitchLabel: formatSwitchLabel,
    planMainSend: planMainSend,
    planAutoRouteAction: planAutoRouteAction,
    planOptInSwitch: planOptInSwitch,
  };
}));
